package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type nodePageServiceFunc func(context.Context, engineaccess.Actor, int64, authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error)

func (f nodePageServiceFunc) ListAuthorizationNodes(c context.Context, a engineaccess.Actor, e int64, q authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error) {
	return f(c, a, e, q)
}

func TestNodeDiscoveryHTTPRequiresGrantReadAndStrictTarget(t *testing.T) {
	projection := testIAMActorContext("tenant")
	calls := 0
	service := nodePageServiceFunc(func(_ context.Context, a engineaccess.Actor, engineID int64, q authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error) {
		calls++
		if engineID != 2 || a.TenantID <= 0 || a.PrincipalID <= 0 {
			t.Fatal("discovery lost caller or engine scope")
		}
		return &authorizationfacts.NodeIdentityPage{Parent: authorizationfacts.NodeIdentity{TenantID: uint(a.TenantID), Target: q.Target, Exists: true}, Nodes: []authorizationfacts.NodeIdentity{}, Page: q.Page, PageSize: q.PageSize}, nil
	})
	routerFor := func() *gin.Engine {
		auth, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &projection})
		if err != nil {
			t.Fatal(err)
		}
		credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
		if err != nil {
			t.Fatal(err)
		}
		router := gin.New()
		router.Use(commoni18n.I18nMiddleware())
		if err := RegisterEngineAccessNodeIdentityRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: auth, UserAccessCredential: credential}, &EngineAccessNodeIdentityHandler{service: service}); err != nil {
			t.Fatal(err)
		}
		return router
	}
	query := authorizationfacts.NodeIdentityPageQuery{Target: plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 2), Page: 1, PageSize: 100}
	body, _ := json.Marshal(query)
	request := func(raw, suffix string, status int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/system/engines/2/authorization-nodes/query"+suffix, strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer user-token")
		w := httptest.NewRecorder()
		routerFor().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("status=%d expected=%d body=%s", w.Code, status, w.Body)
		}
		if status == 200 && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable identity navigation")
		}
	}
	request(string(body), "", 403)
	if calls != 0 {
		t.Fatal("unqualified request reached Meta adapter")
	}
	projection.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.reader", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: shared.AssignmentScope{Type: "tenant", TenantID: projection.Context.TenantID}, Permissions: []string{"system.engine_access_grant.read"}}}
	request(string(body), "", 200)
	for _, bad := range []string{`{}`, strings.Replace(string(body), `"engine_id":2`, `"engine_id":3`, 1), strings.Replace(string(body), `"page":1`, `"page":0`, 1), strings.TrimSuffix(string(body), "}") + `,"tenant_id":8}`, strings.Replace(string(body), `"page":1`, `"page":1,"page":1`, 1)} {
		request(bad, "", 400)
	}
	request(string(body), "?tenant_id=8", 400)
	request(strings.Repeat(" ", authorizationfacts.MaxNodeIdentityBytes+1), "", 413)
	if calls != 1 {
		t.Fatalf("invalid query reached identity consumer: %d", calls)
	}
}
