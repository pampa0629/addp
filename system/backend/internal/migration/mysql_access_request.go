package migration

import (
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

// MigrateMySQLAccessRequestLocks prepares the System-owned request lock store.
func MigrateMySQLAccessRequestLocks(db *gorm.DB) error {
	return schema.MigrateMySQL(db, "system_access_request_locks", 1, func(conn *gorm.DB) error {
		return conn.Exec("CREATE TABLE IF NOT EXISTS `system`.`engine_access_request_locks` " +
			"(request_id BINARY(16) NOT NULL PRIMARY KEY) ENGINE=InnoDB").Error
	})
}
