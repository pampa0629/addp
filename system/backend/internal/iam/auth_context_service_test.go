package iam

import (
	"testing"
	"time"
)

func TestBuildRoleAssignmentsUsesAuthContextCanonicalOrder(t *testing.T) {
	validFrom := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	tenantID := int64(1)
	rows := []RoleAssignmentPermissionProjection{
		{
			AssignmentID: 2, RoleKey: "tenant.role_assignment", ScopeType: "tenant", TenantID: &tenantID,
			SourceType: "manual", ValidFrom: validFrom, PermissionKey: "iam.tenant_role_assignment.read",
		},
		{
			AssignmentID: 1, RoleKey: "tenant.role", ScopeType: "tenant", TenantID: &tenantID,
			SourceType: "manual", ValidFrom: validFrom, PermissionKey: "iam.tenant_role.update",
		},
		{
			AssignmentID: 1, RoleKey: "tenant.role", ScopeType: "tenant", TenantID: &tenantID,
			SourceType: "manual", ValidFrom: validFrom, PermissionKey: "iam.tenant_role.create",
		},
	}

	assignments, err := buildRoleAssignments(rows)
	if err != nil {
		t.Fatalf("build role assignments: %v", err)
	}
	if len(assignments) != 2 || assignments[0].RoleKey != "tenant.role" || assignments[1].RoleKey != "tenant.role_assignment" {
		t.Fatalf("assignment order = %#v", assignments)
	}
	permissions := assignments[0].Permissions
	if len(permissions) != 2 || permissions[0] != "iam.tenant_role.create" || permissions[1] != "iam.tenant_role.update" {
		t.Fatalf("permission order = %#v", permissions)
	}
}
