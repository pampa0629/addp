package service

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func TestMetaCleanupChangesAgainstMySQL(t *testing.T) {
	password := os.Getenv("ADDP_TEST_META_CLEANUP_PASSWORD")
	if password == "" {
		t.Skip("requires the disposable platform-meta-scan gate")
	}
	dial, err := platformmysql.Dialector(database.Config{DBName: "meta", Host: "127.0.0.1", Port: os.Getenv("ADDP_MYSQL_PORT"), User: os.Getenv("ADDP_DATABASE_RUNTIME_USER"), Password: password, ConnectTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dial, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	readBasis := func(conn *gorm.DB, tenant uint) authorizationfacts.Revision {
		t.Helper()
		r, err := NewMetadataQueryService(conn).GetAuthorizationBasisRevision(t.Context(), tenant)
		if err != nil {
			t.Fatal(err)
		}
		return *r
	}
	changes := func(conn *gorm.DB, tenant uint, cursor string) *models.DataItemChangesResponse {
		t.Helper()
		page, err := NewDataItemChangeService(conn).List(tenant, cursor, 10)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	itemBefore, nodeBefore := readBasis(db, 8), readBasis(db, 7)
	if itemBefore.Version != "1" || nodeBefore != itemBefore {
		t.Fatal("installed facts have no initial revision", itemBefore, nodeBefore)
	}
	initial := changes(db, 8, "")
	if len(initial.Changes) != 1 || initial.Changes[0].Operation != "upsert" || initial.Changes[0].SourceIdentity != "fk-gate-item" {
		t.Fatalf("initial Catalog page: %+v", initial)
	}
	rollback := errors.New("roll back fixture cleanup")
	for _, undo := range []bool{true, false} {
		err := db.Transaction(func(tx *gorm.DB) error {
			for _, q := range []string{"DELETE FROM meta.meta_item WHERE tenant_id=8 AND engine_id=202", "DELETE FROM meta.meta_node WHERE tenant_id=7 AND engine_id=201"} {
				result := tx.Exec(q)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					t.Fatalf("expected exactly one child, got %d", result.RowsAffected)
				}
			}
			page := changes(tx, 8, initial.NextCursor)
			if len(page.Changes) != 1 {
				t.Fatalf("missing change: %+v", page)
			}
			change := page.Changes[0]
			if change.Operation != "missing" || change.SourceIdentity != initial.Changes[0].SourceIdentity ||
				change.SourceVersion <= initial.Changes[0].SourceVersion || change.Snapshot["schema_name"] != "orders" || change.Snapshot["table_name"] != "fk gate" {
				t.Fatalf("wrong Catalog deletion: %+v", change)
			}
			for _, tenant := range []uint{7, 8} {
				r := readBasis(tx, tenant)
				if r.Version != "2" || r.Generation != itemBefore.Generation {
					t.Fatalf("tenant %d basis: %+v", tenant, r)
				}
			}
			if undo {
				return rollback
			}
			return nil
		})
		if undo {
			if !errors.Is(err, rollback) || readBasis(db, 8) != itemBefore || readBasis(db, 7) != nodeBefore || len(changes(db, 8, initial.NextCursor).Changes) != 0 {
				t.Fatal("rollback escaped into a reader", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if readBasis(db, 7).Version != "2" || readBasis(db, 8).Version != "2" || len(changes(db, 8, initial.NextCursor).Changes) != 1 || len(changes(db, 7, "").Changes) != 0 || readBasis(db, 9).Version != "0" {
		t.Fatal("committed cleanup or tenant isolation differs")
	}
	t.Log("Catalog upsert -> missing; item and node tenant revisions 1 -> 2; rollback preserved both revisions and cursor; untouched tenant remained at 0")
}
