package repository

import (
	"fmt"
	"log"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
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

	if err := commonExecution.EnsureStore(db); err != nil {
		return nil, fmt.Errorf("failed to ensure execution store: %w", err)
	}

	// AutoMigrate - 确保表结构最新
	// 所有表由 GORM AutoMigrate 管理（符合统一的数据库表创建策略）
	if err := db.AutoMigrate(
		&models.DevTask{}, // 开发任务（query、workflow、script）
	); err != nil {
		return nil, fmt.Errorf("failed to auto migrate: %w", err)
	}

	if err := db.Exec("DROP TABLE IF EXISTS develop.dev_items").Error; err != nil {
		return nil, fmt.Errorf("failed to drop legacy develop.dev_items: %w", err)
	}

	if err := normalizeDevTaskContent(db); err != nil {
		return nil, err
	}

	log.Println("✅ Database connected successfully (AutoMigrate 完成)")

	return db, nil
}

func normalizeDevTaskContent(db *gorm.DB) error {
	statements := []struct {
		name string
		sql  string
	}{
		{
			name: "normalize notebook dev_type",
			sql:  "UPDATE develop.dev_tasks SET dev_type = 'script', updated_at = NOW() WHERE dev_type = 'notebook'",
		},
		{
			name: "normalize notebook executions task_type",
			sql:  "UPDATE common.task_executions SET task_type = 'script', updated_at = NOW() WHERE module = 'develop' AND task_type = 'notebook'",
		},
		{
			name: "normalize query content.sql",
			sql: `
UPDATE develop.dev_tasks
SET content = jsonb_set(content - 'sql', '{query}', content->'sql', true),
    updated_at = NOW()
WHERE dev_type = 'query'
  AND content ? 'sql'
  AND NOT content ? 'query'`,
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
			name: "normalize workflow_def",
			sql: `
UPDATE develop.dev_tasks
SET content = jsonb_set(content - 'workflow_def', '{workflow_definition}', content->'workflow_def', true),
    updated_at = NOW()
WHERE dev_type = 'workflow'
  AND content ? 'workflow_def'
  AND NOT content ? 'workflow_definition'`,
		},
		{
			name: "normalize workflow top-level graph",
			sql: `
UPDATE develop.dev_tasks
SET content = jsonb_set(
        content - 'nodes' - 'edges',
        '{workflow_definition}',
        jsonb_build_object(
            'nodes', COALESCE(content->'nodes', '[]'::jsonb),
            'edges', COALESCE(content->'edges', '[]'::jsonb)
        ),
        true
    ),
    updated_at = NOW()
WHERE dev_type = 'workflow'
  AND NOT content ? 'workflow_definition'
  AND (content ? 'nodes' OR content ? 'edges')`,
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
			name: "normalize input_data",
			sql: `
UPDATE develop.dev_tasks
SET content = jsonb_set(content - 'input_data', '{inputs}', content->'input_data', true),
    updated_at = NOW()
WHERE content ? 'input_data'
  AND NOT content ? 'inputs'`,
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

	for _, stmt := range statements {
		if err := db.Exec(stmt.sql).Error; err != nil {
			return fmt.Errorf("failed to %s: %w", stmt.name, err)
		}
	}

	return nil
}
