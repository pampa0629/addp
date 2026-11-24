package exportartifact

import (
	"fmt"

	"gorm.io/gorm"
)

// The owner installer opens its physical database before preparing this store.
// Execution IDs and request digests have the same exact-match semantics as in PG.
func prepareMySQLExportSessions(db *gorm.DB, table string) error {
	var owner string
	if err := db.Raw("SELECT DATABASE()").Scan(&owner).Error; err != nil {
		return err
	}
	if (owner != "manager" && owner != "develop") || table != owner+".export_sessions" {
		return fmt.Errorf("export session table %q does not belong to connected MySQL database %q", table, owner)
	}
	return db.Exec(`CREATE TABLE IF NOT EXISTS export_sessions (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		tenant_id BIGINT UNSIGNED NOT NULL,
		user_id BIGINT UNSIGNED NOT NULL,
		source_item_locator TEXT NOT NULL,
		format VARCHAR(64) NOT NULL,
		file_name VARCHAR(512) NOT NULL,
		target_parent_locator TEXT NOT NULL,
		target_locator TEXT NOT NULL,
		protection JSON,
		artifact_manifest JSON,
		transfer_execution_id VARCHAR(64) NOT NULL,
		execution_request_digest VARCHAR(64) NOT NULL DEFAULT '',
		status VARCHAR(32) NOT NULL,
		error_message TEXT,
		created_at DATETIME(6),
		updated_at DATETIME(6),
		INDEX idx_export_sessions_scope_status_created (tenant_id,user_id,status,created_at DESC),
		INDEX idx_export_sessions_transfer_execution_id (transfer_execution_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`).Error
}
