package projectionstore

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

const initialProjectionStoreMigration = "001_initial_projection_store"

type storeMigration struct {
	version string
	apply   func(*gorm.DB, *Store) error
}

var storeMigrations = []storeMigration{
	{version: initialProjectionStoreMigration, apply: createInitialProjectionStore},
}

type postgresColumnDefinition struct {
	Name              string `gorm:"column:name"`
	DataType          string `gorm:"column:data_type"`
	NotNull           bool   `gorm:"column:not_null"`
	DefaultExpression string `gorm:"column:default_expression"`
}

var expectedPostgresProjectionStoreColumns = map[string][]postgresColumnDefinition{
	"protection_projection_entries": {
		{Name: "tenant_id", DataType: "bigint", NotNull: true},
		{Name: "projection_id", DataType: "character varying(64)", NotNull: true},
		{Name: "consumer_owner", DataType: "character varying(32)", NotNull: true},
		{Name: "target_owner_module", DataType: "character varying(32)", NotNull: true},
		{Name: "target_resource_type", DataType: "character varying(64)", NotNull: true},
		{Name: "target_resource_identity", DataType: "text", NotNull: true},
		{Name: "target_component_key", DataType: "text", NotNull: true, DefaultExpression: "''::text"},
		{Name: "state", DataType: "character varying(16)", NotNull: true},
		{Name: "revision", DataType: "character(20)", NotNull: true},
		{Name: "projection_payload", DataType: "jsonb", NotNull: true},
		{Name: "updated_at", DataType: "timestamp without time zone", NotNull: true},
	},
	"protection_projection_checkpoints": {
		{Name: "tenant_id", DataType: "bigint", NotNull: true},
		{Name: "cursor", DataType: "text", NotNull: true},
		{Name: "updated_at", DataType: "timestamp without time zone", NotNull: true},
	},
	"protection_projection_store_migrations": {
		{Name: "version", DataType: "character varying(255)", NotNull: true},
		{Name: "applied_at", DataType: "timestamp with time zone", NotNull: true, DefaultExpression: "now()"},
	},
}

func (s *Store) ensureSchema() error {
	if s.db.Dialector.Name() == "postgres" {
		return s.db.Transaction(func(tx *gorm.DB) error {
			lockKey := "addp.protection_projection_store/" + s.schema
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
				return fmt.Errorf("acquire protection projection store migration lock: %w", err)
			}
			return s.ensureSchemaLocked(tx)
		})
	}
	return s.db.Transaction(s.ensureSchemaLocked)
}

func (s *Store) ensureSchemaLocked(tx *gorm.DB) error {
	if tx.Dialector.Name() == "postgres" {
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS " + s.schema).Error; err != nil {
			return fmt.Errorf("create protection projection owner schema: %w", err)
		}
	}
	if err := s.ensureMigrationTable(tx); err != nil {
		return err
	}
	if err := validateStoreMigrations(storeMigrations); err != nil {
		return err
	}
	known := make(map[string]storeMigration, len(storeMigrations))
	for _, migration := range storeMigrations {
		known[migration.version] = migration
	}
	applied, err := s.appliedMigrations(tx)
	if err != nil {
		return err
	}
	for version := range applied {
		if _, ok := known[version]; !ok {
			return fmt.Errorf("unknown protection projection store migration %q", version)
		}
	}
	for _, migration := range storeMigrations {
		if _, ok := applied[migration.version]; ok {
			continue
		}
		if err := migration.apply(tx, s); err != nil {
			return fmt.Errorf("apply protection projection store migration %s: %w", migration.version, err)
		}
		if err := tx.Exec("INSERT INTO "+s.migrationsTable+" (version) VALUES (?)", migration.version).Error; err != nil {
			return fmt.Errorf("record protection projection store migration %s: %w", migration.version, err)
		}
	}
	if tx.Dialector.Name() == "postgres" {
		if err := s.verifyPostgresSchema(tx); err != nil {
			return err
		}
	}
	return nil
}

func validateStoreMigrations(migrations []storeMigration) error {
	if len(migrations) == 0 {
		return errors.New("protection projection store migrations are empty")
	}
	versions := make([]string, len(migrations))
	for index, migration := range migrations {
		if strings.TrimSpace(migration.version) == "" || migration.apply == nil {
			return errors.New("invalid protection projection store migration")
		}
		versions[index] = migration.version
	}
	sorted := append([]string(nil), versions...)
	sort.Strings(sorted)
	for index := range versions {
		if versions[index] != sorted[index] || (index > 0 && versions[index] == versions[index-1]) {
			return errors.New("protection projection store migrations must be unique and ordered")
		}
	}
	return nil
}

func (s *Store) ensureMigrationTable(tx *gorm.DB) error {
	statement := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version VARCHAR(255) PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`, s.migrationsTable)
	if tx.Dialector.Name() == "postgres" {
		statement = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.migrationsTable)
	}
	if err := tx.Exec(statement).Error; err != nil {
		return fmt.Errorf("ensure protection projection store migration table: %w", err)
	}
	return nil
}

func (s *Store) appliedMigrations(tx *gorm.DB) (map[string]struct{}, error) {
	var versions []string
	if err := tx.Table(s.migrationsTable).Order("version").Pluck("version", &versions).Error; err != nil {
		return nil, fmt.Errorf("read protection projection store migrations: %w", err)
	}
	result := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		result[version] = struct{}{}
	}
	return result, nil
}

func createInitialProjectionStore(tx *gorm.DB, store *Store) error {
	payloadType := "TEXT"
	if tx.Dialector.Name() == "postgres" {
		payloadType = "JSONB"
	}
	indexStatement := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_protection_projection_target ON %s
		(tenant_id, target_owner_module, target_resource_type, target_resource_identity)`, store.consumerOwner, store.entriesTable)
	if tx.Dialector.Name() == "sqlite" {
		_, table, _ := strings.Cut(store.entriesTable, ".")
		indexStatement = fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s.idx_%s_protection_projection_target ON %s
			(tenant_id, target_owner_module, target_resource_type, target_resource_identity)`, store.schema, store.consumerOwner, table)
	}
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			tenant_id BIGINT NOT NULL,
			projection_id VARCHAR(64) NOT NULL,
			consumer_owner VARCHAR(32) NOT NULL,
			target_owner_module VARCHAR(32) NOT NULL,
			target_resource_type VARCHAR(64) NOT NULL,
			target_resource_identity TEXT NOT NULL,
			target_component_key TEXT NOT NULL DEFAULT '',
			state VARCHAR(16) NOT NULL,
			revision CHAR(20) NOT NULL,
			projection_payload %s NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (tenant_id, projection_id)
		)`, store.entriesTable, payloadType),
		indexStatement,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			tenant_id BIGINT PRIMARY KEY,
			cursor TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`, store.checkpointTable),
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) verifyPostgresSchema(tx *gorm.DB) error {
	for table, expected := range expectedPostgresProjectionStoreColumns {
		if err := verifyPostgresColumns(tx, s.schema, table, expected); err != nil {
			return err
		}
	}
	if err := verifyPostgresPrimaryKey(tx, s.schema, "protection_projection_entries", "tenant_id,projection_id"); err != nil {
		return err
	}
	if err := verifyPostgresPrimaryKey(tx, s.schema, "protection_projection_checkpoints", "tenant_id"); err != nil {
		return err
	}
	if err := verifyPostgresPrimaryKey(tx, s.schema, "protection_projection_store_migrations", "version"); err != nil {
		return err
	}
	if err := verifyPostgresTargetIndex(tx, s.schema); err != nil {
		return err
	}
	for table, expectedIndexCount := range map[string]int64{
		"protection_projection_entries":          2,
		"protection_projection_checkpoints":      1,
		"protection_projection_store_migrations": 1,
	} {
		if err := verifyPostgresObjectCount(tx, s.schema, table, "indexes", expectedIndexCount, `
			SELECT COUNT(*)
			FROM pg_catalog.pg_index index_record
			JOIN pg_catalog.pg_class relation ON relation.oid = index_record.indrelid
			JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = ? AND relation.relname = ?`); err != nil {
			return err
		}
		if err := verifyPostgresObjectCount(tx, s.schema, table, "constraints", 1, `
			SELECT COUNT(*)
			FROM pg_catalog.pg_constraint constraint_record
			JOIN pg_catalog.pg_class relation ON relation.oid = constraint_record.conrelid
			JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = ? AND relation.relname = ?`); err != nil {
			return err
		}
	}
	return nil
}

func verifyPostgresColumns(tx *gorm.DB, schema, table string, expected []postgresColumnDefinition) error {
	var actual []postgresColumnDefinition
	if err := tx.Raw(`
		SELECT attribute.attname AS name,
		       pg_catalog.format_type(attribute.atttypid, attribute.atttypmod) AS data_type,
		       attribute.attnotnull AS not_null,
		       COALESCE(pg_get_expr(default_value.adbin, default_value.adrelid), '') AS default_expression
		FROM pg_catalog.pg_attribute attribute
		JOIN pg_catalog.pg_class relation ON relation.oid = attribute.attrelid
		JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
		LEFT JOIN pg_catalog.pg_attrdef default_value
		  ON default_value.adrelid = relation.oid AND default_value.adnum = attribute.attnum
		WHERE namespace.nspname = ? AND relation.relname = ?
		  AND relation.relkind IN ('r', 'p')
		  AND attribute.attnum > 0 AND NOT attribute.attisdropped
		ORDER BY attribute.attnum`, schema, table).Scan(&actual).Error; err != nil {
		return fmt.Errorf("inspect protection projection store table %s.%s: %w", schema, table, err)
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("protection projection store schema drift in %s.%s: column count %d, want %d", schema, table, len(actual), len(expected))
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf("protection projection store schema drift in %s.%s column %d: got %+v, want %+v", schema, table, index+1, actual[index], expected[index])
		}
	}
	return nil
}

func verifyPostgresPrimaryKey(tx *gorm.DB, schema, table, expected string) error {
	var columns sql.NullString
	row := tx.Raw(`
		SELECT string_agg(attribute.attname, ',' ORDER BY key.ordinality)
		FROM pg_catalog.pg_constraint constraint_record
		CROSS JOIN LATERAL unnest(constraint_record.conkey) WITH ORDINALITY AS key(attnum, ordinality)
		JOIN pg_catalog.pg_attribute attribute
		  ON attribute.attrelid = constraint_record.conrelid AND attribute.attnum = key.attnum
		JOIN pg_catalog.pg_class relation ON relation.oid = constraint_record.conrelid
		JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = ? AND relation.relname = ? AND constraint_record.contype = 'p'
		GROUP BY constraint_record.oid`, schema, table).Row()
	if err := row.Scan(&columns); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("protection projection store schema drift in %s.%s: primary key is missing", schema, table)
		}
		return fmt.Errorf("inspect protection projection store primary key %s.%s: %w", schema, table, err)
	}
	if !columns.Valid || columns.String != expected {
		return fmt.Errorf("protection projection store schema drift in %s.%s: primary key %q, want %q", schema, table, columns.String, expected)
	}
	return nil
}

func verifyPostgresTargetIndex(tx *gorm.DB, schema string) error {
	var exists bool
	if err := tx.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_index index_record
			JOIN pg_catalog.pg_class relation ON relation.oid = index_record.indrelid
			JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
			JOIN pg_catalog.pg_class index_relation ON index_relation.oid = index_record.indexrelid
			JOIN pg_catalog.pg_am access_method ON access_method.oid = index_relation.relam
			WHERE namespace.nspname = ? AND relation.relname = 'protection_projection_entries'
			  AND NOT index_record.indisunique AND index_record.indisvalid
			  AND index_record.indpred IS NULL AND access_method.amname = 'btree'
			  AND (
				SELECT string_agg(attribute.attname, ',' ORDER BY key.ordinality)
				FROM unnest(index_record.indkey) WITH ORDINALITY AS key(attnum, ordinality)
				JOIN pg_catalog.pg_attribute attribute
				  ON attribute.attrelid = index_record.indrelid AND attribute.attnum = key.attnum
				WHERE key.ordinality <= index_record.indnkeyatts
			  ) = 'tenant_id,target_owner_module,target_resource_type,target_resource_identity'
		)`, schema).Scan(&exists).Error; err != nil {
		return fmt.Errorf("inspect protection projection target index in %s: %w", schema, err)
	}
	if !exists {
		return fmt.Errorf("protection projection store schema drift in %s: target lookup index is missing", schema)
	}
	return nil
}

func verifyPostgresObjectCount(tx *gorm.DB, schema, table, objectName string, expected int64, query string) error {
	var count int64
	if err := tx.Raw(query, schema, table).Scan(&count).Error; err != nil {
		return fmt.Errorf("inspect protection projection store %s in %s.%s: %w", objectName, schema, table, err)
	}
	if count != expected {
		return fmt.Errorf("protection projection store schema drift in %s.%s: %s count %d, want %d", schema, table, objectName, count, expected)
	}
	return nil
}
