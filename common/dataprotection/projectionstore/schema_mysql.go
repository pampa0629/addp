package projectionstore

import (
	"fmt"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const mysqlProjectionOwner = "protection_projection_store"
const mysqlProjectionVersion int64 = 1

// The component revision and its tables belong to the same physical owner database.
func (s *Store) requireMySQLDatabase(db *gorm.DB) error {
	var databaseName string
	if err := db.Raw("SELECT DATABASE()").Scan(&databaseName).Error; err != nil {
		return err
	}
	if databaseName != s.schema {
		return fmt.Errorf("projection schema %q does not match connected MySQL database %q", s.schema, databaseName)
	}
	return nil
}

func (s *Store) prepareMySQL() error {
	if err := s.requireMySQLDatabase(s.db); err != nil {
		return err
	}
	return schema.MigrateMySQLVerified(s.db, mysqlProjectionOwner, mysqlProjectionVersion, func(db *gorm.DB) error {
		// Both DDLs can be replayed after MySQL commits a partial installation.
		if err := db.Exec(`CREATE TABLE IF NOT EXISTS protection_projection_entries (
			tenant_id BIGINT NOT NULL,
			projection_id VARCHAR(64) NOT NULL,
			consumer_owner VARCHAR(32) NOT NULL,
			target_owner_module VARCHAR(32) NOT NULL,
			target_resource_type VARCHAR(64) NOT NULL,
			target_resource_identity LONGTEXT NOT NULL,
			target_component_key LONGTEXT NOT NULL,
			state VARCHAR(16) NOT NULL,
			revision CHAR(20) NOT NULL,
			projection_payload JSON NOT NULL,
			updated_at DATETIME(6) NOT NULL,
			PRIMARY KEY (tenant_id, projection_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`).Error; err != nil {
			return err
		}
		return db.Exec("CREATE TABLE IF NOT EXISTS protection_projection_checkpoints (" +
			"tenant_id BIGINT PRIMARY KEY, `cursor` LONGTEXT NOT NULL, updated_at DATETIME(6) NOT NULL, " +
			"initialized BOOLEAN NOT NULL DEFAULT FALSE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin").Error
	}, verifyMySQLStore)
}

// Both tables must roll back with the owner's barrier. A checkpoint is unique per
// tenant and a projection ID is case-sensitive and unique within its tenant.
func verifyMySQLStore(db *gorm.DB) error {
	var rows []struct {
		Engine         string
		PrimaryColumns string
		BinaryID       bool
	}
	if err := db.Raw(`SELECT t.engine,
		(SELECT GROUP_CONCAT(s.column_name ORDER BY s.seq_in_index) FROM information_schema.statistics s
		 WHERE s.table_schema=t.table_schema AND s.table_name=t.table_name AND s.index_name='PRIMARY') AS primary_columns,
		EXISTS(SELECT 1 FROM information_schema.columns c WHERE c.table_schema=t.table_schema
		 AND c.table_name=t.table_name AND c.column_name='projection_id' AND c.collation_name='utf8mb4_0900_bin') AS binary_id
		FROM information_schema.tables t WHERE t.table_schema=DATABASE()
		AND t.table_name IN ('protection_projection_checkpoints','protection_projection_entries')
		ORDER BY t.table_name`).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) != 2 || rows[0].Engine != "InnoDB" || rows[0].PrimaryColumns != "tenant_id" ||
		rows[1].Engine != "InnoDB" || rows[1].PrimaryColumns != "tenant_id,projection_id" || !rows[1].BinaryID {
		return fmt.Errorf("MySQL protection projection tables require InnoDB and tenant-scoped primary keys with binary projection IDs")
	}
	return nil
}
