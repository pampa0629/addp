package projectionstore

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
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

	owners := []string{"manager", "develop", "service", "transfer", "graph", "future_owner"}
	for _, owner := range owners {
		schema := "projection_store_" + owner + "_it"
		dropProjectionStoreTestSchema(t, db, schema)
		t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
		store, err := Migrate(db, schema, owner, nil)
		if err != nil {
			t.Fatalf("initialize %s projection store: %v", owner, err)
		}
		assertCurrentMigrations(t, db, store)
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SET TRANSACTION READ ONLY").Error; err != nil {
				return err
			}
			_, err := Open(tx, schema, owner, nil)
			return err
		}); err != nil {
			t.Fatalf("Worker read-only Open: %v", err)
		}
		if _, err := Migrate(db, schema, owner, nil); err != nil {
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
	legacyProjection := postgresLegacyStructuredMaskProjection(t)
	legacyPayload, err := json.Marshal(legacyProjection)
	if err != nil {
		t.Fatalf("encode legacy projection: %v", err)
	}
	if err := db.Table(legacyStore.entriesTable).Create(&projectionRow{
		TenantID: 7, ProjectionID: legacyProjection.ProjectionID, ConsumerOwner: legacyProjection.ConsumerOwner,
		TargetOwner: legacyProjection.Target.OwnerModule, TargetType: legacyProjection.Target.ResourceType,
		TargetIdentity: legacyProjection.Target.ResourceIdentity, State: legacyProjection.State,
		Revision: legacyProjection.Revision, ProjectionPayload: string(legacyPayload), UpdatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("persist legacy projection: %v", err)
	}
	if _, err := Migrate(db, legacySchema, "legacy_owner", nil); err != nil {
		t.Fatalf("adopt legacy projection store: %v", err)
	}
	assertCurrentMigrations(t, db, legacyStore)
	replayStore, err := Open(db, legacySchema, "legacy_owner", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayStore.EnsureCurrent(t.Context(), 7); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("legacy owner admitted before replay: %v", err)
	}
	var count int64
	if err := db.Table(legacyStore.entriesTable).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("legacy rows remain: %d, %v", count, err)
	}

	driftSchema := "projection_store_drift_it"
	dropProjectionStoreTestSchema(t, db, driftSchema)
	t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, driftSchema) })
	if _, err := Migrate(db, driftSchema, "drift_owner", nil); err != nil {
		t.Fatalf("initialize drift projection store: %v", err)
	}
	if err := db.Exec("ALTER TABLE " + driftSchema + ".protection_projection_entries ADD COLUMN owner_private_value TEXT").Error; err != nil {
		t.Fatalf("introduce test schema drift: %v", err)
	}
	if _, err := Migrate(db, driftSchema, "drift_owner", nil); err == nil || !strings.Contains(err.Error(), "schema drift") {
		t.Fatalf("drifted projection store error = %v", err)
	}
}

func TestProjectionStoreResultExpiryAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for owner, action := range map[string]string{"manager": "preview", "develop": "query", "service": "service_execute", "transfer": "export"} {
		t.Run(owner, func(t *testing.T) {
			schema := "projection_expiry_" + owner + "_it"
			dropProjectionStoreTestSchema(t, db, schema)
			t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
			store, err := Migrate(db, schema, owner, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertBoundedProjectionMigration(t, store)
			if err := store.db.Exec("DELETE FROM " + store.entriesTable).Error; err != nil {
				t.Fatal(err)
			}
			if err := store.db.Exec("DELETE FROM " + store.checkpointTable).Error; err != nil {
				t.Fatal(err)
			}
			store, err = Migrate(db, schema, owner, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertPreparedOutputExpiry(t, store, owner, action)
		})
	}
}

func postgresLegacyStructuredMaskProjection(t *testing.T) dataprotection.Projection {
	t.Helper()
	now := time.Now().UTC()
	projection := dataprotection.Projection{
		TargetEngineID: 1,
		SchemaVersion:  "addp.protection_projection/v1",
		ProjectionID:   "11111111-1111-1111-1111-111111111111",
		Revision:       "00000000000000000001",
		ConsumerOwner:  "legacy_owner",
		State:          dataprotection.ProjectionStateActive,
		Target: dataprotection.ResourceReference{
			OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:postgres-legacy-mask",
		},
		SourceSnapshotHash: "sha256:postgres-legacy-mask-snapshot",
		Rules: []dataprotection.Rule{{
			Action: "preview",
			Component: dataprotection.Component{
				Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}},
				ValueType: "string", SchemaFingerprint: "sha256:postgres-legacy-mask-schema",
			},
			Decision: dataprotection.Decision{
				Effect: dataprotection.EffectMask, Algorithm: "addp.mask.keep_prefix_suffix/v1",
				Parameters: map[string]any{
					"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit",
				},
				InvalidValueEffect: dataprotection.EffectSuppress,
			},
		}},
		ValidFrom: now.Add(-time.Hour), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)),
	}
	if err := projection.Seal(); err != nil {
		t.Fatalf("seal legacy projection: %v", err)
	}
	return projection
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
			_, err := Migrate(db, schema, "concurrent_owner", nil)
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
	assertCurrentMigrations(t, db, store)
}

func assertCurrentMigrations(t *testing.T, db *gorm.DB, store *Store) {
	t.Helper()
	var versions []string
	if err := db.Table(store.migrationsTable).Order("version").Pluck("version", &versions).Error; err != nil {
		t.Fatalf("read projection store migration versions: %v", err)
	}
	if len(versions) != 4 || versions[3] != "004_projection_engine_scope_v4" || versions[2] != "003_projection_validity_v3" || versions[0] != initialProjectionStoreMigration || versions[1] != keepPrefixSuffixV2StoreMigration {
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
