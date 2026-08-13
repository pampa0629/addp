package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	metaauthorization "github.com/addp/meta/internal/authorization"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"gorm.io/gorm"
)

func TestCollectExecutionLineageRouteRequiresDevelopServiceAndIsIdempotent(t *testing.T) {
	db := openLineageRouteTestDB(t)
	source := createLineageRouteItem(t, db, 7, "source")
	target := createLineageRouteItem(t, db, 7, "target")
	insertLineageRouteExecution(t, db, "execution-1", 7, source.ID, target.ID)
	insertLineageRouteExecution(t, db, "tenant-eight-execution", 8, source.ID, target.ID)

	systemServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer develop-token": {
			ClientID: "addp-develop", Permissions: []string{metaauthorization.PermissionMetaLineageCreate},
		},
		"Bearer wrong-client-token": {
			ClientID: "addp-service", Permissions: []string{metaauthorization.PermissionMetaLineageCreate},
		},
		"Bearer no-permission-token": {
			ClientID: "addp-develop", Permissions: []string{metaauthorization.PermissionMetaLineageRead},
		},
	})
	defer systemServer.Close()
	engineService := service.NewEngineService(db, nil)
	scanService := service.NewScanService(db, engineService)
	lineageService := service.NewLineageService(db, engineService)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	router := SetupRouter(cfg, db, engineService, scanService, nil, nil, nil, nil, lineageService)

	first := performLineageCollectionRequest(router, "execution-1", "develop-token")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body = %s", first.Code, first.Body.String())
	}
	var firstResult service.LineageCollectionResult
	if err := json.Unmarshal(first.Body.Bytes(), &firstResult); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstResult.Observed != 1 || firstResult.Skipped != 0 {
		t.Fatalf("first result = %#v", firstResult)
	}

	second := performLineageCollectionRequest(router, "execution-1", "develop-token")
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200; body = %s", second.Code, second.Body.String())
	}
	var secondResult service.LineageCollectionResult
	if err := json.Unmarshal(second.Body.Bytes(), &secondResult); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if secondResult.Observed != 0 || secondResult.Skipped != 1 {
		t.Fatalf("second result = %#v", secondResult)
	}

	for _, testCase := range []struct {
		name       string
		execution  string
		token      string
		wantStatus int
	}{
		{name: "wrong service client", execution: "execution-1", token: "wrong-client-token", wantStatus: http.StatusForbidden},
		{name: "missing permission", execution: "execution-1", token: "no-permission-token", wantStatus: http.StatusForbidden},
		{name: "missing token", execution: "execution-1", wantStatus: http.StatusUnauthorized},
		{name: "tenant isolation", execution: "tenant-eight-execution", token: "develop-token", wantStatus: http.StatusNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := performLineageCollectionRequest(router, testCase.execution, testCase.token)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.wantStatus, response.Body.String())
			}
		})
	}
}

func performLineageCollectionRequest(handler http.Handler, executionID, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/meta/lineage/executions/"+executionID+"/collect", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func openLineageRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	statements := []string{
		`CREATE TABLE meta.lineage_item_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_item_id INTEGER NOT NULL,
			target_item_id INTEGER NOT NULL, relation_kind TEXT NOT NULL, granularity TEXT NOT NULL,
			write_mode TEXT, status TEXT NOT NULL, first_observed_at DATETIME NOT NULL,
			last_observed_at DATETIME NOT NULL, closed_at DATETIME, closed_by_observation_id INTEGER,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE meta.lineage_service_dependencies (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_item_id INTEGER NOT NULL,
			service_id INTEGER NOT NULL, published_revision TEXT NOT NULL, dependency_hash TEXT,
			dependency_kind TEXT NOT NULL, granularity TEXT NOT NULL, dependency_fields JSON,
			status TEXT NOT NULL, first_observed_at DATETIME NOT NULL, last_observed_at DATETIME NOT NULL,
			closed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE meta.lineage_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, relation_kind TEXT NOT NULL,
			granularity TEXT NOT NULL, source_item_id INTEGER, target_item_id INTEGER, service_id INTEGER,
			published_revision TEXT, execution_id TEXT, producer_module TEXT NOT NULL, capture_method TEXT NOT NULL,
			source_snapshot JSON NOT NULL, target_snapshot JSON, evidence JSON NOT NULL,
			observed_at DATETIME NOT NULL, created_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create lineage route fixture: %v", err)
		}
	}
	return db
}

func createLineageRouteItem(t *testing.T, db *gorm.DB, tenantID uint, name string) models.MetaItem {
	t.Helper()
	item := models.MetaItem{
		TenantID: tenantID, EngineID: 9, NodeID: 1, ItemType: "table",
		Name: name, FullName: "public." + name, Fingerprint: "fp-" + name,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func insertLineageRouteExecution(t *testing.T, db *gorm.DB, executionID string, tenantID, sourceItemID, targetItemID uint) {
	t.Helper()
	metadata := commonModels.JSONMap{
		"lineage_facts": commonModels.JSONMap{
			"schema_version": commonExecution.LineageFactsSchemaVersion,
			"inputs":         []map[string]interface{}{{"port": "source", "item_id": sourceItemID}},
			"outputs":        []map[string]interface{}{{"port": "target", "item_id": targetItemID, "write_mode": "replace"}},
			"operations":     []map[string]interface{}{{"kind": "derive", "input_ports": []string{"source"}, "output_ports": []string{"target"}}},
		},
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal lineage execution: %v", err)
	}
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, status, progress, trigger_type, metadata, created_at, updated_at)
		VALUES (?, ?, 'develop', 'workflow', 'develop', 'success', 100, 'manual', ?, ?, ?)`,
		tenantID, executionID, string(payload), time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("insert lineage execution: %v", err)
	}
}
