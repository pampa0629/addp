package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGraphControlRequiresExactVersionAndNarrowPermissions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err = repository.MigrateGraphTables(db); err != nil {
		t.Fatal(err)
	}
	ontology := models.Ontology{TenantID: 7, Name: "control"}
	if err = db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	kg := models.KnowledgeGraph{TenantID: 7, OntologyID: ontology.ID, EngineID: 13, Name: "closed", IndexManagement: models.GraphIndexesExternal, AdmissionClosed: true, CatalogPath: plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 13, Segments: []plugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "graph", Kind: "graph", Name: "original"}}}}
	if err = db.Create(&kg).Error; err != nil {
		t.Fatal(err)
	}
	handler := NewKnowledgeGraphHandler(service.NewKnowledgeGraphService(repository.NewKnowledgeGraphRepository(db), nil))
	auth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer reader": {"graph.graph.read"}, "Bearer editor": {"graph.graph.update"}, "Bearer reconciler": {"graph.graph.reconcile"}, "Bearer both": {"graph.graph.update", "graph.graph.reconcile"},
	})
	defer auth.Close()
	router := SetupRouter(NewCleanupResultHandler(nil), &config.Config{SystemServiceURL: auth.URL}, nil, handler, nil, nil, nil, nil, nil, modulelifecycle.NewStandalone("graph"))
	for _, tc := range []struct {
		path, token, body string
		status            int
		code              string
	}{
		{"reconcile", "reader", `{"version":1}`, 403, ""},
		{"reconcile", "editor", `{"version":1}`, 403, ""},
		{"reconcile", "reconciler", `{"version":1,"force":true}`, 400, ""},
		{"reconcile", "reconciler", `{}`, 400, ""},
		{"reconcile", "reconciler", `{"version":2}`, 409, "resource_version_conflict"},
		{"reconcile", "reconciler", `{"version":1}`, 200, ""},
		{"reopen", "reconciler", `{"version":2}`, 403, ""},
		{"reopen", "editor", `{"version":2}`, 403, ""},
		{"reopen", "both", `{"version":2,"physical_identity":"original"}`, 400, ""},
		{"reopen", "both", `{"version":2}`, 409, "graph_physical_target_unverified"},
	} {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/graph/graphs/%d/%s", kg.ID, tc.path), strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Accept-Language", "en")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status || tc.code != "" && !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("%s %s: got %d %s", tc.path, tc.token, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "admission_reconciliation_digest") || strings.Contains(w.Body.String(), "lease_token") {
			t.Fatal("private proof exposed")
		}
	}
	var saved models.KnowledgeGraph
	if err = db.First(&saved, kg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !saved.AdmissionClosed || saved.Version != 2 || saved.AdmissionReconciledBy == 0 || saved.AdmissionReconciliationDigest == "" {
		t.Fatalf("control proof lost: %+v", saved)
	}
}
