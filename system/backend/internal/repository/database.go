package repository

import (
	"fmt"
	"log"
	"os"

	commonExecution "github.com/addp/common/execution"
	commonUtils "github.com/addp/common/utils"
	"github.com/addp/system/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(dbPath string) (*gorm.DB, error) {
	// 从环境变量读取 PostgreSQL 连接信息
	host := getEnv("POSTGRES_HOST", "localhost")
	port := getEnv("POSTGRES_PORT", "5432")
	user := getEnv("POSTGRES_USER", "addp")
	password := getEnv("POSTGRES_PASSWORD", "addp_password")
	dbname := getEnv("POSTGRES_DB", "addp")

	// 构建 PostgreSQL DSN
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=system TimeZone=%s",
		host, port, user, password, dbname, commonUtils.GetTimezone())

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	// 确保 system schema 存在
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS system").Error; err != nil {
		return nil, err
	}

	// 设置默认 schema 为 system
	db.Exec("SET search_path TO system")

	return db, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func AutoMigrateNonIAM(db *gorm.DB) error {
	if err := commonExecution.EnsureStore(db); err != nil {
		return err
	}
	if err := migrateEngineOrigin(db); err != nil {
		return err
	}
	if err := renameGeoPythonWorkflowEngineType(db); err != nil {
		return err
	}
	if err := dropEngineScanConfig(db); err != nil {
		return err
	}
	if err := dropDeprecatedEngineColumns(db); err != nil {
		return err
	}
	if err := migrateEngineLifecycle(db); err != nil {
		return err
	}
	if err := removeBuiltinMathWorkflowExample(db); err != nil {
		return err
	}
	return db.AutoMigrate(
		&models.Engine{},
		&models.Application{},
		&models.APIKey{},
		&models.TaskProvider{},
		&models.ModuleRegistry{},
	)
}

func migrateEngineLifecycle(db *gorm.DB) error {
	return db.Exec(`
		DO $$
		BEGIN
			IF to_regclass('system.engines') IS NOT NULL THEN
				IF NOT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'system' AND table_name = 'engines' AND column_name = 'lifecycle_state'
				) THEN
					ALTER TABLE system.engines ADD COLUMN lifecycle_state VARCHAR(20);
					IF EXISTS (
						SELECT 1 FROM information_schema.columns
						WHERE table_schema = 'system' AND table_name = 'engines' AND column_name = 'is_active'
					) THEN
						UPDATE system.engines
						SET lifecycle_state = CASE WHEN is_active THEN 'active' ELSE 'disabled' END;
					ELSE
						UPDATE system.engines SET lifecycle_state = 'active';
					END IF;
				END IF;

				UPDATE system.engines SET lifecycle_state = 'active' WHERE lifecycle_state IS NULL OR lifecycle_state = '';
				ALTER TABLE system.engines
					ALTER COLUMN lifecycle_state SET DEFAULT 'active',
					ALTER COLUMN lifecycle_state SET NOT NULL;

				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'system' AND table_name = 'engines' AND column_name = 'is_active'
				) THEN
					ALTER TABLE system.engines DROP COLUMN is_active;
				END IF;
			END IF;
		END $$;
	`).Error
}

func renameGeoPythonWorkflowEngineType(db *gorm.DB) error {
	return db.Exec(renameGeoPythonWorkflowEngineTypeSQL).Error
}

const renameGeoPythonWorkflowEngineTypeSQL = `
		DO $$
		BEGIN
			IF to_regclass('system.engines') IS NOT NULL THEN
				IF EXISTS (
					SELECT 1 FROM system.engines WHERE lower(engine_type) = 'python_workflow'
				) AND EXISTS (
					SELECT 1 FROM system.engines WHERE lower(engine_type) = 'geopython_workflow'
				) THEN
					RAISE EXCEPTION 'cannot rename python_workflow: geopython_workflow already exists';
				END IF;

				UPDATE system.engines
				SET engine_type = 'geopython_workflow',
					name = 'GeoPython 工作流引擎',
					description = '基于 Python 地理计算生态的工作流引擎，支持 Pandas、GeoPandas、GDAL/OGR 等能力'
				WHERE lower(engine_type) = 'python_workflow';
			END IF;
		END $$;
	`

func migrateEngineOrigin(db *gorm.DB) error {
	return db.Exec(`
		DO $$
		BEGIN
			IF to_regclass('system.engines') IS NOT NULL THEN
				IF EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'engines'
					  AND column_name = 'engine_category'
				) AND NOT EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'engines'
					  AND column_name = 'engine_origin'
				) THEN
					ALTER TABLE system.engines RENAME COLUMN engine_category TO engine_origin;
				END IF;

				IF EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'engines'
					  AND column_name = 'engine_origin'
				) THEN
					UPDATE system.engines
					SET engine_origin = CASE
						WHEN engine_origin = 'standard' THEN 'general'
						WHEN engine_origin IN ('general', 'extension') THEN engine_origin
						ELSE 'general'
					END;

					ALTER TABLE system.engines
						ALTER COLUMN engine_origin SET DEFAULT 'general',
						ALTER COLUMN engine_origin SET NOT NULL;
				END IF;

				IF to_regclass('system.idx_engines_category') IS NOT NULL THEN
					ALTER INDEX system.idx_engines_category RENAME TO idx_engines_origin;
				END IF;
			END IF;
		END $$;
	`).Error
}

func dropEngineScanConfig(db *gorm.DB) error {
	return db.Exec(`
		DO $$
		BEGIN
			IF to_regclass('system.engines') IS NOT NULL THEN
				IF EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'engines'
					  AND column_name = 'scan_config'
				) THEN
					ALTER TABLE system.engines DROP COLUMN scan_config;
				END IF;
			END IF;
		END $$;
	`).Error
}

func dropDeprecatedEngineColumns(db *gorm.DB) error {
	return db.Exec(dropDeprecatedEngineColumnsSQL).Error
}

func removeBuiltinMathWorkflowExample(db *gorm.DB) error {
	return db.Exec(removeBuiltinMathWorkflowExampleSQL).Error
}

const removeBuiltinMathWorkflowExampleSQL = `
		DO $$
		BEGIN
			IF to_regclass('system.engines') IS NOT NULL THEN
				DELETE FROM system.engines
				WHERE lower(engine_type) = 'math_workflow'
				  AND is_builtin = true;
			END IF;
		END $$;
	`

const dropDeprecatedEngineColumnsSQL = `
		DO $$
		BEGIN
			IF to_regclass('system.engines') IS NOT NULL THEN
				IF to_regclass('system.idx_engines_identifier') IS NOT NULL THEN
					DROP INDEX system.idx_engines_identifier;
				END IF;

				IF EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'engines'
					  AND column_name = 'unique_identifier'
				) THEN
					ALTER TABLE system.engines DROP COLUMN unique_identifier;
				END IF;

				IF EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'engines'
					  AND column_name = 'extension_api_config'
				) THEN
					ALTER TABLE system.engines DROP COLUMN extension_api_config;
				END IF;

				IF EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'engines'
					  AND column_name = 'health_check_config'
				) THEN
					ALTER TABLE system.engines DROP COLUMN health_check_config;
				END IF;
			END IF;
		END $$;
	`

// RemoveLocalFileEnginesFromSystem 删除误注册到 System 的本地文件型连接器。
// SQLite/SpatiaLite 作为文件格式或容器处理，System 后端不把本地文件路径注册为 engine。
func RemoveLocalFileEnginesFromSystem(db *gorm.DB) error {
	result := db.Exec(`
		DELETE FROM system.engines
		WHERE lower(engine_type) IN ('sqlite', 'spatialite')
	`)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Printf("✅ 已清理 %d 个误注册到 System 的 SQLite/SpatiaLite 引擎\n", result.RowsAffected)
	}
	return nil
}

// MigrateTaskProviders 迁移 task_providers 表：删除旧 task_providers 顶层入口字段，并规范化历史 endpoint。
func MigrateTaskProviders(db *gorm.DB) error {
	// 1. 检查 create_task_url 列是否存在（幂等）
	var colCount int64
	db.Raw(`
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = 'task_providers'
		AND column_name = 'create_task_url'
	`).Scan(&colCount)

	if colCount > 0 {
		// 2. 删除旧列。任务创建/编辑入口必须由 task.capabilities/v2 的 task_capabilities[] 声明。
		if err := db.Exec(`
			ALTER TABLE system.task_providers
			DROP COLUMN IF EXISTS create_task_url,
			DROP COLUMN IF EXISTS edit_task_url
		`).Error; err != nil {
			return fmt.Errorf("task_providers 旧列删除失败: %w", err)
		}

		log.Println("✅ task_providers 迁移完成（create_task_url/edit_task_url 旧列已删除）")
	}

	if err := db.Exec(`
		UPDATE system.task_providers
		SET task_status_endpoint = '/api/v1/meta/executions/{execution_id}'
		WHERE module_name = 'meta'
		  AND task_status_endpoint = '/api/v1/meta/scan/runs/{execution_id}'
	`).Error; err != nil {
		return fmt.Errorf("task_providers 标准执行详情 endpoint 迁移失败: %w", err)
	}

	return nil
}

// CreateModuleRegistryIndexes 创建模块注册表的索引
func CreateModuleRegistryIndexes(db *gorm.DB) error {
	// 为 status 字段创建索引(加速模块状态查询)
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_module_registry_status
		ON system.module_registry(status)
	`).Error; err != nil {
		return err
	}

	// 为 last_heartbeat 字段创建索引(加速心跳超时查询)
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_module_registry_heartbeat
		ON system.module_registry(last_heartbeat)
	`).Error; err != nil {
		return err
	}

	log.Println("✅ 模块注册表索引已创建")
	return nil
}
