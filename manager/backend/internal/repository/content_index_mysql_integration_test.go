package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestIntegrationMySQLManagerContentReceipt(t *testing.T) {
	db := managerMySQLStoreDB(t, "manager", "ADDP_TEST_MYSQL_MANAGER_STORAGE")
	ctx := t.Context()
	if err := MigrateContentIndexDeliverySchema(db); err != nil {
		t.Fatal(err)
	}
	index := "receipt-" + uuid.NewString()
	repo := NewContentIndexDeliveryRepository(db, index)
	epoch := openActiveContentIndex(t, repo)
	before, err := repo.SearchMaterial(ctx, epoch, 7)
	if err != nil || before.VectorRevision != 0 {
		t.Fatalf("keyword material without vectors: %+v, %v", before, err)
	}
	reserve := func(epoch string, ids ...string) map[string]int64 {
		t.Helper()
		var result map[string]int64
		if err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			result, err = repo.ReserveSearchOrdinals(ctx, tx, epoch, ids)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return result
	}
	original := reserve(epoch, "orders")["orders"]
	scope := client.ManagerContentDeleteScope{OperationID: uuid.NewString(), EngineID: 9, DocumentIDs: []string{"orders"}}
	const filter = "tenant_id = 7 AND engine_id = 9"
	op, err := repo.QueueDelete(ctx, epoch, 7, filter, scope)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ClaimMaintenance(ctx, epoch, op); err != nil || !ok {
		t.Fatalf("claim delete: %v, %v", ok, err)
	}
	if err := repo.Unknown(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateIfSettled(ctx, epoch); !errors.Is(err, ErrContentIndexIsolated) {
		t.Fatalf("unknown delete reopened index: %v", err)
	}
	if err := repo.Receipt(ctx, op, 0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(ctx, 7, op.ID)
	if err != nil || stored.TaskUID == nil || *stored.TaskUID != 0 {
		t.Fatalf("stored external receipt: %+v, %v", stored, err)
	}
	if err := repo.Finish(ctx, stored, IndexDeliverySucceeded); err != nil {
		t.Fatal(err)
	}
	if err := MigrateContentIndexDeliverySchema(db); err != nil {
		t.Fatal(err)
	}
	peer := NewContentIndexDeliveryRepository(db, index)
	next, err := peer.JoinConfiguredIndex(ctx, index, "endpoint", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.ActivateIfSettled(ctx, next); err != nil {
		t.Fatal(err)
	}
	after, err := peer.SearchMaterial(ctx, next, 7)
	if err != nil || after.Generation != before.Generation || after.SearchRevision <= before.SearchRevision || after.NextSearchOrdinal != original || next != epoch {
		t.Fatalf("search material after delivery and restart: %+v -> %+v, %v", before, after, err)
	}
	ordinals := reserve(next, "orders", "Orders")
	if ordinals["orders"] != original || ordinals["Orders"] <= original {
		t.Fatalf("document ordinals after restart: %v (original %d)", ordinals, original)
	}
	replay, err := repo.QueueDelete(ctx, next, 7, filter, scope)
	if err != nil || replay.ID != op.ID || replay.Status != IndexDeliverySucceeded {
		t.Fatalf("replay after preparation: %+v, %v", replay, err)
	}
	documents := []client.ManagerContentDocument{{DocumentID: "orders", EngineID: 9}}
	write := &models.ContentIndexDelivery{TenantID: 7, DocumentIDs: []string{"orders"}}
	check := func(tx *gorm.DB) error { return repo.CheckWriteAfterDeletion(tx, write, documents) }
	if err := db.Transaction(check); !errors.Is(err, ErrContentIndexDeliveryConflict) {
		t.Fatalf("unbound write after delete: %v", err)
	}
	documents[0].AfterDeleteOperationID = op.ID
	if err := db.Transaction(check); err != nil {
		t.Fatalf("bound write after delete: %v", err)
	}
}
