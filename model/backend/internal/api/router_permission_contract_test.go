package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/gin-gonic/gin"
)

func TestModelRoutesEnforcePermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		method      string
		path        string
		permissions []string
		testAllowed bool
	}{
		{name: "entity list", method: http.MethodGet, path: "/api/v1/model/entities", permissions: []string{"model.entity.read"}},
		{name: "entity create", method: http.MethodPost, path: "/api/v1/model/entities", permissions: []string{"model.entity.create"}, testAllowed: true},
		{name: "entity read", method: http.MethodGet, path: "/api/v1/model/entities/invalid", permissions: []string{"model.entity.read"}, testAllowed: true},
		{name: "entity professional relations", method: http.MethodGet, path: "/api/v1/model/entities/invalid/relations", permissions: []string{"model.entity.read", "model.entity_relation.read"}, testAllowed: true},
		{name: "entity update", method: http.MethodPut, path: "/api/v1/model/entities/1", permissions: []string{"model.entity.update"}, testAllowed: true},
		{name: "entity delete", method: http.MethodDelete, path: "/api/v1/model/entities/1", permissions: []string{"model.entity.delete"}, testAllowed: true},
		{name: "entity approve", method: http.MethodPost, path: "/api/v1/model/entities/1/approve", permissions: []string{"model.entity.approve"}, testAllowed: true},
		{name: "entity reopen", method: http.MethodPost, path: "/api/v1/model/entities/1/reopen", permissions: []string{"model.entity.update"}, testAllowed: true},
		{name: "attribute list", method: http.MethodGet, path: "/api/v1/model/entities/invalid/attributes", permissions: []string{"model.entity.read"}, testAllowed: true},
		{name: "attribute create", method: http.MethodPost, path: "/api/v1/model/entities/1/attributes", permissions: []string{"model.entity.create"}, testAllowed: true},
		{name: "attribute update", method: http.MethodPut, path: "/api/v1/model/entities/1/attributes/1", permissions: []string{"model.entity.update"}, testAllowed: true},
		{name: "attribute delete", method: http.MethodDelete, path: "/api/v1/model/entities/1/attributes/1", permissions: []string{"model.entity.delete"}, testAllowed: true},
		{name: "mermaid import", method: http.MethodPost, path: "/api/v1/model/entities/import-mermaid", permissions: []string{"model.entity.create", "model.entity.delete", "model.entity_relation.create", "model.entity_relation.delete"}, testAllowed: true},
		{name: "mermaid export", method: http.MethodGet, path: "/api/v1/model/entities/export-mermaid", permissions: []string{"model.entity.read", "model.entity_relation.read"}},
		{name: "entity relation list", method: http.MethodGet, path: "/api/v1/model/entity-relations", permissions: []string{"model.entity_relation.read"}},
		{name: "entity relation create", method: http.MethodPost, path: "/api/v1/model/entity-relations", permissions: []string{"model.entity_relation.create"}, testAllowed: true},
		{name: "entity relation read", method: http.MethodGet, path: "/api/v1/model/entity-relations/invalid", permissions: []string{"model.entity_relation.read"}, testAllowed: true},
		{name: "entity relation update", method: http.MethodPut, path: "/api/v1/model/entity-relations/1", permissions: []string{"model.entity_relation.update"}, testAllowed: true},
		{name: "entity relation delete", method: http.MethodDelete, path: "/api/v1/model/entity-relations/1", permissions: []string{"model.entity_relation.delete"}, testAllowed: true},
		{name: "logical table list", method: http.MethodGet, path: "/api/v1/model/logical-tables", permissions: []string{"model.logical_model.read"}},
		{name: "logical table create", method: http.MethodPost, path: "/api/v1/model/logical-tables", permissions: []string{"model.logical_model.create"}, testAllowed: true},
		{name: "logical table read", method: http.MethodGet, path: "/api/v1/model/logical-tables/invalid", permissions: []string{"model.logical_model.read"}, testAllowed: true},
		{name: "logical table professional relations", method: http.MethodGet, path: "/api/v1/model/logical-tables/invalid/relations", permissions: []string{"model.logical_model.read"}, testAllowed: true},
		{name: "logical table update", method: http.MethodPut, path: "/api/v1/model/logical-tables/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "logical table delete", method: http.MethodDelete, path: "/api/v1/model/logical-tables/1", permissions: []string{"model.logical_model.delete"}, testAllowed: true},
		{name: "materialized target decommission", method: http.MethodDelete, path: "/api/v1/model/logical-tables/1/materialized-target", permissions: []string{"model.materialized_target.delete"}, testAllowed: true},
		{name: "logical table approve", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/approve", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "logical table reopen", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/reopen", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "logical field list", method: http.MethodGet, path: "/api/v1/model/logical-tables/invalid/fields", permissions: []string{"model.logical_model.read"}, testAllowed: true},
		{name: "logical field create", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/fields", permissions: []string{"model.logical_model.create"}, testAllowed: true},
		{name: "logical field update", method: http.MethodPut, path: "/api/v1/model/logical-tables/1/fields/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "logical field delete", method: http.MethodDelete, path: "/api/v1/model/logical-tables/1/fields/1", permissions: []string{"model.logical_model.delete"}, testAllowed: true},
		{name: "dimension hierarchy list", method: http.MethodGet, path: "/api/v1/model/logical-tables/invalid/dimension-hierarchies", permissions: []string{"model.logical_model.read"}, testAllowed: true},
		{name: "dimension hierarchy create", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/dimension-hierarchies", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dimension hierarchy update", method: http.MethodPut, path: "/api/v1/model/logical-tables/1/dimension-hierarchies/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dimension hierarchy delete", method: http.MethodDelete, path: "/api/v1/model/logical-tables/1/dimension-hierarchies/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dimension hierarchy level create", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/dimension-hierarchies/1/levels", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dimension hierarchy level update", method: http.MethodPut, path: "/api/v1/model/logical-tables/1/dimension-hierarchies/1/levels/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dimension hierarchy level delete", method: http.MethodDelete, path: "/api/v1/model/logical-tables/1/dimension-hierarchies/1/levels/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "DDL preview", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/preview-ddl", permissions: []string{"model.logical_model.read"}, testAllowed: true},
		{name: "fact metric list", method: http.MethodGet, path: "/api/v1/model/logical-tables/invalid/metrics", permissions: []string{"model.logical_model.read"}, testAllowed: true},
		{name: "fact metric add", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/metrics", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "fact metric remove", method: http.MethodDelete, path: "/api/v1/model/logical-tables/1/metrics/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dimension relation list", method: http.MethodGet, path: "/api/v1/model/logical-tables/invalid/dimension-relations", permissions: []string{"model.logical_model.read"}, testAllowed: true},
		{name: "dimension relation add", method: http.MethodPost, path: "/api/v1/model/logical-tables/1/dimension-relations", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dimension relation remove", method: http.MethodDelete, path: "/api/v1/model/logical-tables/1/dimension-relations/1", permissions: []string{"model.logical_model.update"}, testAllowed: true},
		{name: "dw layer list", method: http.MethodGet, path: "/api/v1/model/dw-layers", permissions: []string{"model.dw_layer.read"}},
		{name: "dw layer create", method: http.MethodPost, path: "/api/v1/model/dw-layers", permissions: []string{"model.dw_layer.create"}, testAllowed: true},
		{name: "dw layer read", method: http.MethodGet, path: "/api/v1/model/dw-layers/invalid", permissions: []string{"model.dw_layer.read"}, testAllowed: true},
		{name: "dw layer update", method: http.MethodPut, path: "/api/v1/model/dw-layers/1", permissions: []string{"model.dw_layer.update"}, testAllowed: true},
		{name: "dw layer delete", method: http.MethodDelete, path: "/api/v1/model/dw-layers/1", permissions: []string{"model.dw_layer.delete"}, testAllowed: true},
		{name: "materialization group list", method: http.MethodGet, path: "/api/v1/model/materialization-groups", permissions: []string{"model.materialization_group.read"}},
		{name: "materialization group create", method: http.MethodPost, path: "/api/v1/model/materialization-groups", permissions: []string{"model.materialization_group.create"}, testAllowed: true},
		{name: "materialization group read", method: http.MethodGet, path: "/api/v1/model/materialization-groups/invalid", permissions: []string{"model.materialization_group.read"}, testAllowed: true},
		{name: "materialization group update", method: http.MethodPut, path: "/api/v1/model/materialization-groups/invalid", permissions: []string{"model.materialization_group.update"}, testAllowed: true},
		{name: "materialization group delete", method: http.MethodDelete, path: "/api/v1/model/materialization-groups/invalid", permissions: []string{"model.materialization_group.delete"}, testAllowed: true},
	}
	authContexts := map[string][]string{
		"Bearer no-model-permissions": {"standard.domain.read"},
	}
	for index, test := range tests {
		authContexts[fmt.Sprintf("Bearer allowed-%d", index)] = test.permissions
		for omittedIndex := range test.permissions {
			partialPermissions := append([]string(nil), test.permissions[:omittedIndex]...)
			partialPermissions = append(partialPermissions, test.permissions[omittedIndex+1:]...)
			authContexts[fmt.Sprintf("Bearer partial-%d-%d", index, omittedIndex)] = partialPermissions
		}
	}
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", authContexts)
	defer authServer.Close()

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, authServer.URL, nil, modulelifecycle.NewStandalone("model"))

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer no-model-permissions")
			request.Header.Set("Accept-Language", "en")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got == "" {
				t.Fatalf("missing error content type; body=%s", response.Body.String())
			}
			if response.Body.String() == "" || !strings.Contains(response.Body.String(), `"error_code":"permission_denied"`) {
				t.Fatalf("body = %s, want permission_denied", response.Body.String())
			}

			if test.testAllowed {
				allowedRequest := httptest.NewRequest(test.method, test.path, nil)
				allowedRequest.Header.Set("Authorization", fmt.Sprintf("Bearer allowed-%d", index))
				allowedResponse := httptest.NewRecorder()
				router.ServeHTTP(allowedResponse, allowedRequest)
				if allowedResponse.Code == http.StatusUnauthorized || allowedResponse.Code == http.StatusForbidden {
					t.Fatalf("route rejected exact permissions %v; body=%s", test.permissions, allowedResponse.Body.String())
				}
			}

			if len(test.permissions) > 1 {
				for omittedIndex, omittedPermission := range test.permissions {
					partialRequest := httptest.NewRequest(test.method, test.path, nil)
					partialRequest.Header.Set("Authorization", fmt.Sprintf("Bearer partial-%d-%d", index, omittedIndex))
					partialResponse := httptest.NewRecorder()
					router.ServeHTTP(partialResponse, partialRequest)
					if partialResponse.Code != http.StatusForbidden {
						t.Fatalf("status without %q = %d, want %d; body=%s", omittedPermission, partialResponse.Code, http.StatusForbidden, partialResponse.Body.String())
					}
				}
			}
		})
	}
}
