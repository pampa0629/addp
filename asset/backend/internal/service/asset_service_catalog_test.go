package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/addp/asset/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAssetAggregateUsesCatalogReferencesAndPublishGate(t *testing.T) {
	db := openAssetAggregateTestDB(t)
	typeDefinition := models.TypeDefinition{TenantID: 0, Name: "Dataset", Code: "dataset", Enabled: true}
	if err := db.Create(&typeDefinition).Error; err != nil {
		t.Fatal(err)
	}
	primary, supporting := uuid.New(), uuid.New()
	var publishable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/catalog/runtime/references/resolve" || request.Header.Get("Authorization") != "Bearer asset-tenant-7" {
			t.Fatalf("request = %s authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		var payload struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		results := make([]map[string]any, 0, len(payload.IDs))
		for _, id := range payload.IDs {
			results = append(results, map[string]any{
				"id": id, "found": true, "selectable": true, "publishable": publishable.Load(), "version": "3",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer server.Close()
	tokens := commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		return fmt.Sprintf("asset-tenant-%d", tenantID), nil
	})
	service := NewAssetService(db, commonClient.NewCatalogClient(server.URL, tokens, server.Client()), nil)

	created, err := service.Create(context.Background(), 7, 11, &CreateAssetReq{
		Name: "Orders product", TypeID: typeDefinition.ID, Tags: []string{"orders"},
		Components: []AssetComponentInput{
			{CatalogEntryID: primary.String(), Role: models.AssetComponentRolePrimary, SortOrder: 0},
			{CatalogEntryID: supporting.String(), Role: models.AssetComponentRoleSupporting, SortOrder: 1},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Version != 1 || created.OwnerID != 11 || len(created.Components) != 2 {
		t.Fatalf("created = %#v", created)
	}
	if err := service.Publish(context.Background(), 7, created.ID); err != ErrCatalogReferenceNotPublishable {
		t.Fatalf("Publish() error = %v", err)
	}
	updated, err := service.Update(context.Background(), 7, created.ID, 12, &UpdateAssetReq{
		Version: 1, Name: "Orders data product", TypeID: typeDefinition.ID,
		Components: []AssetComponentInput{{CatalogEntryID: primary.String(), Role: models.AssetComponentRolePrimary, SortOrder: 0}},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 2 || len(updated.Components) != 1 || updated.Components[0].CatalogEntryID != primary {
		t.Fatalf("updated = %#v", updated)
	}
	if _, err := service.Update(context.Background(), 7, created.ID, 12, &UpdateAssetReq{
		Version: 1, Name: "stale", TypeID: typeDefinition.ID,
		Components: []AssetComponentInput{{CatalogEntryID: primary.String(), Role: models.AssetComponentRolePrimary}},
	}); err != ErrAssetVersionConflict {
		t.Fatalf("stale Update() error = %v", err)
	}
	publishable.Store(true)
	if err := service.Publish(context.Background(), 7, created.ID); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if _, err := service.Update(context.Background(), 7, created.ID, 12, &UpdateAssetReq{
		Version: 2, Name: "published edit", TypeID: typeDefinition.ID,
		Components: []AssetComponentInput{{CatalogEntryID: primary.String(), Role: models.AssetComponentRolePrimary}},
	}); err != ErrAssetNotEditable {
		t.Fatalf("published Update() error = %v", err)
	}
	if err := service.Delete(7, created.ID); err == nil {
		t.Fatal("published asset must be offlined before deletion")
	}
	if err := service.Offline(7, created.ID); err != nil {
		t.Fatalf("Offline() error = %v", err)
	}
	if err := service.Delete(7, created.ID); err != nil {
		t.Fatalf("Delete() offline asset error = %v", err)
	}
	if _, err := service.Get(7, created.ID); err == nil {
		t.Fatal("deleted offline asset remains readable")
	}
	var componentCount int64
	if err := db.Model(&models.AssetComponent{}).Where("asset_id = ?", created.ID).Count(&componentCount).Error; err != nil {
		t.Fatal(err)
	}
	if componentCount != 0 {
		t.Fatalf("deleted asset component count = %d", componentCount)
	}
}

func TestBatchPublishRejectsDuplicateAssetIDs(t *testing.T) {
	service := NewAssetService(openAssetAggregateTestDB(t), nil, nil)
	if _, err := service.BatchPublish(context.Background(), 7, []int64{3, 3}); err != ErrInvalidAssetAggregate {
		t.Fatalf("BatchPublish() error = %v", err)
	}
}

func TestAssetAggregateRejectsInvalidComponentShapeBeforeCatalogCall(t *testing.T) {
	db := openAssetAggregateTestDB(t)
	service := NewAssetService(db, nil, nil)
	for _, components := range [][]AssetComponentInput{
		nil,
		{{CatalogEntryID: uuid.NewString(), Role: models.AssetComponentRoleSupporting}},
		{{CatalogEntryID: uuid.NewString(), Role: models.AssetComponentRolePrimary}, {CatalogEntryID: uuid.NewString(), Role: models.AssetComponentRolePrimary}},
	} {
		if _, err := service.validateComponents(context.Background(), 7, components, false); err != ErrInvalidAssetAggregate {
			t.Fatalf("components %#v error = %v", components, err)
		}
	}
}

func openAssetAggregateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS asset").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE asset.type_definitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, code TEXT NOT NULL,
			auth_handler TEXT, entry_type TEXT, icon_url TEXT, description TEXT, enabled BOOLEAN, sort_order INTEGER,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.catalogs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, parent_id INTEGER,
			sort_order INTEGER, description TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT,
			type_id INTEGER NOT NULL, catalog_id INTEGER, tags TEXT, status TEXT, owner_id INTEGER, version INTEGER,
			published_at DATETIME, created_by INTEGER, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.asset_components (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, asset_id INTEGER NOT NULL,
			catalog_entry_id TEXT NOT NULL, role TEXT NOT NULL, sort_order INTEGER NOT NULL,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE asset.asset_ext_fields (
			id INTEGER PRIMARY KEY AUTOINCREMENT, asset_id INTEGER NOT NULL, field_key TEXT, value TEXT,
			created_at DATETIME, updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
