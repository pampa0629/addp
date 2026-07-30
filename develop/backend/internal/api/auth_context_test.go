package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	commonAuthorization "github.com/addp/common/authorization"
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/gin-gonic/gin"
)

func setTenantAuthContextForTest(c *gin.Context, tenantID, userID uint) {
	setTenantAuthContextWithPermissionsForTest(c, tenantID, userID, nil)
}

func setTenantAuthContextWithPermissionsForTest(c *gin.Context, tenantID, userID uint, permissions []string) {
	tenantIDText := strconv.FormatUint(uint64(tenantID), 10)
	userIDText := strconv.FormatUint(uint64(userID), 10)
	membershipID := "1"
	clientID := "addp-web"
	issuedAt := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	authContext := commonAuthorization.AuthContext{
		SchemaVersion: commonAuthorization.AuthContextSchemaVersion,
		Principal:     commonAuthorization.AuthPrincipal{Type: "user", ID: userIDText},
		Context: commonAuthorization.AuthSessionContext{
			Type: "tenant", TenantID: &tenantIDText, TenantMembershipID: &membershipID,
		},
		Authentication: commonAuthorization.AuthenticationFacts{
			Methods: []string{"password"}, AssuranceLevel: "aal1", AuthenticatedAt: issuedAt,
		},
		Client: commonAuthorization.ClientConstraints{
			ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{},
		},
		Organization: commonAuthorization.OrganizationContext{
			Departments: []commonAuthorization.DepartmentMembership{}, ProjectGroups: []commonAuthorization.ProjectGroupMembership{},
		},
		Authorization: commonAuthorization.AuthorizationFacts{AuthorizationVersion: "1"},
		Token: commonAuthorization.TokenFacts{
			Type: "first_party_access_token", IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
		},
	}
	if len(permissions) > 0 {
		authContext.Authorization.RoleAssignments = []commonAuthorization.RoleAssignment{{
			AssignmentID: "1", RoleKey: "tenant.test_role",
			Scope:       commonAuthorization.AssignmentScope{Type: "tenant", TenantID: &tenantIDText},
			Permissions: append([]string(nil), permissions...), SourceType: "manual", ValidFrom: issuedAt,
		}}
	} else {
		authContext.Authorization.RoleAssignments = []commonAuthorization.RoleAssignment{}
	}
	if err := commonAuth.SetAuthContextForGin(c, authContext); err != nil {
		panic(err)
	}
}

func TestRequireNotebookExecutionPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		taskType    string
		permissions []string
		wantAllowed bool
		wantStatus  int
	}{
		{name: "query does not require notebook permission", taskType: commonExecution.TaskTypeQuery, wantAllowed: true, wantStatus: http.StatusOK},
		{name: "script rejects missing notebook permission", taskType: commonExecution.TaskTypeScript, wantStatus: http.StatusForbidden},
		{name: "script accepts notebook permission", taskType: commonExecution.TaskTypeScript, permissions: []string{developauthorization.PermissionDevelopNotebookExecute}, wantAllowed: true, wantStatus: http.StatusOK},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			setTenantAuthContextWithPermissionsForTest(context, 7, 1, testCase.permissions)
			allowed := requireNotebookExecutionPermission(context, testCase.taskType)
			if allowed != testCase.wantAllowed || response.Code != testCase.wantStatus {
				t.Fatalf("allowed = %v, status = %d, body = %s", allowed, response.Code, response.Body.String())
			}
		})
	}
}
