package repository

import (
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/system/internal/config"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(cfg *config.Config) (*gorm.DB, error) {
	connection := cfg.PlatformDatabase()
	connection.Schema = "system"
	return bootstrap.OpenPlatformRuntime(connection, "system", &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
}
