package repository

import (
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const mysqlScanTasksOwner = "meta_scan_tasks"
const mysqlScanTasksVersion = 1

func RequireMySQLScanTasks(db *gorm.DB) error {
	return schema.RequireMySQL(db, mysqlScanTasksOwner, mysqlScanTasksVersion)
}

// MigrateMySQLScanTasks prepares the Meta-owned scan task definitions and summary.
func MigrateMySQLScanTasks(db *gorm.DB) error {
	return schema.MigrateMySQL(db, mysqlScanTasksOwner, mysqlScanTasksVersion, func(conn *gorm.DB) error {
		return conn.Exec(`CREATE TABLE IF NOT EXISTS meta.scan_tasks (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			tenant_id BIGINT UNSIGNED NOT NULL,
			engine_id BIGINT UNSIGNED NOT NULL,
			name VARCHAR(128) NOT NULL,
			description VARCHAR(512),
			schedule VARCHAR(128),
			enabled BOOLEAN DEFAULT FALSE,
			scope JSON,
			parameters JSON,
			owner_module VARCHAR(50) NOT NULL DEFAULT 'meta',
			owner_ref VARCHAR(128),
			last_run_at DATETIME(6),
			next_run_at DATETIME(6),
			last_execution_id VARCHAR(36),
			last_execution_status VARCHAR(20),
			created_by BIGINT UNSIGNED,
			updated_by BIGINT UNSIGNED,
			created_at DATETIME(6),
			updated_at DATETIME(6),
			deleted_at DATETIME(6),
			KEY idx_scan_tasks_tenant_id (tenant_id),
			KEY idx_scan_tasks_engine_id (engine_id),
			KEY idx_scan_tasks_owner (owner_module, owner_ref),
			KEY idx_scan_tasks_deleted_at (deleted_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`).Error
	})
}
