package repository

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// MigrateCaptureProviderResources performs the one-way clean break from PostgreSQL-specific
// columns on capture_resources to provider-owned child facts before GORM AutoMigrate runs.
func MigrateCaptureProviderResources(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("capture schema migration database is not configured")
	}
	return migrateCaptureProviderResources(db, "transfer")
}

func migrateCaptureProviderResources(db *gorm.DB, schema string) error {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return fmt.Errorf("capture schema migration requires a schema")
	}
	var legacyColumns int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = ? AND table_name = 'capture_resources' AND column_name = 'slot_name'
	`, schema).Scan(&legacyColumns).Error; err != nil {
		return fmt.Errorf("inspect capture provider schema: %w", err)
	}
	qualified := func(table string) string { return quoteCaptureIdentifier(schema) + "." + quoteCaptureIdentifier(table) }
	captures := qualified("capture_resources")
	postgresqlResources := qualified("postgresql_capture_resources")
	mysqlResources := qualified("mysql_capture_resources")
	if legacyColumns == 0 {
		return normalizeCaptureProviderIndexes(db, schema, captures, postgresqlResources, mysqlResources)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		statements := []string{
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS source_type VARCHAR(32)`, captures),
			fmt.Sprintf(`UPDATE %s SET source_type = 'postgresql' WHERE source_type IS NULL`, captures),
			fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN source_type SET NOT NULL`, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS chk_transfer_capture_source_type`, captures),
			fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT chk_transfer_capture_source_type CHECK (source_type IN ('postgresql', 'mysql'))`, captures),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_transfer_capture_resources_source_type ON %s (source_type)`, captures),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				capture_resource_id BIGINT PRIMARY KEY,
				slot_name VARCHAR(63) NOT NULL,
				publication_name VARCHAR(63) NOT NULL,
				slot_owned BOOLEAN NOT NULL DEFAULT TRUE,
				publication_owned BOOLEAN NOT NULL DEFAULT TRUE,
				CONSTRAINT fk_transfer_capture_resources_postgre_sql FOREIGN KEY (capture_resource_id) REFERENCES %s(id) ON DELETE CASCADE
			)`, postgresqlResources, captures),
			fmt.Sprintf(`INSERT INTO %s (
				capture_resource_id, slot_name, publication_name, slot_owned, publication_owned
			)
			SELECT id, slot_name, publication_name, slot_owned, publication_owned
			FROM %s
			WHERE source_type = 'postgresql'
			ON CONFLICT (capture_resource_id) DO NOTHING`, postgresqlResources, captures),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				capture_resource_id BIGINT PRIMARY KEY,
				connector_server_id BIGINT NOT NULL,
				schema_history_topic_name VARCHAR(255) NOT NULL,
				schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE,
				CONSTRAINT fk_transfer_capture_resources_my_sql FOREIGN KEY (capture_resource_id) REFERENCES %s(id) ON DELETE CASCADE,
				CONSTRAINT chk_transfer_mysql_capture_server_id CHECK (connector_server_id BETWEEN 1 AND 4294967295)
			)`, mysqlResources, captures),
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS schema_history_topic_name VARCHAR(255)`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE`, mysqlResources),
			fmt.Sprintf(`UPDATE %s m SET schema_history_topic_name = '__addp_cdc_schema.' || c.tenant_id || '.' || c.task_id || '.' || c.generation FROM %s c WHERE c.id = m.capture_resource_id AND (m.schema_history_topic_name IS NULL OR m.schema_history_topic_name = '')`, mysqlResources, captures),
			fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN schema_history_topic_name SET NOT NULL`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_postgresql_capture_slot`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_postgresql_capture_publication`, postgresqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_slot ON %s (slot_name)`, postgresqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_publication ON %s (publication_name)`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_mysql_capture_server_id`, mysqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_server_id ON %s (connector_server_id)`, mysqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_schema_history_topic ON %s (schema_history_topic_name)`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS fk_transfer_capture_resources_postgre_sql`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT fk_transfer_capture_resources_postgre_sql FOREIGN KEY (capture_resource_id) REFERENCES %s(id) ON DELETE CASCADE`, postgresqlResources, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS postgresql_capture_resources_capture_resource_id_fkey`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS fk_transfer_capture_resources_my_sql`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT fk_transfer_capture_resources_my_sql FOREIGN KEY (capture_resource_id) REFERENCES %s(id) ON DELETE CASCADE`, mysqlResources, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS mysql_capture_resources_capture_resource_id_fkey`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_capture_slot`, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_capture_publication`, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP COLUMN slot_name`, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP COLUMN publication_name`, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP COLUMN slot_owned`, captures),
			fmt.Sprintf(`ALTER TABLE %s DROP COLUMN publication_owned`, captures),
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("migrate capture provider resources: %w", err)
			}
		}
		return nil
	})
}

func normalizeCaptureProviderIndexes(db *gorm.DB, schema, captures, postgresqlResources, mysqlResources string) error {
	var tables int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = ? AND table_name IN ('postgresql_capture_resources', 'mysql_capture_resources')
	`, schema).Scan(&tables).Error; err != nil {
		return fmt.Errorf("inspect capture provider tables: %w", err)
	}
	if tables != 2 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		statements := []string{
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS schema_history_topic_name VARCHAR(255)`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS schema_history_topic_owned BOOLEAN NOT NULL DEFAULT TRUE`, mysqlResources),
			fmt.Sprintf(`UPDATE %s m SET schema_history_topic_name = '__addp_cdc_schema.' || c.tenant_id || '.' || c.task_id || '.' || c.generation FROM %s c WHERE c.id = m.capture_resource_id AND (m.schema_history_topic_name IS NULL OR m.schema_history_topic_name = '')`, mysqlResources, captures),
			fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN schema_history_topic_name SET NOT NULL`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_postgresql_capture_slot`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_postgresql_capture_publication`, postgresqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_slot ON %s (slot_name)`, postgresqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_postgresql_capture_publication ON %s (publication_name)`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS uq_transfer_mysql_capture_server_id`, mysqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_server_id ON %s (connector_server_id)`, mysqlResources),
			fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS uq_transfer_mysql_capture_schema_history_topic ON %s (schema_history_topic_name)`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS fk_transfer_capture_resources_postgre_sql`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT fk_transfer_capture_resources_postgre_sql FOREIGN KEY (capture_resource_id) REFERENCES %s.%s(id) ON DELETE CASCADE`, postgresqlResources, quoteCaptureIdentifier(schema), quoteCaptureIdentifier("capture_resources")),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS postgresql_capture_resources_capture_resource_id_fkey`, postgresqlResources),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS fk_transfer_capture_resources_my_sql`, mysqlResources),
			fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT fk_transfer_capture_resources_my_sql FOREIGN KEY (capture_resource_id) REFERENCES %s.%s(id) ON DELETE CASCADE`, mysqlResources, quoteCaptureIdentifier(schema), quoteCaptureIdentifier("capture_resources")),
			fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS mysql_capture_resources_capture_resource_id_fkey`, mysqlResources),
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("normalize capture provider indexes: %w", err)
			}
		}
		return nil
	})
}

func quoteCaptureIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
