package iam

import (
	"errors"
	"testing"
	"time"
)

func TestValidateSessionCredentialSnapshotPlatformAssuranceByPrincipalType(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		principalType PrincipalType
		assurance     AssuranceLevel
		wantValid     bool
	}{
		{name: "user aal2", principalType: PrincipalTypeUser, assurance: AssuranceLevelAAL2, wantValid: true},
		{name: "user aal3", principalType: PrincipalTypeUser, assurance: AssuranceLevelAAL3, wantValid: true},
		{name: "user not applicable", principalType: PrincipalTypeUser, assurance: AssuranceLevelNotApplicable},
		{name: "service principal not applicable", principalType: PrincipalTypeServicePrincipal, assurance: AssuranceLevelNotApplicable, wantValid: true},
		{name: "service principal aal2", principalType: PrincipalTypeServicePrincipal, assurance: AssuranceLevelAAL2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := &SessionCredentialAuthSnapshot{
				CredentialExpiresAt:           now.Add(time.Minute),
				CredentialCreatedAt:           now.Add(-time.Minute),
				FamilyContextType:             ContextTypePlatform,
				FamilyAuthorizationVersion:    3,
				FamilyAssuranceLevel:          test.assurance,
				FamilyAuthenticatedAt:         now.Add(-time.Minute),
				FamilyExpiresAt:               now.Add(time.Minute),
				PrincipalType:                 test.principalType,
				PrincipalStatus:               PrincipalStatusActive,
				PrincipalAuthorizationVersion: 3,
				DatabaseTime:                  now,
			}

			err := validateSessionCredentialSnapshot(snapshot)
			if test.wantValid && err != nil {
				t.Fatalf("validate platform snapshot: %v", err)
			}
			var validationError *CredentialValidationError
			if !test.wantValid && (!errors.As(err, &validationError) || validationError.Reason != CredentialInvalidContext) {
				t.Fatalf("validate platform snapshot error = %v, want %s", err, CredentialInvalidContext)
			}
		})
	}
}

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
