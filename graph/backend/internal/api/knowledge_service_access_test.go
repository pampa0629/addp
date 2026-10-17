package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugins/falkordb"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/common/resourcetree"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeGraphPublicStateUsesCanonicalUpdate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = db.AutoMigrate(&models.Ontology{}, &models.KnowledgeGraph{}); err != nil {
		t.Fatal(err)
	}
	ontology := models.Ontology{ID: 1, TenantID: 7, Name: "test ontology"}
	if err = db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	provider := &falkordb.FalkorDBPlugin{}
	path, err := resourcetree.EngineCatalogPathFromLocator(provider.EngineCatalogModel(), &resourcetree.ResourceLocator{EngineID: 13, Type: resourcetree.TypeGraph, Path: []string{"test graph"}})
	if err != nil {
		t.Fatal(err)
	}
	graph := models.KnowledgeGraph{ID: 1, TenantID: 7, OntologyID: 1, EngineID: 13, CatalogPath: path, IndexManagement: models.GraphIndexesExternal, Name: "private graph"}
	if err = db.Create(&graph).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewKnowledgeGraphRepository(db)
	handler := NewKnowledgeGraphHandler(service.NewKnowledgeGraphService(repo, nil))
	auth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer reader": {"graph.graph.read"}, "Bearer editor": {"graph.graph.update"},
	})
	defer auth.Close()
	router := SetupRouter(&config.Config{SystemServiceURL: auth.URL}, nil, handler, nil, nil, nil, nil, nil, modulelifecycle.NewStandalone("graph"))
	for _, test := range []struct {
		name, method, token, body string
		status                    int
		public                    bool
	}{
		{"reader cannot publish", "PUT", "reader", `{"is_public":true}`, 403, false},
		{"publish", "PUT", "editor", `{"is_public":true}`, 200, true},
		{"reload sees saved state", "GET", "reader", "", 200, true},
		{"rename preserves publication", "PUT", "editor", `{"name":"renamed"}`, 200, true},
		{"invalid boolean rejected", "PUT", "editor", `{"is_public":"false"}`, 400, true},
		{"unpublish", "PUT", "editor", `{"is_public":false}`, 200, false},
		{"reload sees private state", "GET", "reader", "", 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, "/api/v1/graph/graphs/1", strings.NewReader(test.body))
			req.Header.Set("Authorization", "Bearer "+test.token)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			stored, err := repo.GetByID(1, 7)
			if err != nil || stored.IsPublic != test.public {
				t.Fatalf("persisted public state: %v %v", stored, err)
			}
			if response.Code == http.StatusOK {
				var result models.KnowledgeGraph
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.IsPublic != test.public {
					t.Fatalf("non-canonical response: %s %v", response.Body.String(), err)
				}
			}
		})
	}
	foreignAuth := authtest.NewTenantUserAuthContextServer(t, "8", map[string][]string{"Bearer editor": {"graph.graph.update"}})
	defer foreignAuth.Close()
	foreignRouter := SetupRouter(&config.Config{SystemServiceURL: foreignAuth.URL}, nil, handler, nil, nil, nil, nil, nil, modulelifecycle.NewStandalone("graph"))
	request := httptest.NewRequest(http.MethodPut, "/api/v1/graph/graphs/1", strings.NewReader(`{"is_public":true}`))
	request.Header.Set("Authorization", "Bearer editor")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	foreignRouter.ServeHTTP(response, request)
	stored, err := repo.GetByID(1, 7)
	if response.Code != 404 || err != nil || stored.IsPublic {
		t.Fatalf("foreign tenant changed publication: %d %v", response.Code, err)
	}
}
