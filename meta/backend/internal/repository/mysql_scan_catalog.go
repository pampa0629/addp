package repository

import (
	"embed"
	"fmt"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

//go:embed mysql_scan_catalog/*.sql
var mysqlScanCatalogDDL embed.FS

// MigrateMySQLScanCatalog installs scan facts, independently of the complete Meta revision.
func MigrateMySQLScanCatalog(db *gorm.DB) error {
	var target string
	if err := db.Raw("SELECT DATABASE()").Scan(&target).Error; err != nil {
		return err
	}
	if target != "meta" {
		return fmt.Errorf("Meta scan catalog installation requires the meta database")
	}
	return schema.MigrateMySQL(db, "meta_scan_catalog", 1, func(conn *gorm.DB) error {
		entries, err := mysqlScanCatalogDDL.ReadDir("mysql_scan_catalog")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			ddl, err := mysqlScanCatalogDDL.ReadFile("mysql_scan_catalog/" + entry.Name())
			if err != nil {
				return err
			}
			if err := conn.Exec(string(ddl)).Error; err != nil {
				return fmt.Errorf("install %s: %w", entry.Name(), err)
			}
		}
		return nil
	})
}
