package api

import (
	"context"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"testing"
	"time"
)

type managementScopeFixture struct{ calls int }

func (f *managementScopeFixture) GetMetaScanManagementScope(_ context.Context, actor engineaccess.Actor, engine int64, operation string) (*shared.EngineAccessHandlingScope, error) {
	f.calls++
	return &shared.EngineAccessHandlingScope{TenantID: actor.TenantID, EngineID: engine, Operator: shared.SharingFulfillmentOperator{PrincipalID: actor.PrincipalID, MembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion}, VerifiedAt: time.Now()}, nil
}
func TestManagementScopeRequiresExactCurrentHumanPermission(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete", "execute", "cancel"} {
		path := "/api/v1/system/engines/9/scan_" + operation + "_scope"
		for _, current := range []shared.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-meta")} {
			fixture := &managementScopeFixture{}
			authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
			if err != nil {
				t.Fatal(err)
			}
			user, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			if err := RegisterMetaScanManagementScopeRoute(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: user}, &MetaScanManagementScopeHandler{service: fixture}); err != nil {
				t.Fatal(err)
			}
			collectionPermissions(&current, "meta.scan_task.read")
			collectionRequest(t, router, "GET", path, "", 403)
			collectionPermissions(&current, "meta.scan_task."+operation)
			if current.Context.Type != "tenant" || current.Principal.Type != "user" {
				collectionRequest(t, router, "GET", path, "", 403)
				if fixture.calls != 0 {
					t.Fatal("non-human/tenant called scope service")
				}
				continue
			}
			collectionRequest(t, router, "GET", path+"?tenant_id=8", "", 400)
			collectionRequest(t, router, "GET", path, `{"operation":"execute"}`, 400)
			response := collectionRequest(t, router, "GET", path, "", 200)
			if fixture.calls != 1 || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unexpected scope call or cacheable observation")
			}
		}
	}

}
