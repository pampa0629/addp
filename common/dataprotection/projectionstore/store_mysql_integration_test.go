package projectionstore

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"gorm.io/gorm"
)

func TestOwnerStoreAgainstMySQL(t *testing.T) {
	name := os.Getenv("ADDP_TEST_MYSQL_PROJECTION_DATABASE")
	if name == "" {
		t.Skip("run the platform-projection-store disposable MySQL gate")
	}
	if name != "manager" {
		t.Fatal("expected the gate-owned Manager database")
	}
	dialect, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
		DBName: name, ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err := Open(db, name, "manager", nil); err == nil {
		t.Fatal("opened an unprepared store")
	}
	if _, err := Migrate(db, "other_owner", "manager", nil); err == nil {
		t.Fatal("used a different physical database")
	}
	store, err := Migrate(db, name, "manager", failingProjectionChangeBarrier{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:item"}
	projection := enrollingProjection(t, "manager", target)
	batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "first"}
	if err := store.ApplyBatch(ctx, 7, "", batch, time.Now()); err == nil {
		t.Fatal("owner barrier failure was accepted")
	}
	for _, table := range []string{store.entriesTable, store.checkpointTable} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("barrier rollback %s: %d, %v", table, count, err)
		}
	}
	store, err = Open(db, name, "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() { defer group.Done(); <-start; results <- store.ApplyBatch(ctx, 7, "", batch, time.Now()) }()
	}
	close(start)
	group.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if err.Error() != "protection projection checkpoint changed concurrently" {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("accepted %d concurrent deliveries", success)
	}
	if _, err := Migrate(db, name, "manager", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		reopened, err := Open(tx, name, "manager", nil)
		if err != nil {
			return err
		}
		cursor, err := reopened.CurrentCursor(ctx, 7)
		if err != nil {
			return err
		}
		if cursor != "first" || !reopened.Gate(7, target, time.Now()).Managed || reopened.Gate(8, target, time.Now()).Managed {
			t.Fatal("read-only reopen lost tenant scope, projection or cursor")
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE protection_projection_entries DROP PRIMARY KEY, ADD PRIMARY KEY (projection_id)").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Open(db, name, "manager", nil); err == nil {
		t.Fatal("runtime accepted a projection key shared across tenants")
	}
	if _, err := Migrate(db, name, "manager", nil); err == nil {
		t.Fatal("installer accepted drift at the current revision")
	}
}
