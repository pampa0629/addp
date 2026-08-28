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

func TestRequirePostgreSQLCatalogTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/v1/system/engines/12/catalog/facts" {
			_ = json.NewEncoder(w).Encode(map[string]any{"table": map[string]any{"fields": []map[string]any{{"name": "id"}, {"name": "created_at"}}}})
			return
		}
		if r.URL.Path != "/api/v1/system/engines/12/catalog/children" {
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
				Name: "public", Role: "branch", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: 12, Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "schema", Kind: "namespace", Name: "public"}}},
			}}})
		case 2:
			entry := commonClient.EngineCatalogEntry{
				Name: "orders", Role: "leaf",
				Path: commonClient.EngineCatalogPath{
					Version: "catalog.path/v1", EngineID: 12,
					Segments: []commonClient.EngineCatalogSegment{{Term: "server"}, {Term: "schema", Name: "public"}, {Term: "table", Name: "orders"}},
				},
			}
			_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{entry}})
		default:
			t.Fatalf("unexpected catalog path: %#v", request.Path)
		}
	}))
	defer server.Close()

	client := commonClient.NewSystemServiceClient(server.URL, qualityCatalogTokenSource("tenant-token"), server.Client())
	table, err := requirePostgreSQLCatalogTable(context.Background(), client, 7, 12, "public", "orders")
	if err != nil {
		t.Fatalf("valid table error = %v", err)
	}
	if err := requirePostgreSQLCatalogColumn(context.Background(), client, 7, 12, table, "id"); err != nil {
		t.Fatalf("valid target error = %v", err)
	}
	if err := requirePostgreSQLCatalogColumn(context.Background(), client, 7, 12, table, "missing"); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("missing column error = %v, want bad request", err)
	}
	if _, err := requirePostgreSQLCatalogTable(context.Background(), client, 7, 12, "public", "missing"); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("missing table error = %v, want bad request", err)
	}
}

func TestUpdateRuleApplicationRequiresActiveEngineWhenEnabling(t *testing.T) {
	for _, test := range []struct {
		name      string
		lifecycle string
		wantErr   bool
	}{
		{name: "active", lifecycle: commonModels.EngineLifecycleActive},
		{name: "disabled", lifecycle: commonModels.EngineLifecycleDisabled, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/system/engines/12" {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(commonModels.Engine{ID: 12, EngineType: "postgresql", LifecycleState: test.lifecycle, ConnectionStatus: commonModels.EngineConnectionOnline})
			}))
			defer server.Close()

			db := newRuleEngineUpdateTestDB(t)
			application := models.RuleApplication{
				TenantID: 7, ElementID: 31, ElementRevisionID: 3101, EngineID: 12, SchemaName: "public", Table: "orders", ColumnName: "id",
				RuleConfig: []byte(`{"schema_version":"addp.quality.rules/v1","rules":[]}`), Enabled: false, CreatedBy: 1,
			}
			if err := db.Create(&application).Error; err != nil {
				t.Fatalf("create rule application: %v", err)
			}
			if err := db.Model(&models.RuleApplication{}).Where("id = ?", application.ID).Update("enabled", false).Error; err != nil {
				t.Fatalf("disable rule application fixture: %v", err)
			}
			client := commonClient.NewSystemServiceClient(server.URL, qualityCatalogTokenSource("tenant-token"), server.Client())
			svc := NewRuleEngineService(nil, client, repository.NewRuleApplicationRepository(db))
			enabled := true
			updated, err := svc.UpdateRuleApplication(context.Background(), application.ID, application.TenantID, 19, &UpdateRuleApplicationRequest{Enabled: &enabled})
			if test.wantErr {
				if !errors.Is(err, commonAPI.ErrBadRequest) {
					t.Fatalf("UpdateRuleApplication() error = %v, want bad request", err)
				}
				var stored models.RuleApplication
				if loadErr := db.First(&stored, application.ID).Error; loadErr != nil {
					t.Fatalf("load rule application: %v", loadErr)
				}
				if stored.Enabled {
					t.Fatal("inactive engine rule application was enabled")
				}
				return
			}
			if err != nil || updated == nil || !updated.Enabled {
				t.Fatalf("UpdateRuleApplication() = %#v, %v, want enabled", updated, err)
			}
		})
	}
}

func TestUpdateRuleApplicationAllowsDisablingWithoutEngineAccess(t *testing.T) {
	db := newRuleEngineUpdateTestDB(t)
	application := models.RuleApplication{
		TenantID: 7, ElementID: 31, ElementRevisionID: 3101, EngineID: 12, SchemaName: "public", Table: "orders", ColumnName: "id",
		RuleConfig: []byte(`{"schema_version":"addp.quality.rules/v1","rules":[]}`), Enabled: true, CreatedBy: 1,
	}
	if err := db.Create(&application).Error; err != nil {
		t.Fatalf("create rule application: %v", err)
	}
	svc := NewRuleEngineService(nil, nil, repository.NewRuleApplicationRepository(db))
	enabled := false
	updated, err := svc.UpdateRuleApplication(context.Background(), application.ID, application.TenantID, 19, &UpdateRuleApplicationRequest{Enabled: &enabled})
	if err != nil || updated == nil || updated.Enabled {
		t.Fatalf("UpdateRuleApplication() = %#v, %v, want disabled", updated, err)
	}
}

func newRuleEngineUpdateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS quality").Error; err != nil {
		t.Fatalf("attach quality schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE quality.rule_applications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		element_id INTEGER NOT NULL,
		element_revision_id INTEGER NOT NULL,
		engine_id INTEGER NOT NULL,
		schema_name TEXT NOT NULL,
		table_name TEXT NOT NULL,
		column_name TEXT NOT NULL,
		rule_config BLOB NOT NULL,
		enabled BOOLEAN NOT NULL,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create rule application table: %v", err)
	}
	return db
}

type qualityCatalogTokenSource string

func (s qualityCatalogTokenSource) Token(context.Context, uint) (string, error) {
	return string(s), nil
}

func (s qualityCatalogTokenSource) PlatformToken(context.Context) (string, error) {
	return string(s), nil
}
