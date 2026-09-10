package projectionstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type failingProjectionChangeBarrier struct{}

func (failingProjectionChangeBarrier) ApplyProjectionChanges(context.Context, *gorm.DB, int64, []dataprotection.ProjectionChange, time.Time) error {
	return errors.New("derived result cleanup failed")
}

func TestStoreGateIsSingleLocalMissForUnmanagedAndFailClosedForEnrolling(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:item"}
	if gate := store.Gate(7, target, time.Now().UTC()); gate.Managed || gate.Err != nil || len(gate.Projections) != 0 {
		t.Fatalf("unmanaged gate = %#v", gate)
	}

	projection := enrollingProjection(t, "manager", target)
	batch := &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-1",
	}
	if err := store.ApplyBatch(context.Background(), 7, "", batch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	gate := store.Gate(7, target, projection.ExpiresAt.Add(time.Hour))
	if !gate.Managed || gate.State != dataprotection.ProjectionStateEnrolling || gate.Err != nil {
		t.Fatalf("expired enrolling gate = %#v", gate)
	}
}

func TestStorePersistsCursorAndRequiresExplicitRelease(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:item"}
	projection := enrollingProjection(t, "manager", target)
	if err := store.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "cursor-1",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cursor, err := reloaded.CurrentCursor(context.Background(), 7); err != nil || cursor != "cursor-1" {
		t.Fatalf("cursor = %q, err = %v", cursor, err)
	}
	if !reloaded.Gate(7, target, time.Now().UTC()).Managed {
		t.Fatal("persisted projection must remain managed")
	}
	release := dataprotection.ProjectionRelease{ProjectionID: projection.ProjectionID, Revision: "00000000000000000002", Target: target}
	if err := reloaded.ApplyBatch(context.Background(), 7, "cursor-1", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "change-2", Operation: dataprotection.ChangeOperationRelease, Release: &release}},
		NextCursor:    "cursor-2",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if reloaded.Gate(7, target, time.Now().UTC()).Managed {
		t.Fatal("explicit release must remove the local managed-resource index")
	}
}

func TestStoreGateAnyKeepsAllUnmanagedOnLocalFastPath(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	unmanaged := []dataprotection.ResourceReference{
		{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-a"},
		{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-b"},
	}
	if match := store.GateAny(7, unmanaged, time.Now().UTC()); match != nil {
		t.Fatalf("unmanaged match = %#v", match)
	}

	managed := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-c"}
	projection := enrollingProjection(t, "manager", managed)
	if err := store.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{
			ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-1",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	targets := append(unmanaged, managed)
	match := store.GateAny(7, targets, time.Now().UTC())
	if match == nil || match.Target.ResourceIdentity != managed.ResourceIdentity || !match.Gate.Managed {
		t.Fatalf("managed match = %#v", match)
	}
}

func TestStoreChangeBarrierFailureRollsBackProjectionAndCursor(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", failingProjectionChangeBarrier{})
	if err != nil {
		t.Fatal(err)
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-a"}
	projection := enrollingProjection(t, "manager", target)
	err = store.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{
			ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-1",
	}, time.Now().UTC())
	if err == nil {
		t.Fatal("ApplyBatch() must fail when the owner barrier fails")
	}
	if cursor, cursorErr := store.CurrentCursor(context.Background(), 7); cursorErr != nil || cursor != "" {
		t.Fatalf("cursor = %q, err = %v", cursor, cursorErr)
	}
	if store.Gate(7, target, time.Now().UTC()).Managed {
		t.Fatal("rolled back projection must not enter the local index")
	}
}

func TestStoreManagedTargetsReturnsInstalledResourcesInStableOrder(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, identity := range []string{"item-b", "item-a"} {
		target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: identity}
		projection := enrollingProjection(t, "manager", target)
		projection.ProjectionID = "projection-" + identity
		if err := projection.Seal(); err != nil {
			t.Fatal(err)
		}
		cursor := ""
		if index > 0 {
			cursor = "cursor-1"
		}
		if err := store.ApplyBatch(context.Background(), 7, cursor, &dataprotection.ProjectionChangesResponse{
			SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
			Changes: []dataprotection.ProjectionChange{{
				ChangeID: "change-" + identity, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
			}},
			NextCursor: "cursor-" + string(rune('1'+index)),
		}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	targets := store.ManagedTargets()
	if len(targets) != 2 || targets[0].Target.ResourceIdentity != "item-a" || targets[1].Target.ResourceIdentity != "item-b" {
		t.Fatalf("managed targets = %#v", targets)
	}
}

func TestStoreRequireUnmanagedRefreshesAnotherProcessCheckpointBeforeGate(t *testing.T) {
	db := openProjectionStoreDB(t)
	writer, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item-a"}
	if err := reader.RequireUnmanaged(context.Background(), 7, []dataprotection.ResourceReference{target}, time.Now().UTC()); err != nil {
		t.Fatalf("initial unmanaged gate failed: %v", err)
	}

	projection := enrollingProjection(t, "manager", target)
	if err := writer.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{
			ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-1",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := reader.RequireUnmanaged(context.Background(), 7, []dataprotection.ResourceReference{target}, time.Now().UTC()); !errors.Is(err, dataprotection.ErrDenied) {
		t.Fatalf("stale reader gate error = %v, want ErrDenied", err)
	}
	if !reader.Gate(7, target, time.Now().UTC()).Managed {
		t.Fatal("reader did not refresh the durable managed target")
	}
}

func TestNewRejectsInvalidConsumerOwnerIdentifier(t *testing.T) {
	db := openProjectionStoreDB(t)
	if _, err := New(db, "manager", "manager-owner", nil); err == nil {
		t.Fatal("invalid consumer owner identifier was accepted")
	}
}

func TestStoreRecordsAndRejectsUnknownMigration(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	if err := db.Table(store.migrationsTable).Order("version").Pluck("version", &versions).Error; err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != initialProjectionStoreMigration || versions[1] != keepPrefixSuffixV2StoreMigration {
		t.Fatalf("migration versions = %#v", versions)
	}
	if err := db.Exec("INSERT INTO "+store.migrationsTable+" (version) VALUES (?)", "999_unknown").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, "manager", "manager", nil); err == nil {
		t.Fatal("unknown migration version was accepted")
	}
}

func TestStoreMigratesPersistedProjectionSchemaAndStructuredMaskToV2(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:item"}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "projection-legacy-mask", Revision: "00000000000000000001",
		ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             target,
		SourceSnapshotHash: "sha256:snapshot",
		Rules: []dataprotection.Rule{{Action: "preview", Component: dataprotection.Component{
			Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: "string", SchemaFingerprint: "sha256:schema",
		}, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, InvalidValueEffect: dataprotection.EffectSuppress,
			Parameters: map[string]any{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
		}}},
		ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "legacy-mask-change", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "legacy-mask-cursor",
	}, now); err != nil {
		t.Fatal(err)
	}
	projection.Rules[0].Decision.Algorithm = "addp.mask.keep_prefix_suffix/v1"
	projection.Rules[0].Decision.Parameters = map[string]any{
		"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit",
	}
	projection.SchemaVersion = "addp.protection_projection/v1"
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table(store.entriesTable).Where("tenant_id = ? AND projection_id = ?", 7, projection.ProjectionID).Update("projection_payload", string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM "+store.migrationsTable+" WHERE version = ?", keepPrefixSuffixV2StoreMigration).Error; err != nil {
		t.Fatal(err)
	}

	migratedStore, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	gate := migratedStore.Gate(7, target, now)
	if !gate.Managed || gate.Err != nil || len(gate.Projections) != 1 {
		t.Fatalf("migrated gate = %#v", gate)
	}
	if gate.Projections[0].SchemaVersion != dataprotection.ProjectionSchemaV2 {
		t.Fatalf("migrated owner projection schema = %q", gate.Projections[0].SchemaVersion)
	}
	decision := gate.Projections[0].Rules[0].Decision
	if decision.Algorithm != dataprotection.AlgorithmKeepPrefixSuffixV2 || decision.Parameters["mask_rune"] != "*" || len(decision.Parameters) != 3 {
		t.Fatalf("migrated owner decision = %#v", decision)
	}
}

func TestStoreResealsProjectionWhenOnlyPersistedSchemaChanges(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:schema-only-item"}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "projection-legacy-schema-only", Revision: "00000000000000000001",
		ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             target,
		SourceSnapshotHash: "sha256:schema-only-snapshot",
		Rules: []dataprotection.Rule{{Action: "preview", Component: dataprotection.Component{
			Key: "email", Path: []dataprotection.PathSegment{{Name: "email", Container: "scalar"}}, ValueType: "string", SchemaFingerprint: "sha256:schema-only",
		}, Decision: dataprotection.Decision{
			Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress,
		}}},
		ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes:       []dataprotection.ProjectionChange{{ChangeID: "legacy-schema-only-change", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		NextCursor:    "legacy-schema-only-cursor",
	}, now); err != nil {
		t.Fatal(err)
	}
	projection.SchemaVersion = "addp.protection_projection/v1"
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	legacyChecksum := projection.Checksum
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table(store.entriesTable).Where("tenant_id = ? AND projection_id = ?", 7, projection.ProjectionID).Update("projection_payload", string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM "+store.migrationsTable+" WHERE version = ?", keepPrefixSuffixV2StoreMigration).Error; err != nil {
		t.Fatal(err)
	}

	migratedStore, err := New(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	gate := migratedStore.Gate(7, target, now)
	if !gate.Managed || gate.Err != nil || len(gate.Projections) != 1 {
		t.Fatalf("migrated gate = %#v", gate)
	}
	migrated := gate.Projections[0]
	if migrated.SchemaVersion != dataprotection.ProjectionSchemaV2 || migrated.Checksum == legacyChecksum {
		t.Fatalf("schema-only migrated projection schema=%q checksum=%q", migrated.SchemaVersion, migrated.Checksum)
	}
	if err := migrated.Validate(time.Time{}); err != nil {
		t.Fatalf("validate schema-only migrated projection: %v", err)
	}
}

func TestStoreMigrationSequenceMustBeUniqueAndOrdered(t *testing.T) {
	noop := func(*gorm.DB, *Store) error { return nil }
	if err := validateStoreMigrations([]storeMigration{{version: "002", apply: noop}, {version: "001", apply: noop}}); err == nil {
		t.Fatal("unordered migration sequence was accepted")
	}
	if err := validateStoreMigrations([]storeMigration{{version: "001", apply: noop}, {version: "001", apply: noop}}); err == nil {
		t.Fatal("duplicate migration version was accepted")
	}
}

func openProjectionStoreDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func enrollingProjection(t *testing.T, owner string, target dataprotection.ResourceReference) dataprotection.Projection {
	t.Helper()
	now := time.Now().UTC().Add(-time.Minute)
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2,
		ProjectionID:  "projection-1",
		Revision:      "00000000000000000001",
		ConsumerOwner: owner,
		State:         dataprotection.ProjectionStateEnrolling,
		Target:        target,
		Rules:         []dataprotection.Rule{},
		ValidFrom:     now,
		ExpiresAt:     now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	return projection
}
