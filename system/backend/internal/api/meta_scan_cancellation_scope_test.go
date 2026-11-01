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

type cancellationScopeFixture struct{ calls int }

func (f *cancellationScopeFixture) GetMetaScanCancellationScope(_ context.Context, actor engineaccess.Actor, engine int64) (*shared.EngineAccessHandlingScope, error) {
	f.calls++
	return &shared.EngineAccessHandlingScope{TenantID: actor.TenantID, EngineID: engine, Operator: shared.SharingFulfillmentOperator{PrincipalID: actor.PrincipalID, MembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion}, VerifiedAt: time.Now()}, nil
}
func TestCancellationScopeRequiresCurrentHumanCancelPermission(t *testing.T) {
	const path = "/api/v1/system/engines/9/scan_cancellation_scope"
	for _, current := range []shared.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-meta")} {
		fixture := &cancellationScopeFixture{}
		authenticate, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: &current})
		if err != nil {
			t.Fatal(err)
		}
		user, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
		if err != nil {
			t.Fatal(err)
		}
		router := gin.New()
		if err := RegisterMetaScanCancellationScopeRoute(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authenticate, UserAccessCredential: user}, &MetaScanCancellationScopeHandler{service: fixture}); err != nil {
			t.Fatal(err)
		}
		collectionPermissions(&current, "meta.scan_task.execute")
		collectionRequest(t, router, "GET", path, "", 403)
		collectionPermissions(&current, "meta.scan_task.cancel")
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
