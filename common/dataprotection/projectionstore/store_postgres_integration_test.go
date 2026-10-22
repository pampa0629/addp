package projectionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
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

// Future validity is a defensive protocol state, not a configurable product
// start time. Install through the protocol's explicit time argument, reopen
// from PostgreSQL, then read at wall-clock time; no process/host clock is changed.
func TestProjectionStoreFutureReasonHistoryAgainstPostgres(t *testing.T) {
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
	const schema = "projection_future_reason_it"
	dropProjectionStoreTestSchema(t, db, schema)
	t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
	store, err := Migrate(db, schema, "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	initializeTenant(t, store, 7)
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
	projection := activeTableProjection(t, "develop", "query", model, path, fields)
	projection.ValidFrom = time.Now().UTC().Add(time.Hour)
	projection.ExpiresAt = nil
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{ChangeID: "future", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "future"}
	if err := store.ApplyBatch(t.Context(), 7, "", batch, projection.ValidFrom); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(db, schema, "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	protected, refusal := reader.PrepareTableProtection(t.Context(), 7, model, path, fields, "query", time.Now().UTC())
	if protected != nil || !errors.Is(refusal, dataprotection.ErrNotYetValid) {
		t.Fatalf("future projection did not refuse preparation: %v", refusal)
	}
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	t.Cleanup(func() {
		if err := db.Where("execution_id = ?", id).Delete(&execution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
		var remaining int64
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id = ?", id).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("future diagnostic execution cleanup: count=%d, error=%v", remaining, err)
		}
	})
	reason := execution.ProtectionFailureReason(refusal)
	if reason != "protection_not_yet_valid" {
		t.Fatalf("wrong future reason: %s", reason)
	}
	row := execution.TaskExecution{ExecutionID: id, TenantID: 7, Module: "develop", Source: "develop", TaskType: "query",
		Status: "failed", TriggerType: "manual", ErrorDetails: models.JSONMap{"category": "execution_failed",
			"code": "develop.query.protection_required", "diagnostic": execution.NewFailureDiagnostic("preparation", reason)}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	var history execution.TaskExecution
	if err := db.Where("execution_id = ?", id).First(&history).Error; err != nil {
		t.Fatal(err)
	}
	observed := execution.Observe(&history)
	payload, err := json.Marshal(observed.ErrorDetails)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"reason":"protection_not_yet_valid"`) ||
		!strings.Contains(string(payload), `"stage":"preparation"`) || history.Metadata["result"] != nil {
		t.Fatal("future reason history lost the diagnostic or retained a result")
	}
	// Recover through a newer normal protocol revision; the refused historical
	// execution remains unchanged and no clock is advanced to make it pass.
	projection.Revision = "00000000000000000002"
	projection.ValidFrom = time.Now().UTC().Add(-time.Minute)
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	batch.Changes[0].ChangeID, batch.NextCursor = "restored", "restored"
	if err := reader.ApplyBatch(t.Context(), 7, "future", batch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	protected, err = reader.PrepareTableProtection(t.Context(), 7, model, path, fields, "query", time.Now().UTC())
	if err != nil || protected == nil {
		t.Fatalf("normal revision did not recover: %v", err)
	}
	result := &plugin.QueryResult{Columns: []string{"phone"}, Rows: []map[string]interface{}{{"phone": "13812345678"}}}
	if err := protected.Apply(result); err != nil {
		t.Fatal(err)
	}
	if result.Rows[0]["phone"] != "138****5678" {
		t.Fatal("recovered output was not masked")
	}
	t.Log("future validity: persisted projection reopened; preparation refused; execution history kept precise reason/no result; newer revision recovered masked output")
}

// These are owner-local protocol tests backed by the real projection database.
// PreparedQuery evidence and the compiler refusal are controlled protocol inputs,
// not claims that a native Provider or an HTTP outlet was exercised.
type reasonMatrixUnsupportedQuery struct{ plugin.PreparedQuery }

func (reasonMatrixUnsupportedQuery) BindOutputTransforms(context.Context, []plugin.QueryOutputTransform) error {
	return plugin.ErrQueryOutputTransformUnsupported
}

func TestProjectionStoreReasonMatrixAgainstPostgres(t *testing.T) {
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
	owners := []struct{ owner, action string }{{"manager", "preview"}, {"develop", "query"}, {"service", "service_execute"}, {"transfer", "export"}}
	reasons := []string{"protection_not_ready", "protection_expired", "protection_not_yet_valid", "protection_schema_mismatch", "protection_lineage_unverified", "protection_computed_output_unsupported", "protection_operation_unsupported"}
	for _, outlet := range owners {
		t.Run(outlet.owner, func(t *testing.T) {
			for index, reason := range reasons {
				t.Run(reason, func(t *testing.T) {
					schema := fmt.Sprintf("projection_reason_matrix_%s_%d", outlet.owner, index)
					dropProjectionStoreTestSchema(t, db, schema)
					t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
					store, err := Migrate(db, schema, outlet.owner, nil)
					if err != nil {
						t.Fatal(err)
					}
					initializeTenant(t, store, 7)
					model := plugin.DynamicSchemaCatalogModel()
					path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
					fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
					now := time.Now().UTC()
					normal := activeTableProjection(t, outlet.owner, outlet.action, model, path, fields)
					normal.ValidFrom, normal.ExpiresAt = now.Add(-time.Minute), nil
					projection := normal
					installedAt := now
					switch reason {
					case "protection_not_ready":
						projection.State, projection.Rules, projection.SourceSnapshotHash = dataprotection.ProjectionStateEnrolling, []dataprotection.Rule{}, ""
					case "protection_not_yet_valid":
						projection.ValidFrom = now.Add(time.Hour)
						installedAt = projection.ValidFrom
					case "protection_expired":
						projection.ValidFrom = now.Add(-2 * time.Hour)
						projection.ExpiresAt = dataprotection.ProjectionDeadline(now.Add(-time.Hour))
						installedAt = now.Add(-90 * time.Minute)
					}
					if err := projection.Seal(); err != nil {
						t.Fatal(err)
					}
					batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: "matrix", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "matrix"}
					if err := store.ApplyBatch(t.Context(), 7, "", batch, installedAt); err != nil {
						t.Fatal(err)
					}
					reader, err := Open(db, schema, outlet.owner, nil)
					if err != nil {
						t.Fatal(err)
					}
					currentFields := append([]datatype.FieldInfo(nil), fields...)
					if reason == "protection_schema_mismatch" {
						currentFields = append(currentFields, datatype.FieldInfo{Name: "extra", Path: []string{"extra"}, Type: datatype.FieldTypeString, Nullable: true})
					}
					source := plugin.QueryOutputSource{Path: path, Fields: currentFields, IdentityOutput: true}
					if reason == "protection_computed_output_unsupported" {
						source.IdentityOutput = false
						source.Bindings = []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{"phone"}, Transformation: plugin.QueryOutputTransformationDerived}}
					}
					var sources []plugin.QueryOutputSource
					if reason != "protection_lineage_unverified" {
						sources = []plugin.QueryOutputSource{source}
					}
					executed := false
					prepared, err := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "mql", SchemaCoverage: plugin.QuerySchemaCoverageUnknown}, nil,
						func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
						func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
							return &plugin.QueryOutputLineage{Sources: sources}, nil
						},
						func(context.Context) (*plugin.QueryResult, error) { executed = true; return nil, nil })
					if err != nil {
						t.Fatal(err)
					}
					if reason == "protection_operation_unsupported" || reason == "protection_computed_output_unsupported" {
						prepared = reasonMatrixUnsupportedQuery{prepared}
					}
					protected, refusal := reader.PrepareQueryProtection(t.Context(), 7, model, prepared, outlet.action, dataprotection.SubjectReference{}, nil, now)
					if protected != nil || refusal == nil || execution.ProtectionFailureReason(refusal) != reason || executed {
						t.Fatalf("protocol refusal: want=%s got=%s error=%v executed=%t", reason, execution.ProtectionFailureReason(refusal), refusal, executed)
					}
					normal.Revision = "00000000000000000002"
					if err := normal.Seal(); err != nil {
						t.Fatal(err)
					}
					batch.Changes[0].ChangeID, batch.Changes[0].Projection, batch.NextCursor = "restore", &normal, "restore"
					if err := reader.ApplyBatch(t.Context(), 7, "matrix", batch, now); err != nil {
						t.Fatal(err)
					}
					protected, err = reader.PrepareTableProtection(t.Context(), 7, model, path, fields, outlet.action, now)
					if err != nil || protected == nil {
						t.Fatalf("normal revision did not recover: %v", err)
					}
					result := &plugin.QueryResult{Columns: []string{"phone"}, Rows: []map[string]interface{}{{"phone": "13812345678"}}}
					if err := protected.Apply(result); err != nil {
						t.Fatal(err)
					}
					if result.Rows[0]["phone"] != "138****5678" {
						t.Fatal("normal revision did not mask")
					}
				})
			}
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
	if len(versions) != 5 || versions[4] != "005_projection_role_options_v5" || versions[3] != "004_projection_engine_scope_v4" || versions[2] != "003_projection_validity_v3" || versions[0] != initialProjectionStoreMigration || versions[1] != keepPrefixSuffixV2StoreMigration {
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
