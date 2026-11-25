package repository

import (
	"encoding/json"
	"github.com/addp/catalog/internal/service"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"os"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"gorm.io/gorm"
)

func TestInstalledCatalogFulfillment(t *testing.T) {
	if os.Getenv("ADDP_TEST_CATALOG_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Catalog deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "catalog"}, "catalog", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	entry, binding := createFulfillmentEntry(t, db, 7)
	svc := service.NewEntryService(db, nil, nil)
	access := service.EntryAccess{Inventory: true}
	ctx := t.Context()
	if err := db.Model(&binding).Update("observed_snapshot", commonModels.JSONMap{"name": "fixture", "engine_id": 41}).Error; err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(ctx, 7, access, service.EntryListFilter{View: "inventory", SourceEngineID: 41, Page: 1, PageSize: 10})
	if err != nil || list.Total != 1 || list.Data[0].ID != entry.ID {
		t.Fatalf("engine-filtered inventory: %v, %v", list, err)
	}
	if _, err := svc.GetGovernanceCoverage(ctx, 7, access); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListDomainStandardReferences(ctx, 7, access, 2, "inventory", 1, 10); err != nil {
		t.Fatal(err)
	}
	const subject = int64(9007199254740993)
	check := models.FulfillmentCheck{RequestID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, RequestBinding: json.RawMessage(`{"recipient_id":9007199254740993}`)}
	if err := db.Create(&check).Error; err != nil {
		t.Fatal(err)
	}
	var stored models.FulfillmentCheck
	if err := db.First(&stored, "request_id=?", check.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	var facts struct {
		RecipientID int64 `json:"recipient_id"`
	}
	if err := json.Unmarshal(stored.RequestBinding, &facts); err != nil || facts.RecipientID != subject {
		t.Fatalf("exact binding lost: %s (%v)", stored.RequestBinding, err)
	}
	clock := func() time.Time {
		t.Helper()
		now, err := execution.DatabaseNow(db)
		if err != nil {
			t.Fatal(err)
		}
		return now
	}
	deactivate := func() error { return db.Model(&binding).Update("source_status", models.SourceStatusMissing).Error }
	if err := deactivate(); err == nil {
		t.Fatal("unresolved fulfillment allowed a source change")
	}
	if err := db.Delete(&stored).Error; err == nil {
		t.Fatal("fulfillment receipt was deleted")
	}
	if err := db.Model(&check).Update("grant_reconciled_at", clock()).Error; err == nil {
		t.Fatal("issuance preceded acceptance")
	}
	if err := db.Model(&check).Update("resolved_at", clock()).Error; err != nil {
		t.Fatal(err)
	}
	if err := deactivate(); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&check).Update("grant_reconciled_at", clock()).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&stored).Error; err == nil {
		t.Fatal("resolved history was deleted")
	}
	t.Logf("Catalog entry %s: unresolved basis held, acceptance released basis, immutable issuance reconciled", entry.ID)
}
