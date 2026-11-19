package middleware

import (
	"fmt"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const (
	postgresAccessLogTable = "gateway.api_access_logs"
	mysqlAccessLogTable    = "api_access_logs"
	mysqlAccessLogVersion  = 1
)

// MigrateAccessLog prepares the Gateway-owned access log in the selected database.
func MigrateAccessLog(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "postgres":
		if err := db.Exec(`
			ALTER TABLE IF EXISTS gateway.api_access_logs DROP COLUMN IF EXISTS application_id;
			ALTER TABLE IF EXISTS gateway.api_access_logs DROP COLUMN IF EXISTS api_key_prefix;
		`).Error; err != nil {
			return fmt.Errorf("remove obsolete Gateway access log columns: %w", err)
		}
		return db.AutoMigrate(&AccessLog{})
	case "mysql":
		return schema.MigrateMySQL(db, "gateway", mysqlAccessLogVersion, func(conn *gorm.DB) error {
			return conn.Exec(`CREATE TABLE IF NOT EXISTS api_access_logs (
				id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
				api_consumer_id BIGINT UNSIGNED,
				api_credential_prefix VARCHAR(20),
				service_name VARCHAR(255),
				request_method VARCHAR(10),
				request_path TEXT,
				request_params JSON,
				response_status BIGINT,
				response_time_ms BIGINT,
				cache_hit BOOLEAN DEFAULT FALSE,
				rate_limited BOOLEAN DEFAULT FALSE,
				accessed_at DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6)
			) ENGINE=InnoDB`).Error
		})
	default:
		return fmt.Errorf("unsupported Gateway access log database %q", db.Dialector.Name())
	}
}
