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
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type grantServiceFunc func(context.Context, engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error)

func (f grantServiceFunc) InspectSourceGrants(context.Context, engineaccess.Actor, int64, int64, engineplugin.EngineCatalogPath) (*engineaccess.SourceGrantInspection, error) {
	panic("unexpected grant inspection")
}

func (f grantServiceFunc) RevokeGrant(ctx context.Context, input engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error) {
	return f(ctx, input)
}

func (f grantServiceFunc) CreateIndependentGrant(context.Context, engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error) {
	panic("unexpected grant creation")
}
func (f grantServiceFunc) ListSourceGrants(context.Context, engineaccess.Actor, int64, int, int, engineaccess.SourceGrantFilter) ([]engineaccess.SourceGrantView, int64, error) {
	panic("unexpected grant listing")
}
func (f grantServiceFunc) ListSourceGrantRelations(context.Context, engineaccess.Actor, int64, int, int, engineaccess.SourceGrantFilter) ([]engineaccess.SourceGrantRelation, int64, error) {
	panic("unexpected relation listing")
}

func grantTestRouter(t *testing.T, projection *shared.AuthContext, service engineAccessGrantService) *gin.Engine {
	t.Helper()
	authentication, err := middleware.NewIAMAuthenticationMiddleware(iamActorResolver{authContext: projection})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	if err := RegisterEngineAccessGrantRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: credential}, &EngineAccessGrantHandler{service: service}); err != nil {
		t.Fatal(err)
	}
	return router
}

func TestEngineGrantRevocationHTTPContract(t *testing.T) {
	id := uuid.New()
	const prefix = "/api/v1/system/engines/1/access_grants/"
	path := prefix + id.String() + "/revoke"
	unused := grantServiceFunc(func(context.Context, engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error) {
		t.Fatal("denied or invalid request reached service")
		return nil, nil
	})
	unauthenticated := httptest.NewRecorder()
	grantTestRouter(t, nil, unused).ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, path, nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("missing credential=%d %s", unauthenticated.Code, unauthenticated.Body.String())
	}
	for _, projection := range []shared.AuthContext{
		testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-catalog"),
	} {
		router := grantTestRouter(t, &projection, unused)
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{"reason": "withdraw"}, 403)
	}
	projection := testIAMActorContext("tenant")
	grantPermission := func(permission string) {
		projection.Authorization.RoleAssignments = []shared.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.revoker", SourceType: "manual",
			ValidFrom: time.Now().Add(-time.Minute), Scope: shared.AssignmentScope{Type: "tenant", TenantID: projection.Context.TenantID}, Permissions: []string{permission}}}
	}
	// Delegation revoke and new handling are not Grant revocation Permission.
	for _, permission := range []string{"system.engine_access_delegation.revoke", "system.engine_access_fulfillment.create"} {
		grantPermission(permission)
		engineDelegationTestRequest(t, grantTestRouter(t, &projection, unused), "POST", path, nil, 403)
	}
	grantPermission("system.engine_access_grant.revoke")
	router := grantTestRouter(t, &projection, unused)
	for _, body := range []any{nil, map[string]any{"reason": " "}, map[string]any{"reason": "withdraw", "tenant_id": "2"}, map[string]any{"reason": "withdraw", "operator": "2"}} {
		engineDelegationTestRequest(t, router, "POST", path, body, 400)
	}
	engineDelegationTestRequest(t, router, "POST", path+"?tenant_id=2", map[string]any{"reason": "withdraw"}, 400)
	engineDelegationTestRequest(t, router, "POST", prefix+"bad/revoke", map[string]any{"reason": "withdraw"}, 400)
	engineDelegationTestRequest(t, router, "POST", prefix+uuid.Nil.String()+"/revoke", map[string]any{"reason": "withdraw"}, 400)
	for _, operation := range []struct{ method, suffix string }{{"GET", ""}, {"DELETE", ""}, {"PUT", ""}, {"POST", "/restore"}} {
		engineDelegationTestRequest(t, router, operation.method, prefix+id.String()+operation.suffix, nil, 404)
	}
	result := &engineaccess.GrantRevocation{RequestID: id, RevokedByPrincipalID: 9007199254740993,
		RevokedByMembershipID: 9007199254740995, RevokedAt: time.Now().UTC(), Reason: "withdraw"}
	qualified := grantServiceFunc(func(_ context.Context, input engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error) {
		if input.EngineID != 1 || input.RequestID != id || input.Actor.PrincipalID <= 0 || input.Actor.MembershipID <= 0 || input.Actor.TenantID <= 0 {
			t.Fatalf("lost authenticated provenance: %+v", input)
		}
		return result, nil
	})
	response := engineDelegationTestRequest(t, grantTestRouter(t, &projection, qualified), "POST", path, map[string]any{"reason": "withdraw"}, 200)
	var fields map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil || fields["revoked_by_principal_id"] != "9007199254740993" || fields["revoked_by_membership_id"] != "9007199254740995" {
		t.Fatalf("IAM IDs lost precision: %s %v", response.Body.String(), err)
	}
	conflict := grantServiceFunc(func(context.Context, engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error) {
		return nil, engineaccess.ErrGrantRevocationConflict
	})
	engineDelegationTestRequest(t, grantTestRouter(t, &projection, conflict), "POST", path, map[string]any{"reason": "different"}, 409)
	expired := grantServiceFunc(func(context.Context, engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error) {
		return nil, engineaccess.ErrGrantRevocationExpired
	})
	for language, message := range map[string]string{
		"zh-CN": "授权已到期，无须撤销",
		"en":    "This Grant has expired; revocation is unnecessary",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"reason":"withdraw"}`))
		req.Header.Set("Authorization", "Bearer addp_at_fixture")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", language)
		response := httptest.NewRecorder()
		grantTestRouter(t, &projection, expired).ServeHTTP(response, req)
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 409 ||
			body["error_code"] != "engine_access_grant_expired" || body["error"] != message {
			t.Fatalf("expired response language=%s: status=%d body=%s err=%v", language, response.Code, response.Body.String(), err)
		}
	}
}
