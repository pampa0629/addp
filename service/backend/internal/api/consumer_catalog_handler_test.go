package api

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestTenantQueryExecutionPermissionFailsClosedForUnboundScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		scopeType      string
		resourceTenant uint
		want           bool
	}{
		{name: "tenant assignment", scopeType: "tenant", resourceTenant: 7, want: true},
		{name: "department assignment", scopeType: "department", resourceTenant: 7, want: false},
		{name: "project group assignment", scopeType: "project_group", resourceTenant: 7, want: false},
		{name: "other tenant resource", scopeType: "tenant", resourceTenant: 8, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			authContext := protocolAuthContext(7)
			tenantID := strconv.FormatUint(7, 10)
			scope := commonauth.AssignmentScope{Type: tt.scopeType, TenantID: &tenantID}
			departmentID, projectGroupID := "10", "20"
			if tt.scopeType == "department" {
				scope.DepartmentID = &departmentID
			}
			if tt.scopeType == "project_group" {
				scope.ProjectGroupID = &projectGroupID
			}
			authContext.Authorization.RoleAssignments = []commonauth.RoleAssignment{{
				AssignmentID: "1", RoleKey: "tenant.service_consumer", Scope: scope,
				Permissions: []string{"service.data_read.execute"}, SourceType: "manual",
				ValidFrom: time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC),
			}}
			if err := authmiddleware.SetAuthContextForGin(context, authContext); err != nil {
				t.Fatal(err)
			}
			if got := hasTenantQueryExecutionPermission(context, tt.resourceTenant); got != tt.want {
				t.Fatalf("permission = %v, want %v", got, tt.want)
			}
		})
	}
}
