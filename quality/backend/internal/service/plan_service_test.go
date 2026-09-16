package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/quality/internal/testsupport"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPlanServiceValidatesCatalogOnCreateAndUpdate(t *testing.T) {
	t.Parallel()

	server := newPlanCatalogServer(t)
	defer server.Close()
	db := newPlanServiceTestDB(t)
	client := commonClient.NewSystemServiceClient(server.URL, qualityCatalogTokenSource("tenant-token"), server.Client())

	svc := NewPlanService(repository.NewPlanRepository(db), time.Minute).WithClients(client, nil)
	request := PlanWriteRequest{Code: "orders_check", Name: "orders quality", TableBindings: []PlanTableBinding{{Alias: "orders", Locator: "addp://engine/12/path/public/orders?type=table"}}, CheckItems: planRequestFixtures(t, db, 7, gateTestDocument(`{"table":"orders","column":"id"}`, "not_null"))}
	created, err := svc.Create(context.Background(), 7, 11, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Version = created.Version
	request.Name = "renamed"
	updated, err := svc.Update(context.Background(), 7, 22, created.ID, request)
	if err != nil || updated.Version != 2 || updated.UpdatedBy != 22 {
		t.Fatalf("update %#v %v", updated, err)
	}
	if _, err = svc.Update(context.Background(), 7, 22, created.ID, request); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("stale update %v", err)
	}
	request.Version = 2
	request.CheckItems[0].Bindings.Column = "missing"
	if _, err = svc.Update(context.Background(), 7, 22, created.ID, request); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("missing column %v", err)
	}
	stored, err := svc.Get(context.Background(), 7, created.ID)
	if err != nil || stored.Version != 2 || string(stored.Rules) != string(updated.Rules) {
		t.Fatalf("failed update mutated: %#v %v", stored, err)
	}
	request.Version = 0
	request.TableBindings[0].Locator = "addp://engine/12/path/public/missing?type=table"
	if _, err = svc.Create(context.Background(), 7, 11, request); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("missing table %v", err)
	}
}

func newPlanCatalogServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/engines/12" {
			_ = json.NewEncoder(w).Encode(commonModels.Engine{ID: 12, EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline})
			return
		}
		if r.URL.Path == "/api/v1/system/engines/12/catalog/facts" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"table": map[string]interface{}{"fields": []map[string]interface{}{{"name": "id"}}}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/engines/12/catalog/children" {
			http.NotFound(w, r)
			return
		}
		var request commonClient.EngineCatalogListChildrenRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch len(request.Path.Segments) {
		case 0:
			_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{
				Name: "Business PostgreSQL", Role: "branch", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: 12, Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server"}}},
			}}})
		case 1:
			_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{
				Name: "public", Role: "branch", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: 12, Segments: []commonClient.EngineCatalogSegment{{Term: "server"}, {Term: "schema", Name: "public"}}},
			}}})
		case 2:
			_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{
				Name: "orders", Role: "leaf", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: 12, Segments: []commonClient.EngineCatalogSegment{{Term: "server"}, {Term: "schema", Name: "public"}, {Term: "table", Name: "orders"}}},
			}}})
		default:
			t.Fatalf("unexpected catalog path: %#v", request.Path)
		}
	}))
}

func newPlanServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.plans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		code TEXT NOT NULL, version INTEGER NOT NULL, table_bindings JSON NOT NULL,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		last_run_at DATETIME,
		last_execution_id TEXT,
		last_execution_status TEXT
	)`).Error; err != nil {
		t.Fatalf("create check tasks table: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	testsupport.EnsureRuleTables(t, db)
	return db
}
