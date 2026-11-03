package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type metadataRefreshFixture struct {
	calls   int
	corrupt bool
}

func (f *metadataRefreshFixture) GetMetadataRefreshScope(_ context.Context, token string, query shared.MetadataRefreshQuery) (*shared.MetadataRefreshScope, error) {
	f.calls++
	if f.corrupt {
		return nil, nil
	}
	binding := factstest.Binding(plugin.TabularItemPath(9, "schema", "public", "people"))
	return &shared.MetadataRefreshScope{TenantID: 7, EngineVersion: "1", Query: query, Operator: shared.SharingFulfillmentOperator{PrincipalID: 3, MembershipID: 4, AuthorizationVersion: 1}, ObservedAt: time.Now(), Ranges: []shared.MetadataRefreshRange{{Item: &binding}}}, nil
}

func TestMetadataRefreshScopeRejectsWrongCredentialAndRootBeforeOwnerIO(t *testing.T) {
	for _, current := range []shared.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-meta")} {
		fixture := &metadataRefreshFixture{}
		authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
		if err != nil {
			t.Fatal(err)
		}
		user, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
		if err != nil {
			t.Fatal(err)
		}
		router := gin.New()
		if err := RegisterMetadataRefreshScopeRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: user}, &MetadataRefreshScopeHandler{service: fixture}); err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/system/engine-access/metadata-refresh-scopes/query"
		query := shared.MetadataRefreshQuery{Target: plugin.TabularNamespacePath(9, "schema", "public"), Kind: "node"}
		raw, _ := json.Marshal(query)
		collectionPermissions(&current, "meta.catalog.read")
		collectionRequest(t, router, "POST", path, string(raw), 403)
		collectionPermissions(&current, "meta.scan_task.execute")
		if current.Context.Type != "tenant" || current.Principal.Type != "user" {
			collectionRequest(t, router, "POST", path, string(raw), 403)
		} else {
			collectionRequest(t, router, "POST", path+"?tenant_id=99", string(raw), 400)
			collectionRequest(t, router, "POST", path, `{"kind":"node","target":{"version":"catalog.path/v1","engine_id":9,"segments":[{"term":"server","kind":"server","name":""}]}}`, 400)
			collectionRequest(t, router, "POST", path, `{"kind":"node","kind":"item"}`, 400)
			if fixture.calls != 0 {
				t.Fatal("invalid input reached refresh owner")
			}
			response := collectionRequest(t, router, "POST", path, string(raw), 200)
			if response.Header().Get("Cache-Control") != "no-store" || fixture.calls != 1 {
				t.Fatal("scope cached or request repeated")
			}
			fixture.corrupt = true
			collectionRequest(t, router, "POST", path, string(raw), 500)
		}
	}
}
