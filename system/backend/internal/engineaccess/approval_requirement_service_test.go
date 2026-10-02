package engineaccess

import (
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
)

func TestApprovalRequirementPermissionUsesCurrentTenantScopeAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	tenantID := int64(7)
	permission := "system.engine_access_approval_requirement.initialize"
	valid := iam.RoleAssignmentPermissionProjection{TenantID: &tenantID, ScopeType: "tenant", PermissionKey: permission, ValidFrom: now}
	if !hasCurrentTenantPermission([]iam.RoleAssignmentPermissionProjection{valid}, tenantID, permission, now) {
		t.Fatal("valid qualification denied")
	}
	for name, mutate := range map[string]func(*iam.RoleAssignmentPermissionProjection){
		"other permission": func(r *iam.RoleAssignmentPermissionProjection) { r.PermissionKey = "catalog.entry.update" },
		"department":       func(r *iam.RoleAssignmentPermissionProjection) { r.ScopeType = "department" },
		"project":          func(r *iam.RoleAssignmentPermissionProjection) { r.ScopeType = "project_group" },
		"unbound tenant":   func(r *iam.RoleAssignmentPermissionProjection) { r.TenantID = nil },
		"other tenant":     func(r *iam.RoleAssignmentPermissionProjection) { other := tenantID + 1; r.TenantID = &other },
		"future":           func(r *iam.RoleAssignmentPermissionProjection) { r.ValidFrom = now.Add(time.Second) },
		"expired":          func(r *iam.RoleAssignmentPermissionProjection) { r.ValidUntil = &now },
	} {
		t.Run(name, func(t *testing.T) {
			row := valid
			mutate(&row)
			if hasCurrentTenantPermission([]iam.RoleAssignmentPermissionProjection{row}, tenantID, permission, now) {
				t.Fatal("invalid qualification allowed")
			}
		})
	}
	expired := valid
	expired.ValidUntil = &now
	if !hasCurrentTenantPermission([]iam.RoleAssignmentPermissionProjection{expired, valid}, tenantID, permission, now) {
		t.Fatal("expired row hid current qualification")
	}
}
