package migration

import (
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const mysqlRequestLocksOwner = "system_access_request_locks"
const mysqlRequestLocksVersion = 1

// MigrateMySQLAccessRequestLocks prepares the System-owned request lock store.
func MigrateMySQLAccessRequestLocks(db *gorm.DB) error {
	return schema.MigrateMySQL(db, mysqlRequestLocksOwner, mysqlRequestLocksVersion, func(conn *gorm.DB) error {
		return conn.Exec("CREATE TABLE IF NOT EXISTS `system`.`engine_access_request_locks` " +
			"(request_id BINARY(16) NOT NULL PRIMARY KEY) ENGINE=InnoDB").Error
	})
}
