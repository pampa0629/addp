package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonAuth "github.com/addp/common/middleware/auth"
	metaauthorization "github.com/addp/meta/internal/authorization"
	"github.com/gin-gonic/gin"
)

func TestMetaDelegatedToolPoliciesMatchPublishedToolPermissions(t *testing.T) {
	policies := metaDelegatedToolPolicies()
	for _, route := range []struct {
		path  string
		scope string
	}{
		{path: "GET /api/v1/meta/resource-tree/:engine_id", scope: "resource.children.list"},
		{path: "GET /api/v1/meta/resource-tree/:engine_id/node", scope: "resource.children.list"},
		{path: "GET /api/v1/meta/resource-tree/:engine_id/ancestors", scope: "resource.ancestors.get"},
	} {
		policy := policies[route.path]
		if !reflect.DeepEqual(policy.RequiredScopes, []string{route.scope}) {
			t.Fatalf("%s scopes = %#v", route.scope, policy.RequiredScopes)
		}
		if !reflect.DeepEqual(policy.RequiredPermissions, []string{metaauthorization.PermissionMetaCatalogRead}) {
			t.Fatalf("%s permissions = %#v", route.scope, policy.RequiredPermissions)
		}
	}
}

func TestDelegatedResourceRootRejectsUnboundedOrAmbiguousQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"", "expand_depth=-1", "expand_depth=2", "expand_depth=1&expand_depth=1", "expand_depth=1&extra=true", "expand_depth=%zz"} {
		t.Run(query, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				a := authtest.NewTenantUserAuthContext("7", "91", []string{"meta.catalog.read"})
				a.Token.Type = "delegated_access_token"
				a.Client.ScopeMode = "restricted"
				a.Client.Audiences = []string{"meta"}
				a.Client.Scopes = []string{"resource.children.list"}
				a.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *a.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
				if err := commonAuth.SetAuthContextForGin(c, a); err != nil {
					t.Fatal(err)
				}
			})
			// Invalid queries must stop before any owner service or engine access.
			h := &Handler{}
			r.GET("/resource-tree/:engine_id", h.GetResourceTree)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resource-tree/9?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
