package authorization

import "testing"

func TestHasContextPermissionsIgnoresOrganizationalAssignment(t *testing.T) {
	context := validTenantAuthContext()
	tenantID := *context.Context.TenantID
	departmentID := "9"
	context.Authorization.RoleAssignments = []RoleAssignment{
		{Scope: AssignmentScope{Type: "department", TenantID: &tenantID, DepartmentID: &departmentID}, Permissions: []string{"transfer.task.read"}},
		{Scope: AssignmentScope{Type: "tenant", TenantID: &tenantID}, Permissions: []string{"transfer.task.create"}},
	}
	if HasContextPermissions(context, "transfer.task.read") ||
		!HasContextPermissions(context, "transfer.task.create") ||
		HasContextPermissions(context, "transfer.task.create", "transfer.task.read") {
		t.Fatal("tenant permissions were widened by an organizational assignment")
	}
}

func TestHasContextPermissionsRequiresMatchingTenant(t *testing.T) {
	context := validTenantAuthContext()
	otherTenantID := "4"
	context.Authorization.RoleAssignments = []RoleAssignment{{
		Scope: AssignmentScope{Type: "tenant", TenantID: &otherTenantID}, Permissions: []string{"transfer.task.read"},
	}}
	if HasContextPermissions(context, "transfer.task.read") {
		t.Fatal("another tenant assignment must not grant access")
	}
}
