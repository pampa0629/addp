package repository

import (
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/schema"
	"github.com/addp/system/internal/config"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(cfg *config.Config) (*gorm.DB, error) {
	connection := cfg.PlatformDatabase()
	connection.Schema = "system"
	db, err := bootstrap.OpenDatabase(connection, &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	if err := schema.EnsureNamespace(db, "system"); err != nil {
		return nil, err
	}
	return db, nil
}

// AutoMigrateNonIAM is the System startup boundary for shared execution and health storage.
func AutoMigrateNonIAM(db *gorm.DB) error {
	if db.Dialector.Name() == "mysql" {
		return schema.Require(db, "common", schema.CommonVersion)
	}
	return schema.InitializeCommon(db)
}
