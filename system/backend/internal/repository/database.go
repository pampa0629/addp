package repository

import (
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/schema"
	"github.com/addp/system/internal/config"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(cfg *config.Config) (*gorm.DB, error) {
	connection := database.Config{
		Host: cfg.PostgresHost, Port: cfg.PostgresPort, User: cfg.PostgresUser,
		Password: cfg.PostgresPassword, DBName: cfg.PostgresDB, Schema: "system",
	}
	db, err := bootstrap.OpenDatabase(connection, &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	if err := schema.EnsureNamespace(db, "system"); err != nil {
		return nil, err
	}
	return db, nil
}

func AutoMigrateNonIAM(db *gorm.DB) error {
	if err := schema.InitializeCommon(db); err != nil {
		return err
	}
	return nil
}
