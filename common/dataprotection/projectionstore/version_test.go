package projectionstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"gorm.io/gorm"
)

func TestVersionCommitRejectsChangedCheckpointAndUsesDurableFacts(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := Migrate(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}
	version, err := store.CaptureVersion(context.Background(), 7, func(g GateReader) error {
		if g.Gate(7, target, time.Now()).Managed {
			t.Fatal("unexpected managed target")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	projection := enrollingProjection(t, "manager", target)
	if err := other.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "new",
		Changes: []dataprotection.ProjectionChange{{ChangeID: "install", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	called := false
	err = store.CommitVersion(context.Background(), 7, version, func(*gorm.DB, GateReader) error { called = true; return nil })
	if !errors.Is(err, ErrVersionChanged) || called {
		t.Fatalf("old commit: called=%v err=%v", called, err)
	}
	batchWithoutVersionChange := &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "new",
		Changes: []dataprotection.ProjectionChange{{ChangeID: "same-cursor", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
	}
	if err := other.ApplyBatch(context.Background(), 7, "new", batchWithoutVersionChange, time.Now()); err == nil {
		t.Fatal("changes without version advance were accepted")
	}
	// The process-local cache is deliberately stale; CaptureVersion must read
	// the actual rows protected by the checkpoint lock, not that cache.
	if store.Gate(7, target, time.Now()).Managed {
		t.Fatal("test requires stale process cache")
	}
	_, err = store.CaptureVersion(context.Background(), 7, func(g GateReader) error {
		if !g.Gate(7, target, time.Now()).Managed {
			t.Fatal("durable protection was missed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVersionCommitBindingRollbackAndTenantIsolation(t *testing.T) {
	db := openProjectionStoreDB(t)
	store, err := Migrate(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.CaptureVersion(context.Background(), 7, func(GateReader) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Version{{}, {tenantID: 7, schema: "other", owner: "manager"}, {tenantID: 7, schema: "manager", owner: "other"}} {
		if err := store.CommitVersion(context.Background(), 7, invalid, func(*gorm.DB, GateReader) error { t.Fatal("invalid callback invoked"); return nil }); err == nil {
			t.Fatal("invalid version accepted")
		}
	}
	if err := store.CommitVersion(context.Background(), 8, version, func(*gorm.DB, GateReader) error { t.Fatal("cross tenant commit"); return nil }); err == nil {
		t.Fatal("tenant mismatch accepted")
	}
	if err := store.ApplyBatch(context.Background(), 8, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "tenant8"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE manager.derived_result (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	rejected := errors.New("result rejected")
	err = store.CommitVersion(context.Background(), 7, version, func(tx *gorm.DB, _ GateReader) error {
		if err := tx.Exec("INSERT INTO manager.derived_result VALUES ('partial')").Error; err != nil {
			return err
		}
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Table("manager.derived_result").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	if err := store.CommitVersion(context.Background(), 7, version, func(tx *gorm.DB, _ GateReader) error {
		return tx.Exec("INSERT INTO manager.derived_result VALUES ('success')").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("manager.derived_result").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("success count=%d err=%v", count, err)
	}
	if _, err := store.CaptureVersion(context.Background(), 7, func(GateReader) error { return rejected }); !errors.Is(err, rejected) {
		t.Fatal(err)
	}
}
