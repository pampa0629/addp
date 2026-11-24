package repository

import (
	_ "embed"
	"strings"

	"github.com/addp/common/schema"

	"gorm.io/gorm"
)

//go:embed content_index.mysql.sql
var mysqlContentIndexTables string

//go:embed data_profile.mysql.sql
var mysqlProfileTables string

func prepareManagerMySQLTables(db *gorm.DB, ddl string) error {
	for _, statement := range strings.Split(strings.TrimSuffix(strings.TrimSpace(ddl), ";"), ";") {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func prepareMySQLContentIndex(db *gorm.DB) error {
	if err := prepareManagerMySQLTables(db, mysqlContentIndexTables); err != nil {
		return err
	}
	return schema.CheckMySQLTriggers(db, []string{
		`CREATE TRIGGER IF NOT EXISTS search_index_insert
AFTER INSERT ON content_index_deliveries FOR EACH ROW
UPDATE manager.content_index_outlets SET search_revision = search_revision + 1 WHERE index_name = NEW.index_name`,
		`CREATE TRIGGER IF NOT EXISTS search_index_update
AFTER UPDATE ON content_index_deliveries FOR EACH ROW
UPDATE manager.content_index_outlets SET search_revision = search_revision + 1 WHERE index_name IN (OLD.index_name, NEW.index_name)`,
		`CREATE TRIGGER IF NOT EXISTS search_index_delete
AFTER DELETE ON content_index_deliveries FOR EACH ROW
UPDATE manager.content_index_outlets SET search_revision = search_revision + 1 WHERE index_name = OLD.index_name`,
	})
}
