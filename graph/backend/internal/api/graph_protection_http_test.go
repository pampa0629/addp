package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/falkordb"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/common/resourcetree"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/addp/graph/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type graphProtectionHTTPToken struct{}

func (graphProtectionHTTPToken) Token(context.Context, uint) (string, error) {
	return "addp_at_graph", nil
}
func (graphProtectionHTTPToken) PlatformToken(context.Context) (string, error) {
	return "addp_at_graph", nil
}

func TestGraphProtectionHTTPDeniesOrdinaryUserAndPublicKnowledge(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = db.AutoMigrate(&models.Ontology{}, &models.EntityType{}, &models.RelationType{}, &models.OntologyVersion{}, &models.TenantAdmissionLock{}, &models.GraphOperation{}, &models.BuildExecutionControl{}, &models.KnowledgeGraph{}); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("ATTACH DATABASE ':memory:' AS graph").Error; err != nil {
		t.Fatal(err)
	}
	provider := &falkordb.FalkorDBPlugin{}
	path, err := resourcetree.EngineCatalogPathFromLocator(provider.EngineCatalogModel(), &resourcetree.ResourceLocator{EngineID: 13, Type: resourcetree.TypeGraph, Path: []string{"protected.graph/中文"}})
	if err != nil {
		t.Fatal(err)
	}
	ontology := models.Ontology{ID: 1, TenantID: 7, Name: "Public ontology"}
	if err = db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	graph := models.KnowledgeGraph{ID: 1, TenantID: 7, OntologyID: 1, Name: "Public protected graph", EngineID: 13, CatalogPath: path, IndexManagement: models.GraphIndexesManaged, IsPublic: true}
	if err = db.Create(&graph).Error; err != nil {
		t.Fatal(err)
	}
	caps, err := plugin.MarshalEngineCapabilities(provider.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	capabilities := commonmodels.JSONString(caps)
	// No graph server exists here: a denied read must never reach the native connection.
	engine := commonmodels.Engine{Version:1,ID: 13, EngineType: provider.Type(), LifecycleState: commonmodels.EngineLifecycleActive, ConnectionStatus: commonmodels.EngineConnectionOnline, Capabilities: &capabilities, ConnectionInfo: commonmodels.ConnectionInfo{"host": "127.0.0.1", "port": 1, "password": "private-source-password"}}
	var engineReads atomic.Int64
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/engines/13" || r.Header.Get("Authorization") != "Bearer addp_at_graph" {
			http.Error(w, "denied", 403)
			return
		}
		engineReads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engine)
	}))
	defer system.Close()
	store, err := projectionstore.Migrate(db, "graph", "graph", nil)
	if err != nil {
		t.Fatal(err)
	}
	gate := service.NewGraphProtectionGate(store)
	graphRepo, ontologyRepo := repository.NewKnowledgeGraphRepository(db), repository.NewOntologyRepository(db)
	data := service.NewGraphDataService(graphRepo, ontologyRepo, commonclient.NewSystemServiceClient(system.URL, graphProtectionHTTPToken{}, system.Client()), gate)
	auth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer viewer": {"graph.graph.read"}, "Bearer no-permission": {"graph.ontology.read"}})
	defer auth.Close()
	router := SetupRouter(&config.Config{SystemServiceURL: auth.URL}, nil, nil, NewBrowseHandler(data, nil), nil, nil, nil, NewServiceHandler(service.NewKnowledgeService(data, ontologyRepo, graphRepo)), modulelifecycle.NewStandalone("graph"))
	target, err := dataprotection.DataItemTargetFromCatalogPath(provider.EngineCatalogModel(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"not-synchronized", "managed"} {
		if state == "managed" {
			projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: "protected", Revision: "00000000000000000001", ConsumerOwner: "graph", State: dataprotection.ProjectionStateEnrolling, Target: target, TargetEngineID: 13, Rules: []dataprotection.Rule{}, ValidFrom: time.Now().UTC().Add(-time.Minute)}
			if err = projection.Seal(); err != nil {
				t.Fatal(err)
			}
			if err = store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "installed", Changes: []dataprotection.ProjectionChange{{ChangeID: "install", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}}, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		for _, route := range []struct{ path, token string }{
			{"/api/v1/graph/graphs/1/browse-snapshot", "viewer"},
			{"/api/v1/graph/kg/1/entities/Person", ""},
			{"/api/v1/graph/kg/1/entities/Person/0", ""},
			{"/api/v1/graph/kg/1/nodes/0/neighbors", ""},
			{"/api/v1/graph/kg/1/search?q=name", ""},
			{"/api/v1/graph/kg/1/ontology", ""},
			{"/api/v1/graph/kg/1/stats", ""},
			{"/api/v1/graph/kg/1/stats", "no-permission"},
		} {
			t.Run(fmt.Sprintf("%s_%s", state, route.path), func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, route.path, nil)
				request.Header.Set("Accept-Language", "en")
				if route.token != "" {
					request.Header.Set("Authorization", "Bearer "+route.token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != 403 || !strings.Contains(response.Body.String(), "Data protection") || strings.Contains(response.Body.String(), "private-source-password") {
					t.Fatalf("unsafe protection response: %d %s", response.Code, response.Body.String())
				}
				if err := gate.ReadyToAcknowledge(t.Context(), 7, "installed"); err != nil {
					t.Fatal("request leaked a protection read", err)
				}
			})
		}
	}
	beforeInvalid := engineReads.Load()
	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/graph/kg/1/stats", nil)
	invalid.Header.Set("Authorization", "Bearer invalid")
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != 401 || engineReads.Load() != beforeInvalid {
		t.Fatal("invalid credentials fell back to anonymous access", invalidResponse.Code)
	}
	graph.IsPublic = false
	if err := graphRepo.DB().Save(&graph).Error; err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/v1/graph/kg/1/entities/Person", ""},
		{"GET", "/api/v1/graph/kg/1/entities/Person/0", ""},
		{"GET", "/api/v1/graph/kg/1/nodes/0/neighbors", ""},
		{"GET", "/api/v1/graph/kg/1/search?q=name", ""},
		{"GET", "/api/v1/graph/kg/1/ontology", ""},
		{"GET", "/api/v1/graph/kg/1/stats", ""},
		{"POST", "/api/v1/graph/kg/1/paths", `{"source_node_id":"0","target_node_id":"1"}`},
		{"POST", "/api/v1/graph/kg/1/subgraph", `{"node_id":"0"}`},
	} {
		for _, token := range []string{"no-permission", "viewer", ""} {
			t.Run("private_"+route.path+"_"+token, func(t *testing.T) {
				before := engineReads.Load()
				request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				request.Header.Set("Accept-Language", "en")
				request.Header.Set("Content-Type", "application/json")
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want, reason := 403, "Data protection"
				if token == "no-permission" {
					reason = "permission_denied"
				}
				if token == "" {
					want, reason = 401, ""
				}
				if response.Code != want || !strings.Contains(response.Body.String(), reason) {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
				if token != "viewer" && engineReads.Load() != before {
					t.Fatal("unauthorized request obtained an engine connection")
				}
			})
		}
	}
	otherTenant := authtest.NewTenantUserAuthContextServer(t, "8", map[string][]string{"Bearer viewer": {"graph.graph.read"}})
	defer otherTenant.Close()
	otherRouter := SetupRouter(&config.Config{SystemServiceURL: otherTenant.URL}, nil, nil, NewBrowseHandler(data, nil), nil, nil, nil, NewServiceHandler(service.NewKnowledgeService(data, ontologyRepo, graphRepo)), modulelifecycle.NewStandalone("graph"))
	before := engineReads.Load()
	for _, path := range []string{"/api/v1/graph/graphs/1/browse-snapshot", "/api/v1/graph/kg/1/entities/Person"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer viewer")
		response := httptest.NewRecorder()
		otherRouter.ServeHTTP(response, request)
		if response.Code != 404 || engineReads.Load() != before {
			t.Fatalf("cross tenant read reached engine: %d %s", response.Code, response.Body.String())
		}
	}

}
