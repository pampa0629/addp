package repository

import (
	"fmt"
	"log"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := ConnectDatabase(cfg)
	if err != nil {
		return nil, err
	}

	if err := commonExecution.EnsureStore(db); err != nil {
		return nil, fmt.Errorf("failed to ensure execution store: %w", err)
	}
	if err := exportartifact.EnsureStore(db, "develop.export_sessions"); err != nil {
		return nil, fmt.Errorf("failed to ensure export session store: %w", err)
	}

	// AutoMigrate - 确保表结构最新
	// 所有表由 GORM AutoMigrate 管理（符合统一的数据库表创建策略）
	if err := db.AutoMigrate(
		&models.DevTask{},      // 开发任务（query、workflow、script）
		&models.ToolApproval{}, // 委托 workflow.run 审批事实
		&models.QueryPolicy{},  // Develop 查询策略
		&models.CatalogResourceChangeRow{},
	); err != nil {
		return nil, fmt.Errorf("failed to auto migrate: %w", err)
	}

	if err := db.Exec("DROP TABLE IF EXISTS develop.dev_items").Error; err != nil {
		return nil, fmt.Errorf("failed to drop legacy develop.dev_items: %w", err)
	}

	if err := normalizeDevTaskContent(db); err != nil {
		return nil, err
	}
	if err := migrateCatalogDevTaskChanges(db); err != nil {
		return nil, err
	}

	log.Println("✅ Database connected successfully (AutoMigrate 完成)")

	return db, nil
}

func migrateCatalogDevTaskChanges(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(2026082605)).Error; err != nil {
			return fmt.Errorf("acquire Develop catalog schema lock: %w", err)
		}
		statements := []string{
			`CREATE TABLE IF NOT EXISTS develop.data_migrations (
				version BIGINT PRIMARY KEY,
				name TEXT NOT NULL,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
			`CREATE INDEX IF NOT EXISTS idx_develop_catalog_changes_tenant_id
				ON develop.catalog_resource_changes (tenant_id, id)`,
			`CREATE INDEX IF NOT EXISTS idx_develop_catalog_changes_source
				ON develop.catalog_resource_changes (tenant_id, source_type, source_identity, id DESC)`,
			`INSERT INTO develop.catalog_resource_changes (
				tenant_id, source_type, source_identity, operation, snapshot, observed_at
			)
			SELECT
				task.tenant_id,
				'dev_task',
				task.id,
				'upsert',
				jsonb_strip_nulls(jsonb_build_object(
					'name', COALESCE(NULLIF(btrim(task.display_name), ''), task.name),
					'code', task.name,
					'object_kind', 'development_task',
					'artifact_type', task.dev_type,
					'task_status', task.status,
					'query_type', CASE WHEN task.dev_type = 'query' THEN task.content->>'query_type' ELSE NULL END,
					'engine_id', CASE WHEN COALESCE(task.execution_config->>'engine_id', '') ~ '^[1-9][0-9]*$' THEN task.execution_config->>'engine_id' ELSE NULL END
				)),
				COALESCE(task.updated_at, task.created_at, NOW())
			FROM develop.dev_tasks AS task
			WHERE task.deleted_at IS NULL
			  AND task.dev_type IN ('query', 'workflow')
			  AND NOT EXISTS (SELECT 1 FROM develop.data_migrations WHERE version = 2026082603)
			ORDER BY task.id`,
			`INSERT INTO develop.data_migrations (version, name)
			VALUES (2026082603, 'catalog_dev_task_change_feed_v1')
			ON CONFLICT (version) DO NOTHING`,
			`CREATE OR REPLACE FUNCTION develop.capture_dev_task_catalog_change()
			RETURNS TRIGGER
			LANGUAGE plpgsql
			AS $function$
			DECLARE
				changed develop.dev_tasks%ROWTYPE;
				operation TEXT;
			BEGIN
				changed := CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
				IF changed.dev_type NOT IN ('query', 'workflow') THEN
					RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
				END IF;
				operation := CASE
					WHEN TG_OP = 'DELETE' OR changed.deleted_at IS NOT NULL THEN 'missing'
					ELSE 'upsert'
				END;
				INSERT INTO develop.catalog_resource_changes (
					tenant_id, source_type, source_identity, operation, snapshot, observed_at
				) VALUES (
					changed.tenant_id,
					'dev_task',
					changed.id,
					operation,
					jsonb_strip_nulls(jsonb_build_object(
						'name', COALESCE(NULLIF(btrim(changed.display_name), ''), changed.name),
						'code', changed.name,
						'object_kind', 'development_task',
						'artifact_type', changed.dev_type,
						'task_status', changed.status,
						'query_type', CASE WHEN changed.dev_type = 'query' THEN changed.content->>'query_type' ELSE NULL END,
						'engine_id', CASE WHEN COALESCE(changed.execution_config->>'engine_id', '') ~ '^[1-9][0-9]*$' THEN changed.execution_config->>'engine_id' ELSE NULL END
					)),
					NOW()
				);
				RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
			END;
			$function$`,
			`DROP TRIGGER IF EXISTS trg_develop_dev_task_catalog_change ON develop.dev_tasks`,
			`CREATE TRIGGER trg_develop_dev_task_catalog_change
			AFTER INSERT OR UPDATE OR DELETE ON develop.dev_tasks
			FOR EACH ROW EXECUTE FUNCTION develop.capture_dev_task_catalog_change()`,
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("migrate Develop catalog DevTask changes: %w", err)
			}
		}
		return nil
	})
}

// ConnectDatabase opens the Develop store without running owner migrations.
// Worker processes use this path; the Backend remains the only schema owner.
func ConnectDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresUser,
		cfg.PostgresPassword,
		cfg.PostgresDB,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 设置默认 schema
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s, public", cfg.DBSchema)).Error; err != nil {
		return nil, fmt.Errorf("failed to set search_path: %w", err)
	}

	return db, nil
}

func normalizeDevTaskContent(db *gorm.DB) error {
	for _, stmt := range normalizeDevTaskContentStatements() {
		if err := db.Exec(stmt.sql).Error; err != nil {
			return fmt.Errorf("failed to %s: %w", stmt.name, err)
		}
	}

	return nil
}

type normalizationStatement struct {
	name string
	sql  string
}

func normalizeDevTaskContentStatements() []normalizationStatement {
	return []normalizationStatement{
		{
			name: "normalize notebook dev_type",
			sql:  "UPDATE develop.dev_tasks SET dev_type = 'script', updated_at = NOW() WHERE dev_type = 'notebook'",
		},
		{
			name: "normalize notebook executions task_type",
			sql:  "UPDATE common.task_executions SET task_type = 'script', updated_at = NOW() WHERE module = 'develop' AND task_type = 'notebook'",
		},
		{
			name: "remove query content.sql",
			sql: `
UPDATE develop.dev_tasks
SET content = content - 'sql',
    updated_at = NOW()
WHERE dev_type = 'query'
  AND content ? 'sql'`,
		},
		{
			name: "default query_type",
			sql: `
UPDATE develop.dev_tasks
SET content = jsonb_set(content, '{query_type}', '"sql"'::jsonb, true),
    updated_at = NOW()
WHERE dev_type = 'query'
  AND NOT content ? 'query_type'`,
		},
		{
			name: "remove workflow_def",
			sql: `
UPDATE develop.dev_tasks
SET content = content - 'workflow_def',
    updated_at = NOW()
WHERE dev_type = 'workflow'
  AND content ? 'workflow_def'`,
		},
		{
			name: "remove workflow top-level graph",
			sql: `
UPDATE develop.dev_tasks
SET content = content - 'nodes' - 'edges',
    updated_at = NOW()
WHERE dev_type = 'workflow'
  AND (content ? 'nodes' OR content ? 'edges')`,
		},
		{
			name: "remove input_data",
			sql: `
UPDATE develop.dev_tasks
SET content = content - 'input_data',
    updated_at = NOW()
WHERE content ? 'input_data'`,
		},
		{
			name: "drop unsupported dev_type",
			sql:  "DELETE FROM develop.dev_tasks WHERE dev_type NOT IN ('query', 'workflow', 'script')",
		},
		{
			name: "drop unsupported dev task schedule columns",
			sql: `
ALTER TABLE develop.dev_tasks
    DROP COLUMN IF EXISTS schedule,
    DROP COLUMN IF EXISTS enabled,
    DROP COLUMN IF EXISTS next_run_at`,
		},
	}
}
