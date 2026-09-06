package projectionstore

import (
	"context"
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
	if len(versions) != 1 || versions[0] != initialProjectionStoreMigration {
		t.Fatalf("migration versions = %#v", versions)
	}
	if err := db.Exec("INSERT INTO "+store.migrationsTable+" (version) VALUES (?)", "999_unknown").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, "manager", "manager", nil); err == nil {
		t.Fatal("unknown migration version was accepted")
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
