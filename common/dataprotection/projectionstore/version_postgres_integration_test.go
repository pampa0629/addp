package projectionstore

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type versionResultCleaner struct{ table string }

func (b versionResultCleaner) ApplyProjectionChanges(ctx context.Context, tx *gorm.DB, tenant int64, _ []dataprotection.ProjectionChange, _ time.Time) error {
	return tx.WithContext(ctx).Exec("DELETE FROM "+b.table+" WHERE tenant_id = ?", tenant).Error
}

func TestProjectionStoreVersionCommitSerializesCleanupAgainstPostgres(t *testing.T) {
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
	const schema = "projection_store_version_it"
	dropProjectionStoreTestSchema(t, db, schema)
	t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
	cleaner := versionResultCleaner{table: schema + ".derived_results"}
	store, err := Migrate(db, schema, "manager", cleaner)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE " + cleaner.table + " (tenant_id BIGINT PRIMARY KEY, value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	other, err := Open(db, schema, "manager", cleaner)
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.CaptureVersion(context.Background(), 7, func(GateReader) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}
	projection := enrollingProjection(t, "manager", target)
	batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "installed",
		Changes: []dataprotection.ProjectionChange{{ChangeID: "install", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	commitDone := make(chan error, 1)
	commitFinished := make(chan struct{})
	commitCtx, cancelCommit := context.WithTimeout(context.Background(), 5*time.Second)
	go func() {
		defer close(commitFinished)
		commitDone <- store.CommitVersion(commitCtx, 7, version, func(tx *gorm.DB, _ GateReader) error {
			close(locked)
			<-release
			return tx.Exec("INSERT INTO "+cleaner.table+" VALUES (?, ?)", 7, "old result").Error
		})
	}()
	t.Cleanup(func() {
		cancelCommit()
		select {
		case <-release:
		default:
			close(release)
		}
		select {
		case <-commitFinished:
		case <-time.After(3 * time.Second):
			t.Error("commit goroutine did not stop before schema cleanup")
		}
	})
	select {
	case <-locked:
	case err := <-commitDone:
		t.Fatalf("commit before lock: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("commit lock timed out")
	}
	// A competing process must not install/clean between version comparison
	// and the actual result INSERT. Its deadline must expire on the row lock.
	waitCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	err = other.ApplyBatch(waitCtx, 7, "", batch, time.Now())
	ctxErr := waitCtx.Err()
	cancel()
	if err == nil || !errors.Is(ctxErr, context.DeadlineExceeded) {
		t.Fatalf("installation did not wait for commit: err=%v ctx=%v", err, ctxErr)
	}
	close(release)
	select {
	case err := <-commitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("commit completion timed out")
	}
	if err := other.ApplyBatch(context.Background(), 7, "", batch, time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Table(cleaner.table).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("old result not cleaned: count=%d err=%v", count, err)
	}
	if err := store.CommitVersion(context.Background(), 7, version, func(*gorm.DB, GateReader) error { t.Error("late old write invoked"); return nil }); !errors.Is(err, ErrVersionChanged) {
		t.Fatal(err)
	}
	// Concurrent first use must create/lock one checkpoint without a duplicate
	// key failure, including tenants that have not yet received protection.
	start := make(chan struct{})
	firstUse := make(chan error, 2)
	for _, reader := range []*Store{store, other} {
		go func(reader *Store) {
			<-start
			_, err := reader.CaptureVersion(context.Background(), 8, func(GateReader) error { return nil })
			firstUse <- err
		}(reader)
	}
	close(start)
	for range 2 {
		select {
		case err := <-firstUse:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("first use timed out")
		}
	}
}
