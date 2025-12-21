package repository

import (
	"fmt"
	"log"
	"os"

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
		&models.Resource{},
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

// MigrateExistingResourcesDisplayName 迁移现有资源的 display_name
// 为所有 display_name 为空的资源设置为 name 的值
func MigrateExistingResourcesDisplayName(db *gorm.DB) error {
	// 为所有 display_name 为空的资源设置为 name 的值
	result := db.Exec(`
		UPDATE system.resources
		SET display_name = name
		WHERE display_name = '' OR display_name IS NULL
	`)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		log.Printf("✅ 已为 %d 个现有资源设置 display_name\n", result.RowsAffected)
	}
	return nil
}

// InitGeoPandasEngine 初始化 GeoPandas 计算引擎资源
// GeoPandas 作为内置计算引擎，只提供 API 地址，不暴露算子到 capabilities
// Develop 模块通过调用 GeoPandas API 动态获取算子列表
func InitGeoPandasEngine(db *gorm.DB) error {
	// 从环境变量读取 GeoPandas 配置
	apiURL := getEnv("GEOPANDAS_ENGINE_URL", "http://geopandas-engine:8099")

	// 定义 unique_identifier
	uniqueIdentifier := "geopandas.default"

	// 检查 GeoPandas 资源是否已存在
	var resource models.Resource
	result := db.Where("unique_identifier = ?", uniqueIdentifier).First(&resource)

	if result.Error == gorm.ErrRecordNotFound {
		// 创建 GeoPandas 资源
		// 注意：按照架构要求，不在 capabilities 中暴露算子
		// Develop 模块将调用 API 动态获取算子列表
		// 但需要声明基本的计算能力，以便前端过滤器识别
		capabilitiesJSON := `{"compute":[{"type":"spatial","dev_modes":["workflow"],"description":"空间计算引擎","features":["geopandas","dag","memory_efficient"]}]}`
		resource = models.Resource{
			Name:             "geopandas_default",
			DisplayName:      "GeoPandas 计算引擎",
			ResourceType:     "geopandas",
			Description:      "基于 Python 的空间数据处理引擎，提供 GeoDataFrame 计算能力",
			IsActive:         true,
			IsBuiltin:        true,
			UniqueIdentifier: &uniqueIdentifier,
			ConnectionInfo: models.ConnectionInfo{
				"api_url": apiURL,
				"type":    "compute_engine",
			},
			// Capabilities 声明基本计算能力（用于前端过滤）
			// 具体算子列表通过调用 {api_url}/api/spatial/operators 动态获取
			Capabilities: &capabilitiesJSON,
		}

		if err := db.Create(&resource).Error; err != nil {
			log.Printf("❌ GeoPandas 引擎注册失败: %v\n", err)
			return err
		}

		log.Printf("✅ GeoPandas 引擎已注册: %s (API: %s)\n", uniqueIdentifier, apiURL)
		return nil
	}

	if result.Error != nil {
		log.Printf("❌ 查询 GeoPandas 引擎失败: %v\n", result.Error)
		return result.Error
	}

	// 如果资源已存在，更新 API 地址和 capabilities（保持幂等性）
	resource.ConnectionInfo["api_url"] = apiURL

	// 确保 capabilities 已设置（兼容旧数据）
	if resource.Capabilities == nil || *resource.Capabilities == "" {
		capabilitiesJSON := `{"compute":[{"type":"spatial","dev_modes":["workflow"],"description":"空间计算引擎","features":["geopandas","dag","memory_efficient"]}]}`
		resource.Capabilities = &capabilitiesJSON
	}

	if err := db.Save(&resource).Error; err != nil {
		log.Printf("❌ 更新 GeoPandas 引擎失败: %v\n", err)
		return err
	}

	log.Printf("ℹ️  GeoPandas 引擎已存在并更新: %s (API: %s)\n", uniqueIdentifier, apiURL)
	return nil
}
