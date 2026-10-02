package api

import (
	"context"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type unusedHandlingScopeService struct{}

func (unusedHandlingScopeService) GetHandlingScope(context.Context, engineaccess.Actor, int64) (*commonauth.EngineAccessHandlingScope, error) {
	panic("denied request reached handling scope")
}

func handlingScopeTestRouter(t *testing.T, s engineAccessHandlingScopeService, auth *commonauth.AuthContext) *gin.Engine {
	t.Helper()
	if err := commonauth.ValidateAuthContext(*auth); err != nil {
		t.Fatal(err)
	}
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: auth})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if err := RegisterEngineAccessHandlingScopeRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: credential}, &EngineAccessHandlingScopeHandler{service: s}); err != nil {
		t.Fatal(err)
	}
	return router
}

func TestHandlingScopeRequiresIndependentTenantUserPermission(t *testing.T) {
	path := "/api/v1/system/engines/12/access_handling_scope"
	for _, auth := range []commonauth.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-catalog")} {
		router := handlingScopeTestRouter(t, unusedHandlingScopeService{}, &auth)
		engineDelegationTestRequest(t, router, "GET", path, nil, 403)
	}
	auth := testIAMActorContext("tenant")
	auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.handler", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: auth.Context.TenantID}, Permissions: []string{"system.engine_access_fulfillment.create"}}}
	router := handlingScopeTestRouter(t, unusedHandlingScopeService{}, &auth)
	for _, suffix := range []string{"?tenant_id=8", "/invalid"} {
		want := 400
		if suffix == "/invalid" {
			want = 404
		}
		engineDelegationTestRequest(t, router, "GET", path+suffix, nil, want)
	}
	engineDelegationTestRequest(t, router, "GET", "/api/v1/system/engines/012/access_handling_scope", nil, 400)
	engineDelegationTestRequest(t, router, "POST", path, nil, 404)
	auth.Authorization.RoleAssignments[0].Permissions = []string{"catalog.sharing_decision.create", "system.engine_access_delegation.read", "system.engine_access_fulfillment.execute"}
	engineDelegationTestRequest(t, router, "GET", path, nil, 403)
}
