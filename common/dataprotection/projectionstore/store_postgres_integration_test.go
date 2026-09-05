package projectionstore

import (
	"os"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestProjectionStoreSchemaContractAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}

	owners := []string{"manager", "develop", "service", "transfer", "future_owner"}
	for _, owner := range owners {
		schema := "projection_store_" + owner + "_it"
		dropProjectionStoreTestSchema(t, db, schema)
		t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
		store, err := New(db, schema, owner, nil)
		if err != nil {
			t.Fatalf("initialize %s projection store: %v", owner, err)
		}
		assertSingleCurrentMigration(t, db, store)
		if _, err := New(db, schema, owner, nil); err != nil {
			t.Fatalf("reopen %s projection store: %v", owner, err)
		}
	}

	legacySchema := "projection_store_legacy_it"
	dropProjectionStoreTestSchema(t, db, legacySchema)
	t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, legacySchema) })
	if err := db.Exec("CREATE SCHEMA " + legacySchema).Error; err != nil {
		t.Fatalf("create legacy projection store schema: %v", err)
	}
	legacyStore := &Store{
		db: db, schema: legacySchema, consumerOwner: "legacy_owner",
		entriesTable: legacySchema + ".protection_projection_entries", checkpointTable: legacySchema + ".protection_projection_checkpoints",
		migrationsTable: legacySchema + ".protection_projection_store_migrations",
	}
	if err := createInitialProjectionStore(db, legacyStore); err != nil {
		t.Fatalf("create legacy projection store tables: %v", err)
	}
	if _, err := New(db, legacySchema, "legacy_owner", nil); err != nil {
		t.Fatalf("adopt legacy projection store: %v", err)
	}
	assertSingleCurrentMigration(t, db, legacyStore)

	driftSchema := "projection_store_drift_it"
	dropProjectionStoreTestSchema(t, db, driftSchema)
	t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, driftSchema) })
	if _, err := New(db, driftSchema, "drift_owner", nil); err != nil {
		t.Fatalf("initialize drift projection store: %v", err)
	}
	if err := db.Exec("ALTER TABLE " + driftSchema + ".protection_projection_entries ADD COLUMN owner_private_value TEXT").Error; err != nil {
		t.Fatalf("introduce test schema drift: %v", err)
	}
	if _, err := New(db, driftSchema, "drift_owner", nil); err == nil || !strings.Contains(err.Error(), "schema drift") {
		t.Fatalf("drifted projection store error = %v", err)
	}
}

func TestProjectionStoreMigrationIsSerializedAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open SQL database: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)

	schema := "projection_store_concurrent_it"
	dropProjectionStoreTestSchema(t, db, schema)
	t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })

	start := make(chan struct{})
	errorsByProcess := make(chan error, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := New(db, schema, "concurrent_owner", nil)
			errorsByProcess <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByProcess)
	for err := range errorsByProcess {
		if err != nil {
			t.Fatalf("concurrent projection store initialization: %v", err)
		}
	}
	store := &Store{migrationsTable: schema + ".protection_projection_store_migrations"}
	assertSingleCurrentMigration(t, db, store)
}

func assertSingleCurrentMigration(t *testing.T, db *gorm.DB, store *Store) {
	t.Helper()
	var versions []string
	if err := db.Table(store.migrationsTable).Order("version").Pluck("version", &versions).Error; err != nil {
		t.Fatalf("read projection store migration versions: %v", err)
	}
	if len(versions) != 1 || versions[0] != initialProjectionStoreMigration {
		t.Fatalf("projection store migration versions = %#v", versions)
	}
}

func dropProjectionStoreTestSchema(t *testing.T, db *gorm.DB, schema string) {
	t.Helper()
	if !schemaNamePattern.MatchString(schema) {
		t.Fatalf("invalid test schema %q", schema)
	}
	if err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
		t.Fatalf("drop test schema %s: %v", schema, err)
	}
}
