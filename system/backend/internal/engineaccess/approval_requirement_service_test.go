package engineaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
)

func TestApprovalRequirementInitializationRejectsMissingOrUnknownModeBeforeStorage(t *testing.T) {
	service := &Service{}
	for _, mode := range []string{"", "Catalog", "unknown", " independent "} {
		t.Run(mode, func(t *testing.T) {
			_, err := service.InitializeApprovalRequirement(context.Background(), InitializeApprovalRequirementInput{
				EngineID: 1, CatalogPath: engineplugin.TabularItemPath(1, "schema", "public", "table"), Mode: mode, Reason: "Explicit configuration",
			})
			if !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("invalid mode %q reached storage: %v", mode, err)
			}
		})
	}
}

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

func TestEngineManagementPermissionsDoNotImplyOtherActions(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	tenantID := int64(7)
	permissions := []string{
		authorization.PermissionSystemEngineAccessApprovalRequirementInitialize,
		authorization.PermissionSystemEngineAccessApprovalRequirementRead,
		authorization.PermissionSystemEngineAccessGrantRevoke,
		authorization.PermissionSystemEngineAccessDenyCreate,
		authorization.PermissionSystemEngineAccessDenyRelease,
	}
	for _, assigned := range permissions {
		t.Run(assigned, func(t *testing.T) {
			rows := []iam.RoleAssignmentPermissionProjection{{
				TenantID: &tenantID, ScopeType: "tenant", PermissionKey: assigned, ValidFrom: now,
			}}
			for _, requested := range permissions {
				if got := hasCurrentTenantPermission(rows, tenantID, requested, now); got != (assigned == requested) {
					t.Fatalf("assigned %q, requested %q: qualification=%v", assigned, requested, got)
				}
			}
		})
	}
}

func TestEngineManagementPermissionRechecksAssignmentTime(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	tenantID := int64(7)
	permission := authorization.PermissionSystemEngineAccessGrantRevoke
	deadline := now.Add(time.Minute)
	rows := []iam.RoleAssignmentPermissionProjection{{
		TenantID: &tenantID, ScopeType: "tenant", PermissionKey: permission,
		ValidFrom: now, ValidUntil: &deadline,
	}}
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before start", now.Add(-time.Nanosecond), false},
		{"at start", now, true},
		{"before deadline", deadline.Add(-time.Nanosecond), true},
		{"at deadline", deadline, false},
		{"after deadline", deadline.Add(time.Nanosecond), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCurrentTenantPermission(rows, tenantID, permission, tc.at); got != tc.want {
				t.Fatalf("qualification=%v, want %v at %s", got, tc.want, tc.at)
			}
		})
	}
}
