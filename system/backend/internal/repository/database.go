package repository

import (
	"log"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/pkg/utils"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}
	return db, nil
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