package api

import (
	"context"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type unusedApprovalRequirementService struct{}

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
	for _, field := range []string{"tenant_id", "principal_id", "mode", "version", "successor_principal_id"} {
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{field: "1"}, 400)
	}
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
