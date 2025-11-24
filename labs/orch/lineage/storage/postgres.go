package storage

import (
	"fmt"
	"lineage/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgresConfig PostgreSQL 连接配置
type PostgresConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string // disable, require, verify-ca, verify-full
}

// NewPostgresDB 创建 PostgreSQL 数据库连接
func NewPostgresDB(config PostgresConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.Host,
		config.Port,
		config.User,
		config.Password,
		config.DBName,
		config.SSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// 自动迁移表结构
	if err := db.AutoMigrate(&models.DataLineage{}); err != nil {
		return nil, fmt.Errorf("failed to migrate PostgreSQL schema: %w", err)
	}

	return db, nil
}
