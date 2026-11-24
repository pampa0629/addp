package repository

import (
	"github.com/addp/common/schema"

	"gorm.io/gorm"
)

func prepareMySQLContentIndex(db *gorm.DB) error {
	if err := prepareManagerMySQLTables(db, mysqlContentIndexTables); err != nil {
		return err
	}
	return schema.CheckMySQLTriggers(db, contentIndexMySQLTriggers())
}

func contentIndexMySQLTriggers() []string {
	return []string{
		`CREATE TRIGGER IF NOT EXISTS search_index_insert
AFTER INSERT ON content_index_deliveries FOR EACH ROW
UPDATE manager.content_index_outlets SET search_revision = search_revision + 1 WHERE index_name = NEW.index_name`,
		`CREATE TRIGGER IF NOT EXISTS search_index_update
AFTER UPDATE ON content_index_deliveries FOR EACH ROW
UPDATE manager.content_index_outlets SET search_revision = search_revision + 1 WHERE index_name IN (OLD.index_name, NEW.index_name)`,
		`CREATE TRIGGER IF NOT EXISTS search_index_delete
AFTER DELETE ON content_index_deliveries FOR EACH ROW
UPDATE manager.content_index_outlets SET search_revision = search_revision + 1 WHERE index_name = OLD.index_name`,
	}
}
