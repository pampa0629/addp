package repository

import (
	"fmt"
	"log"
	"os"

	commonModels "github.com/addp/common/models"
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
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=system",
		host, port, user, password, dbname)

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
	)
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

// InitBuiltinEngines 初始化内置引擎（Python Workflow、Spark Workflow 等）
// 内置引擎在启动时自动注册，不属于任何租户，所有租户均可使用
func InitBuiltinEngines(db *gorm.DB) error {
	// 定义内置引擎配置
	builtinEngines := []struct {
		Name        string
		EngineType  string
		Description string
		BaseURL     string
	}{
		{
			Name:        "Python 工作流引擎",
			EngineType:  "python_workflow",
			Description: "基于 Python 的工作流执行引擎,支持 GeoPandas、Pandas、NumPy 等数据处理库",
			BaseURL:     getEnv("PYTHON_WORKFLOW_URL", "http://localhost:8099"),
		},
	}

	// 注册每个内置引擎
	for _, engineDef := range builtinEngines {
		// 幂等检查：通过 engine_type + is_builtin 查找
		var existingEngine models.Engine
		result := db.Where("engine_type = ? AND is_builtin = true", engineDef.EngineType).First(&existingEngine)

		if result.Error == gorm.ErrRecordNotFound {
			// 创建新引擎
			// 将 base_url 解析为 protocol, host, port 格式
			connectionInfo, err := commonModels.ParseBaseURLToConnectionInfo(engineDef.BaseURL)
			if err != nil {
				log.Printf("❌ 内置引擎 %s 的 base_url 解析失败: %v\n", engineDef.Name, err)
				continue
			}

			// 根据引擎类型生成默认 capabilities
			var capabilities string
			if engineDef.EngineType == "python_workflow" {
				capabilities = `{"compute":[{"dev_modes":["workflow"],"supported_formats":["geojson","wkt","csv","parquet"],"features":["dag","memory_efficient","batch","pandas","numpy","scipy"],"description":"Python数据处理（Pandas, GeoPandas, NumPy, SciPy）"}]}`
			} else if engineDef.EngineType == "spark_workflow" {
				capabilities = `{"compute":[{"dev_modes":["workflow"],"engine":"spark","scale":"distributed","features":["big_data","distributed"],"description":"分布式空间分析"}]}`
			} else {
				capabilities = `{"storage":[{"type":"generic"}]}`
			}

			engine := models.Engine{
				Name:             engineDef.Name,
				EngineType:       engineDef.EngineType,
				EngineCategory:   "extension",
				Description:      engineDef.Description,
				ConnectionInfo:   connectionInfo,
				Capabilities:     &capabilities,
				IsActive:         true,
				IsBuiltin:        true,
				TenantID:         nil, // 内置引擎不属于任何租户
				ConnectionStatus: "unknown",
			}

			if err := db.Create(&engine).Error; err != nil {
				log.Printf("❌ 内置引擎创建失败: %s - %v\n", engineDef.Name, err)
				continue
			}

			log.Printf("✅ 内置引擎已注册: %s (ID: %d, 类型: %s)\n", engineDef.Name, engine.ID, engineDef.EngineType)
		} else if result.Error != nil {
			log.Printf("❌ 查询内置引擎失败: %s - %v\n", engineDef.Name, result.Error)
			continue
		} else {
			// 已存在，更新配置（确保配置最新）
			connectionInfo, err := commonModels.ParseBaseURLToConnectionInfo(engineDef.BaseURL)
			if err != nil {
				log.Printf("❌ 内置引擎 %s 的 base_url 解析失败: %v\n", engineDef.Name, err)
				continue
			}
			existingEngine.ConnectionInfo = connectionInfo

			if err := db.Save(&existingEngine).Error; err != nil {
				log.Printf("❌ 内置引擎更新失败: %s - %v\n", engineDef.Name, err)
				continue
			}
			log.Printf("🔄 内置引擎已更新: %s (ID: %d)\n", engineDef.Name, existingEngine.ID)
		}
	}

	return nil
}

