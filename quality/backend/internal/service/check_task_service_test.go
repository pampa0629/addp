package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCheckTaskServiceValidatesCatalogOnCreateAndUpdate(t *testing.T) {
	t.Parallel()

	server := newCheckTaskCatalogServer(t)
	defer server.Close()
	db := newCheckTaskServiceTestDB(t)
	client := commonClient.NewSystemServiceClient(server.URL, qualityCatalogTokenSource("tenant-token"), server.Client())
	service := NewCheckTaskService(repository.NewCheckTaskRepository(db), client)

	created, err := service.Create(context.Background(), 7, 11, &CreateCheckTaskRequest{
		Name: "orders quality", EngineID: 12, SchemaName: "public", TableName: "orders",
	})
	if err != nil {
		t.Fatalf("create valid task: %v", err)
	}

	if _, err := service.Create(context.Background(), 7, 11, &CreateCheckTaskRequest{
		Name: "missing", EngineID: 12, SchemaName: "public", TableName: "missing",
	}); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("create missing table error = %v, want bad request", err)
	}
	var count int64
	if err := db.Model(&models.CheckTask{}).Where("tenant_id = ?", 7).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("task count = %d, want 1", count)
	}

	if _, err := service.Update(context.Background(), created.ID, 7, 22, &UpdateCheckTaskRequest{
		Name: "invalid update", EngineID: 12, SchemaName: "public", TableName: "missing",
	}); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("update missing table error = %v, want bad request", err)
	}
	unchanged, err := service.Get(created.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Name != "orders quality" || unchanged.Table != "orders" {
		t.Fatalf("failed update changed task = %#v", unchanged)
	}
}

func newCheckTaskCatalogServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/engines/12" {
			_ = json.NewEncoder(w).Encode(commonModels.Engine{ID: 12, EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive})
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

func newCheckTaskServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.check_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		engine_id INTEGER NOT NULL,
		schema_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
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
	return db
}
