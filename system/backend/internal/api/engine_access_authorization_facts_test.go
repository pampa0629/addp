package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

func authorizationFactsRequest(t *testing.T, router http.Handler, path string, expected int) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer user-token")
	router.ServeHTTP(response, request)
	if response.Code != expected {
		t.Fatalf("GET %s = %d want %d body=%s", path, response.Code, expected, response.Body.String())
	}
	return response
}

type testAuthorizationFactsService struct {
	calls int
	err   error
}

func (s *testAuthorizationFactsService) GetAuthorizationFacts(_ context.Context, _ engineaccess.Actor, id int64, fingerprint string) (*dataitem.AuthorizationFacts, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	facts := &dataitem.AuthorizationFacts{SchemaVersion: dataitem.AuthorizationFactsSchemaV1, EngineID: uint(id), ItemFingerprint: fingerprint, ItemType: "table", FullName: "outdoor.persons", BasisState: dataitem.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(), Boundary: &dataitem.AuthorizationBoundary{Layout: "single", NativeTarget: plugin.TabularItemPath(uint(id), "schema", "outdoor", "persons")}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		return nil, err
	}
	facts.BoundaryHash = &hash
	return facts, nil
}

func authorizationFactsTestRouter(t *testing.T, projection *commonauth.AuthContext, s authorizationFactsService) *gin.Engine {
	t.Helper()
	if err := commonauth.ValidateAuthContext(*projection); err != nil {
		t.Fatal(err)
	}
	auth, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: projection})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if err := RegisterEngineAccessAuthorizationFactsRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: auth, UserAccessCredential: credential}, &EngineAccessAuthorizationFactsHandler{service: s}); err != nil {
		t.Fatal(err)
	}
	return router
}

func TestAuthorizationFactsRequiresQualifiedTenantUser(t *testing.T) {
	fingerprint := commonmodels.GenerateItemFingerprint(2, "outdoor.persons")
	path := "/api/v1/system/engines/2/data-items/" + fingerprint + "/authorization-facts"
	for _, projection := range []commonauth.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-system")} {
		s := &testAuthorizationFactsService{}
		router := authorizationFactsTestRouter(t, &projection, s)
		authorizationFactsRequest(t, router, path, 403)
		if s.calls != 0 {
			t.Fatal("unauthorized request reached resource consumer")
		}
	}
	projection := testIAMActorContext("tenant")
	projection.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.reader", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: projection.Context.TenantID}, Permissions: []string{"system.engine_access_grant.read"}}}
	s := &testAuthorizationFactsService{}
	router := authorizationFactsTestRouter(t, &projection, s)
	response := authorizationFactsRequest(t, router, path, 200)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("facts are cacheable")
	}
	authorizationFactsRequest(t, router, path+"?tenant_id=99", 400)
	authorizationFactsRequest(t, router, "/api/v1/system/engines/02/data-items/"+fingerprint+"/authorization-facts", 400)
	s.err = engineaccess.ErrAuthorizationFactsUnavailable
	authorizationFactsRequest(t, router, path, 503)
}
