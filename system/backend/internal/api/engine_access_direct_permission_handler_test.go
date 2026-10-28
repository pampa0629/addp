package api

import (
	"context"
	"encoding/json"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"strings"
	"testing"
	"time"
)

type unusedDirectPermissionService struct{}

func (unusedDirectPermissionService) SetDirectPermission(context.Context, engineaccess.SetDirectPermissionInput) (*engineaccess.DirectPermissionView, error) {
	panic("denied request reached mutation")
}
func (unusedDirectPermissionService) GetDirectPermission(context.Context, engineaccess.Actor, int64, string) (*engineaccess.DirectPermissionView, error) {
	panic("denied request reached lookup")
}
func (unusedDirectPermissionService) ListDirectPermissions(context.Context, engineaccess.Actor, int64, int, int, engineaccess.DirectPermissionFilter) ([]engineaccess.DirectPermissionIdentityView, int64, error) {
	panic("denied request reached list")
}
func directPermissionTestRouter(t *testing.T, service engineAccessDirectPermissionService, projection *commonauth.AuthContext) *gin.Engine {
	t.Helper()
	if projection == nil {
		t.Fatal("AuthContext fixture required")
	}
	if err := commonauth.ValidateAuthContext(*projection); err != nil {
		t.Fatal(err)
	}
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
	if err := RegisterEngineAccessDirectPermissionRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: credential},
		&EngineAccessDirectPermissionHandler{service: service}); err != nil {
		t.Fatal(err)
	}
	return router
}

func directPermissionAuth(permissions ...string) commonauth.AuthContext {
	auth := testIAMActorContext("tenant")
	auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.engine_access_delegation_administrator", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: auth.Context.TenantID}, Permissions: permissions}}
	return auth
}
func TestDirectPermissionRoutesEnforceUserTenantAndSeparatePermissions(t *testing.T) {
	list := "/api/v1/system/engines/1/direct-permissions"
	item := "/api/v1/system/engines/1/data-items/" + strings.Repeat("a", 64) + "/direct-permission"
	for _, auth := range []commonauth.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-catalog")} {
		router := directPermissionTestRouter(t, unusedDirectPermissionService{}, &auth)
		engineDelegationTestRequest(t, router, "GET", list, nil, 403)
		engineDelegationTestRequest(t, router, "GET", item, nil, 403)
		engineDelegationTestRequest(t, router, "PUT", item, nil, 403)
	}
	auth := directPermissionAuth("system.engine_access_direct_permission.read")
	router := directPermissionTestRouter(t, unusedDirectPermissionService{}, &auth)
	engineDelegationTestRequest(t, router, "PUT", item, nil, 403)
	for _, suffix := range []string{"?tenant_id=2", "?page=0", "?page=01", "?search=a&search=b", "?unknown=x"} {
		engineDelegationTestRequest(t, router, "GET", list+suffix, nil, 400)
	}
	engineDelegationTestRequest(t, router, "GET", item+"?tenant_id=2", nil, 400)
	for _, method := range []string{"GET", "POST", "PUT"} {
		engineDelegationTestRequest(t, router, method, "/api/v1/system/engines/1/access_approval_requirements", nil, 404)
	}
	engineDelegationTestRequest(t, router, "POST", "/api/v1/system/engines/1/access_handling_requirement", nil, 404)
}

type successfulDirectPermissionFixture struct {
	unusedDirectPermissionService
	t       *testing.T
	allowed bool
}

func (f successfulDirectPermissionFixture) SetDirectPermission(_ context.Context, input engineaccess.SetDirectPermissionInput) (*engineaccess.DirectPermissionView, error) {
	if input.EngineID != 9007199254740993 || input.ExpectedVersion != 9007199254740993 || input.ExpectedSource != "explicit" || input.Allowed != f.allowed || input.Actor.PrincipalID <= 0 || int64(input.CatalogPath.EngineID) != input.EngineID {
		f.t.Fatalf("lost authority/precision: %+v", input)
	}
	state := "denied"
	if input.Allowed {
		state = "allowed"
	}
	return &engineaccess.DirectPermissionView{EngineID: input.EngineID, ItemFingerprint: input.ItemFingerprint, Source: "explicit", State: state, Version: input.ExpectedVersion + 1}, nil
}
func TestDirectPermissionWriteStrictBodyAndLosslessVersion(t *testing.T) {
	auth := directPermissionAuth("system.engine_access_direct_permission.update")
	target := engineplugin.TabularItemPath(9007199254740993, "schema", "public", "exact.table")
	binding := factstest.Binding(target)
	route := "/api/v1/system/engines/9007199254740993/data-items/" + binding.ItemFingerprint + "/direct-permission"
	body := func() map[string]any {
		return map[string]any{"catalog_path": target, "expected_boundary_hash": binding.BoundaryHash, "allowed": false, "expected_source": "explicit", "expected_version": "9007199254740993", "reason": "Explicit setting"}
	}
	router := directPermissionTestRouter(t, unusedDirectPermissionService{}, &auth)
	for _, field := range []string{"tenant_id", "principal_id", "engine_id", "mode", "initialize_approval", "requirement_version"} {
		b := body()
		b[field] = "1"
		engineDelegationTestRequest(t, router, "PUT", route, b, 400)
	}
	for _, version := range []any{nil, 1, 1.5, "-1", "01", "9223372036854775808"} {
		b := body()
		b["expected_version"] = version
		engineDelegationTestRequest(t, router, "PUT", route, b, 400)
	}
	b := body()
	delete(b, "allowed")
	engineDelegationTestRequest(t, router, "PUT", route, b, 400)
	engineDelegationTestRequest(t, router, "PUT", route+"?anything=1", body(), 400)
	for _, allowed := range []bool{false, true} {
		b := body()
		b["allowed"] = allowed
		router = directPermissionTestRouter(t, successfulDirectPermissionFixture{t: t, allowed: allowed}, &auth)
		response := engineDelegationTestRequest(t, router, "PUT", route, b, 200)
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["version"] != "9007199254740994" {
			t.Fatal(response.Body.String(), err)
		}
	}
}
