package repository

import (
	"fmt"
	"log"
	"os"

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
	return db.AutoMigrate(
		&models.Tenant{},
		&models.User{},
		&models.AuditLog{},
		&models.Engine{},
		&models.Application{},
		&models.APIKey{},
		&models.TaskProvider{},
		&models.ModuleRegistry{},
	)
}

// RemoveLocalFileEnginesFromSystem 删除误注册到 System 的本地文件型连接器。
// SQLite/SpatiaLite 文件路径只在 Transfer 本地引擎执行面有意义，System 后端无法保证可访问这些路径。
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
			IsSuperuser:  true,
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
		user.IsSuperuser = true
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
			IsSuperuser:  false,
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
		user.IsSuperuser = false
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

// MigrateTaskProviders 迁移 task_providers 表：将 create_task_url/edit_task_url 合入 capabilities，添加 task_cancel_endpoint
func MigrateTaskProviders(db *gorm.DB) error {
	// 1. 检查 create_task_url 列是否存在（幂等）
	var colCount int64
	db.Raw(`
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = 'task_providers'
		AND column_name = 'create_task_url'
	`).Scan(&colCount)

	if colCount == 0 {
		// 列已删除，无需迁移
		return nil
	}

	// 2. 将 create_task_url/edit_task_url 合并进 capabilities JSONB
	if err := db.Exec(`
		UPDATE system.task_providers
		SET capabilities = jsonb_set(
			jsonb_set(
				COALESCE(capabilities::jsonb, '{}'),
				'{create_task_url}',
				to_jsonb(create_task_url)
			),
			'{edit_task_url}',
			to_jsonb(edit_task_url)
		)
		WHERE create_task_url IS NOT NULL AND create_task_url != ''
	`).Error; err != nil {
		return fmt.Errorf("task_providers capabilities 迁移失败: %w", err)
	}

	// 3. 删除旧列
	if err := db.Exec(`
		ALTER TABLE system.task_providers
		DROP COLUMN IF EXISTS create_task_url,
		DROP COLUMN IF EXISTS edit_task_url
	`).Error; err != nil {
		return fmt.Errorf("task_providers 旧列删除失败: %w", err)
	}

	log.Println("✅ task_providers 迁移完成（create_task_url/edit_task_url 已合入 capabilities）")
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
