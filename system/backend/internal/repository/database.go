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
	)
}

// InitSuperAdmin 初始化超级管理员用户
func InitSuperAdmin(db *gorm.DB) error {
	// 检查SuperAdmin用户是否存在
	var user models.User
	result := db.Where("username = ?", "SuperAdmin").First(&user)

	if result.Error == gorm.ErrRecordNotFound {
		// 创建SuperAdmin用户
		passwordHash, err := utils.HashPassword("20251001#SuperAdmin")
		if err != nil {
			return err
		}

		superAdminUser := models.User{
			Username:     "SuperAdmin",
			Email:        "superadmin@addp.com",
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

		log.Println("超级管理员已创建: SuperAdmin / 20251001#SuperAdmin")
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
