package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	graphauthorization "github.com/addp/graph/internal/authorization"
	"github.com/addp/graph/internal/config"
)

func TestApplyInferredSchemaRequiresGraphReadAndOntologyUpdate(t *testing.T) {
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer graph-update":    {graphauthorization.PermissionGraphGraphUpdate},
		"Bearer graph-read":      {graphauthorization.PermissionGraphGraphRead},
		"Bearer ontology-update": {graphauthorization.PermissionGraphOntologyUpdate},
		"Bearer both":            {graphauthorization.PermissionGraphGraphRead, graphauthorization.PermissionGraphOntologyUpdate},
	})
	defer authServer.Close()

	router := SetupRouter(
		&config.Config{SystemServiceURL: authServer.URL},
		nil, nil, &BrowseHandler{}, nil, nil, nil, nil,
		modulelifecycle.NewStandalone("graph"),
	)

	for _, test := range []struct {
		name, token string
		want        int
	}{
		{name: "graph update cannot write ontology", token: "graph-update", want: http.StatusForbidden},
		{name: "graph read alone cannot write ontology", token: "graph-read", want: http.StatusForbidden},
		{name: "ontology update alone cannot read graph", token: "ontology-update", want: http.StatusForbidden},
		{name: "both permissions enter handler", token: "both", want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/graph/graphs/1/infer-schema/apply", strings.NewReader("{"))
			request.Header.Set("Authorization", "Bearer "+test.token)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestEngineSchemaInferenceRequiresCatalogAndOntologyPermissions(t *testing.T) {
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer ontology-read":   {graphauthorization.PermissionGraphOntologyRead},
		"Bearer ontology-update": {graphauthorization.PermissionGraphOntologyUpdate},
		"Bearer catalog-read":    {"system.engine_catalog.read"},
		"Bearer preview":         {graphauthorization.PermissionGraphOntologyRead, "system.engine_catalog.read"},
		"Bearer apply":           {graphauthorization.PermissionGraphOntologyUpdate, "system.engine_catalog.read"},
	})
	defer authServer.Close()

	router := SetupRouter(
		&config.Config{SystemServiceURL: authServer.URL},
		&OntologyHandler{}, nil, nil, nil, nil, nil, nil,
		modulelifecycle.NewStandalone("graph"),
	)

	for _, test := range []struct {
		name, method, path, token string
		want                      int
	}{
		{name: "ontology read cannot inspect engine", method: http.MethodGet, path: "/api/v1/graph/ontologies/infer-schema/from-engine", token: "ontology-read", want: http.StatusForbidden},
		{name: "catalog read cannot inspect ontology", method: http.MethodGet, path: "/api/v1/graph/ontologies/infer-schema/from-engine", token: "catalog-read", want: http.StatusForbidden},
		{name: "preview enters handler", method: http.MethodGet, path: "/api/v1/graph/ontologies/infer-schema/from-engine", token: "preview", want: http.StatusServiceUnavailable},
		{name: "ontology update cannot infer from engine", method: http.MethodPost, path: "/api/v1/graph/ontologies/1/infer-schema/from-engine/apply", token: "ontology-update", want: http.StatusForbidden},
		{name: "catalog read cannot update ontology", method: http.MethodPost, path: "/api/v1/graph/ontologies/1/infer-schema/from-engine/apply", token: "catalog-read", want: http.StatusForbidden},
		{name: "apply enters handler", method: http.MethodPost, path: "/api/v1/graph/ontologies/1/infer-schema/from-engine/apply", token: "apply", want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/graph/ontologies/neo4j-engines" {
			t.Fatal("full-engine listing route must not be registered")
		}
	}
}

func TestGraphSchemaPreviewRequiresOntologyReadWhenComparingTarget(t *testing.T) {
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer graph-read": {graphauthorization.PermissionGraphGraphRead},
	})
	defer authServer.Close()

	router := SetupRouter(
		&config.Config{SystemServiceURL: authServer.URL},
		nil, nil, &BrowseHandler{}, nil, nil, nil, nil,
		modulelifecycle.NewStandalone("graph"),
	)

	for _, test := range []struct {
		name, query string
		want        int
	}{
		{name: "target ontology needs read permission", query: "ontology_id=5", want: http.StatusForbidden},
		{name: "invalid ontology id rejected", query: "ontology_id=5oops", want: http.StatusBadRequest},
		{name: "empty ontology id rejected", query: "ontology_id=", want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/graph/graphs/1/infer-schema?"+test.query, nil)
			request.Header.Set("Authorization", "Bearer graph-read")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
