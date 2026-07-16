package repository

import (
	"fmt"
	"log"
	"os"

	commonExecution "github.com/addp/common/execution"
	commonUtils "github.com/addp/common/utils"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/pkg/utils"
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

func AutoMigrate(db *gorm.DB) error {
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
	if err := removeBuiltinMathWorkflowExample(db); err != nil {
		return err
	}
	if err := dropUsersIsSuperuser(db); err != nil {
		return err
	}
	return db.AutoMigrate(
		&models.Tenant{},
		&models.User{},
		&models.AuditLog{},
		&models.Engine{},
		&models.Application{},
		&models.APIKey{},
		&models.OAuthClient{},
		&models.RefreshTokenFamily{},
		&models.RefreshToken{},
		&models.AccessToken{},
		&models.OAuthAuthorizationCode{},
		&models.OAuthDeviceAuthorization{},
		&models.TaskProvider{},
		&models.ModuleRegistry{},
	)
}

func EnsureBuiltinOAuthClients(db *gorm.DB) error {
	client := models.OAuthClient{
		ClientID:          "addp-cli",
		Name:              "ADDP CLI",
		ClientType:        models.OAuthClientTypePublic,
		RedirectURIs:      []string{"http://127.0.0.1:8765/callback"},
		AllowedScopes:     []string{"addp.api"},
		DeviceFlowEnabled: true,
		IsActive:          true,
	}
	return db.Where("client_id = ?", client.ClientID).Assign(client).FirstOrCreate(&client).Error
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

func dropUsersIsSuperuser(db *gorm.DB) error {
	return db.Exec(`
		DO $$
		BEGIN
			IF to_regclass('system.users') IS NOT NULL THEN
				IF EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = 'system'
					  AND table_name = 'users'
					  AND column_name = 'is_superuser'
				) THEN
					ALTER TABLE system.users DROP COLUMN is_superuser;
				END IF;
			END IF;
		END $$;
	`).Error
}

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

// InitSuperAdmin 初始化超级管理员用户
func InitSuperAdmin(db *gorm.DB) error {
	// 从环境变量读取超级管理员配置
	adminUsername := getEnv("SUPER_ADMIN_USERNAME", "SuperAdmin")
	adminPassword := getEnv("SUPER_ADMIN_PASSWORD", "20251001#SuperAdmin")
	adminEmail := getEnv("SUPER_ADMIN_EMAIL", "superadmin@addp.com")

	// 检查SuperAdmin用户是否存在
	var user models.User
	result := db.Where("username = ?", adminUsername).First(&user)

	if result.Error == gorm.ErrRecordNotFound {
		// 创建SuperAdmin用户
		passwordHash, err := utils.HashPassword(adminPassword)
		if err != nil {
			return err
		}

		superAdminUser := models.User{
			Username:     adminUsername,
			Email:        adminEmail,
			PasswordHash: passwordHash,
			FullName:     "系统超级管理员",
			IsActive:     true,
			UserType:     models.UserTypeSuperAdmin,
			TenantID:     nil, // 超级管理员没有租户
		}

		if err := db.Create(&superAdminUser).Error; err != nil {
			return err
		}

		log.Printf("✅ 超级管理员已创建: %s / %s\n", adminUsername, adminPassword)
		return nil
	}

	if result.Error != nil {
		return result.Error
	}

	// 如果SuperAdmin用户存在，确保类型正确
	if user.UserType != models.UserTypeSuperAdmin {
		user.UserType = models.UserTypeSuperAdmin
		user.TenantID = nil
		if err := db.Save(&user).Error; err != nil {
			return err
		}
		log.Println("SuperAdmin用户类型已更新")
	}

	return nil
}

// InitDefaultTenant 初始化默认租户和租户管理员
// 仅在开发环境且显式启用时创建,生产环境强制禁用
func InitDefaultTenant(db *gorm.DB) error {
	// 检查是否启用默认租户功能
	enableDefaultTenant := getEnv("ENABLE_DEFAULT_TENANT", "false")
	env := getEnv("ENV", "development")

	// 生产环境强制禁用
	if env == "production" {
		log.Println("⚠️  跳过默认租户初始化 (生产环境禁止创建默认测试账户)")
		return nil
	}

	// 未显式启用则跳过
	if enableDefaultTenant != "true" {
		log.Println("ℹ️  跳过默认租户初始化 (未启用 ENABLE_DEFAULT_TENANT)")
		return nil
	}

	// 从环境变量读取默认租户配置
	tenantName := getEnv("DEFAULT_TENANT_NAME", "默认租户")
	tenantDesc := getEnv("DEFAULT_TENANT_DESCRIPTION", "用于开发和测试的默认租户")
	adminUsername := getEnv("DEFAULT_ADMIN_USERNAME", "admin")
	adminPassword := getEnv("DEFAULT_ADMIN_PASSWORD", "123456")
	adminEmail := getEnv("DEFAULT_ADMIN_EMAIL", "admin@addp.com")

	// 1. 检查并创建默认租户
	var tenant models.Tenant
	result := db.Where("name = ?", tenantName).First(&tenant)

	if result.Error == gorm.ErrRecordNotFound {
		// 创建默认租户
		tenant = models.Tenant{
			Name:        tenantName,
			Description: tenantDesc,
			IsActive:    true,
		}

		if err := db.Create(&tenant).Error; err != nil {
			log.Printf("❌ 默认租户创建失败: %v\n", err)
			return err
		}

		log.Printf("✅ 默认租户已创建: %s (ID: %d)\n", tenantName, tenant.ID)
	} else if result.Error != nil {
		log.Printf("❌ 查询默认租户失败: %v\n", result.Error)
		return result.Error
	} else {
		log.Printf("ℹ️  默认租户已存在: %s (ID: %d)\n", tenantName, tenant.ID)
	}

	// 2. 检查并创建租户管理员
	var user models.User
	result = db.Where("username = ?", adminUsername).First(&user)

	if result.Error == gorm.ErrRecordNotFound {
		// 创建租户管理员用户
		passwordHash, err := utils.HashPassword(adminPassword)
		if err != nil {
			log.Printf("❌ 密码加密失败: %v\n", err)
			return err
		}

		tenantAdmin := models.User{
			Username:     adminUsername,
			Email:        adminEmail,
			PasswordHash: passwordHash,
			FullName:     "默认租户管理员",
			IsActive:     true,
			UserType:     models.UserTypeTenantAdmin,
			TenantID:     &tenant.ID, // 关联到默认租户
		}

		if err := db.Create(&tenantAdmin).Error; err != nil {
			log.Printf("❌ 租户管理员创建失败: %v\n", err)
			return err
		}

		log.Printf("✅ 默认租户管理员已创建: %s / %s (租户: %s)\n", adminUsername, adminPassword, tenantName)
		return nil
	}

	if result.Error != nil {
		log.Printf("❌ 查询租户管理员失败: %v\n", result.Error)
		return result.Error
	}

	// 如果用户已存在,确保类型和租户关联正确
	if user.UserType != models.UserTypeTenantAdmin || user.TenantID == nil || *user.TenantID != tenant.ID {
		user.UserType = models.UserTypeTenantAdmin
		user.TenantID = &tenant.ID
		if err := db.Save(&user).Error; err != nil {
			log.Printf("❌ 更新租户管理员信息失败: %v\n", err)
			return err
		}
		log.Printf("ℹ️  租户管理员信息已更新: %s (租户: %s)\n", adminUsername, tenantName)
	} else {
		log.Printf("ℹ️  租户管理员已存在: %s (租户: %s)\n", adminUsername, tenantName)
	}

	return nil
}

// MigrateExistingEnginesDisplayName 迁移现有引擎的 display_name
// 为所有 display_name 为空的引擎设置为 name 的值
func MigrateExistingEnginesDisplayName(db *gorm.DB) error {
	// 为所有 display_name 为空的引擎设置为 name 的值
	result := db.Exec(`
		UPDATE system.engines
		SET display_name = name
		WHERE display_name = '' OR display_name IS NULL
	`)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		log.Printf("✅ 已为 %d 个现有引擎设置 display_name\n", result.RowsAffected)
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
		// 2. 删除旧列。任务创建/编辑入口必须由 task.capabilities/v1 的 task_capabilities[] 声明。
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
