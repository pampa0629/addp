package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type unusedApprovalRequirementService struct{}

func (unusedApprovalRequirementService) GetHandlingRequirement(context.Context, engineaccess.Actor, engineplugin.EngineCatalogPath) (*engineaccess.HandlingRequirementView, error) {
	panic("denied request reached handling observation")
}

func (unusedApprovalRequirementService) InitializeApprovalRequirement(context.Context, engineaccess.InitializeApprovalRequirementInput) (*engineaccess.ApprovalRequirementView, error) {
	panic("denied request reached service")
}
func (unusedApprovalRequirementService) ListApprovalRequirements(context.Context, engineaccess.Actor, int64, int, int) ([]engineaccess.ApprovalRequirementView, int64, error) {
	panic("denied request reached service")
}
func (unusedApprovalRequirementService) GetApprovalRequirement(context.Context, engineaccess.Actor, int64, uuid.UUID) (*engineaccess.ApprovalRequirementView, error) {
	panic("denied request reached service")
}

func approvalRequirementTestRouter(t *testing.T, service engineAccessApprovalRequirementService, projection *commonauth.AuthContext) *gin.Engine {
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
	if err := RegisterEngineAccessApprovalRequirementRoutes(router.Group("/api/v1/system"), &IAMRuntime{Authentication: authentication, UserAccessCredential: credential},
		&EngineAccessApprovalRequirementHandler{service: service}); err != nil {
		t.Fatal(err)
	}
	return router
}

func TestApprovalRequirementRoutesRejectUnqualifiedContextsAndBodyAuthority(t *testing.T) {
	const path = "/api/v1/system/engines/1/access_approval_requirements"
	id := uuid.NewString()
	permissions := []string{"system.engine_access_approval_requirement.initialize", "system.engine_access_approval_requirement.read"}
	for _, tc := range []struct {
		name string
		auth commonauth.AuthContext
	}{
		{"no permission", testIAMActorContext("tenant")},
		{"platform", testIAMActorContext("platform")},
		{"machine", testIAMServiceActorContext("tenant", "addp-catalog")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := approvalRequirementTestRouter(t, unusedApprovalRequirementService{}, &tc.auth)
			engineDelegationTestRequest(t, router, "GET", path, nil, 403)
			engineDelegationTestRequest(t, router, "GET", path+"/"+id, nil, 403)
			engineDelegationTestRequest(t, router, "POST", path, nil, 403)
		})
	}
	auth := testIAMActorContext("tenant")
	auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.governance", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute),
		Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: auth.Context.TenantID}, Permissions: permissions}}
	router := approvalRequirementTestRouter(t, unusedApprovalRequirementService{}, &auth)
	for _, field := range []string{"tenant_id", "principal_id", "version", "successor_principal_id"} {
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{field: "1"}, 400)
	}
	for _, mode := range []any{nil, "", "Catalog", "unknown", 1} {
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{"mode": mode, "reason": "Explicit configuration"}, 400)
	}
	engineDelegationTestRequest(t, router, "POST", path, map[string]any{"reason": "Missing mode"}, 400)
	engineDelegationTestRequest(t, router, "GET", path+"?tenant_id=2", nil, 400)
	engineDelegationTestRequest(t, router, "POST", path+"?tenant_id=2", nil, 400)
	engineDelegationTestRequest(t, router, "GET", path+"/invalid", nil, 400)
	for _, op := range []struct{ method, suffix string }{{"PUT", "/" + id}, {"DELETE", "/" + id}, {"POST", "/" + id + "/exit"}, {"POST", "/" + id + "/restore"}} {
		engineDelegationTestRequest(t, router, op.method, path+op.suffix, nil, 404)
	}
	auth.Authorization.RoleAssignments[0].Scope.Type = "department"
	departmentID := "5"
	auth.Authorization.RoleAssignments[0].Scope.DepartmentID = &departmentID
	if err := commonauth.ValidateAuthContext(auth); err != nil {
		t.Fatal(err)
	}
	router = approvalRequirementTestRouter(t, unusedApprovalRequirementService{}, &auth)
	engineDelegationTestRequest(t, router, "POST", path, nil, 403)
	engineDelegationTestRequest(t, router, "GET", path, nil, 403)
}

type initializationRequirementFixture struct {
	unusedApprovalRequirementService
	t    *testing.T
	mode string
}

func (f initializationRequirementFixture) InitializeApprovalRequirement(_ context.Context, input engineaccess.InitializeApprovalRequirementInput) (*engineaccess.ApprovalRequirementView, error) {
	if input.Mode != f.mode || input.EngineID != 9007199254740993 || int64(input.CatalogPath.EngineID) != input.EngineID || input.Actor.PrincipalID <= 0 {
		f.t.Fatalf("initialization lost explicit mode, authenticated actor or target: %+v", input)
	}
	return &engineaccess.ApprovalRequirementView{ID: uuid.New(), Mode: input.Mode, Version: 1, EngineID: input.EngineID}, nil
}

func TestApprovalRequirementInitializationPreservesExplicitModeAndExactTarget(t *testing.T) {
	auth := testIAMActorContext("tenant")
	auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.governance", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute),
		Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: auth.Context.TenantID}, Permissions: []string{"system.engine_access_approval_requirement.initialize"}}}
	target := engineplugin.TabularItemPath(9007199254740993, "schema", "public", "exact.table")
	for _, mode := range []string{"catalog", "independent"} {
		t.Run(mode, func(t *testing.T) {
			router := approvalRequirementTestRouter(t, initializationRequirementFixture{t: t, mode: mode}, &auth)
			response := engineDelegationTestRequest(t, router, "POST", "/api/v1/system/engines/9007199254740993/access_approval_requirements",
				InitializeEngineAccessApprovalRequirementRequest{CatalogPath: target, Mode: mode, Reason: "Explicit configuration"}, 201)
			var result engineaccess.ApprovalRequirementView
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Mode != mode || result.Version != 1 || result.EngineID != int64(target.EngineID) {
				t.Fatalf("configuration response=%+v err=%v", result, err)
			}
		})
	}
}

func TestHandlingRequirementRejectsUnqualifiedContextsAndInvalidTargets(t *testing.T) {
	const route = "/api/v1/system/engines/12/access_handling_requirement"
	path := engineplugin.TabularItemPath(12, "schema", "public", "exact.table")
	body := EngineAccessHandlingRequirementRequest{Version: path.Version, Segments: path.Segments}
	for _, auth := range []commonauth.AuthContext{testIAMActorContext("tenant"), testIAMActorContext("platform"), testIAMServiceActorContext("tenant", "addp-catalog")} {
		router := approvalRequirementTestRouter(t, unusedApprovalRequirementService{}, &auth)
		engineDelegationTestRequest(t, router, "POST", route, body, 403)
	}
	auth := testIAMActorContext("tenant")
	auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.handler", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: auth.Context.TenantID}, Permissions: []string{"system.engine_access_fulfillment.create"}}}
	router := approvalRequirementTestRouter(t, unusedApprovalRequirementService{}, &auth)
	for _, field := range []string{"engine_id", "tenant_id", "principal_id", "mode", "requirement_version"} {
		engineDelegationTestRequest(t, router, "POST", route, map[string]any{"version": path.Version, "segments": path.Segments, field: "1"}, 400)
	}
	for _, input := range []any{nil, map[string]any{}, map[string]any{"version": "wrong", "segments": path.Segments}, map[string]any{"version": path.Version, "segments": path.Segments[:1]}} {
		engineDelegationTestRequest(t, router, "POST", route, input, 400)
	}
	engineDelegationTestRequest(t, router, "POST", route+"?anything=1", body, 400)
	engineDelegationTestRequest(t, router, "GET", route, nil, 404)
}

type handlingRequirementFixture struct {
	unusedApprovalRequirementService
	t *testing.T
}

func (f handlingRequirementFixture) GetHandlingRequirement(_ context.Context, _ engineaccess.Actor, path engineplugin.EngineCatalogPath) (*engineaccess.HandlingRequirementView, error) {
	if uint64(path.EngineID) != 9007199254740993 || path.Segments[len(path.Segments)-1].Name != "exact.table" {
		f.t.Fatalf("lossy target: %+v", path)
	}
	return &engineaccess.HandlingRequirementView{Mode: "catalog", RequirementVersion: 9007199254740993}, nil
}

func TestHandlingRequirementPreservesExactDecimalPrecision(t *testing.T) {
	auth := testIAMActorContext("tenant")
	auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: "custom.handler", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: auth.Context.TenantID}, Permissions: []string{"system.engine_access_fulfillment.create"}}}
	router := approvalRequirementTestRouter(t, handlingRequirementFixture{t: t}, &auth)
	path := engineplugin.TabularItemPath(1, "schema", "public", "exact.table")
	response := engineDelegationTestRequest(t, router, "POST", "/api/v1/system/engines/9007199254740993/access_handling_requirement", EngineAccessHandlingRequirementRequest{Version: path.Version, Segments: path.Segments}, 200)
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result) != 2 || result["requirement_version"] != "9007199254740993" {
		t.Fatalf("minimal precision result=%v err=%v", result, err)
	}
}
