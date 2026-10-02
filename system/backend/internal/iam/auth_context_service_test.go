package iam

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
)

func TestCredentialValidationCapturesOriginalTimeEvidence(t *testing.T) {
	now := time.Date(2026, time.October, 2, 12, 0, 0, 123456000, time.UTC)
	for _, test := range []struct {
		condition string
		modify    func(*SessionCredentialAuthSnapshot)
	}{
		{"credential_created_after_database", func(s *SessionCredentialAuthSnapshot) { s.CredentialCreatedAt = now.Add(time.Nanosecond) }},
		{"family_authenticated_after_database", func(s *SessionCredentialAuthSnapshot) { s.FamilyAuthenticatedAt = now.Add(time.Nanosecond) }},
		{"credential_exceeds_family", func(s *SessionCredentialAuthSnapshot) { s.CredentialExpiresAt = s.FamilyExpiresAt.Add(time.Nanosecond) }},
		{"step_up_exceeds_family", func(s *SessionCredentialAuthSnapshot) {
			expiry := s.FamilyExpiresAt.Add(time.Nanosecond)
			s.FamilyStepUpExpiresAt = &expiry
		}},
		{"platform_context_invalid", func(s *SessionCredentialAuthSnapshot) { s.FamilyAssuranceLevel = AssuranceLevelAAL1 }},
		{"tenant_membership_binding_invalid", func(s *SessionCredentialAuthSnapshot) { s.FamilyContextType = ContextTypeTenant }},
		{"context_type_invalid", func(s *SessionCredentialAuthSnapshot) { s.FamilyContextType = ContextType("invalid") }},
	} {
		t.Run(test.condition, func(t *testing.T) {
			snapshot := &SessionCredentialAuthSnapshot{
				CredentialCreatedAt: now.Add(-time.Minute), CredentialExpiresAt: now.Add(time.Minute),
				FamilyContextType: ContextTypePlatform, FamilyAuthenticatedAt: now.Add(-time.Minute),
				FamilyExpiresAt: now.Add(2 * time.Minute), FamilyAssuranceLevel: AssuranceLevelAAL2,
				PrincipalType: PrincipalTypeUser, PrincipalStatus: PrincipalStatusActive, DatabaseTime: now,
			}
			test.modify(snapshot)
			original := *snapshot
			err := validateSessionCredentialSnapshot(snapshot)
			var validation *CredentialValidationError
			if !errors.As(fmt.Errorf("wrapped: %w", err), &validation) || validation.Reason != CredentialInvalidContext || validation.diagnostic == nil {
				t.Fatalf("validation must preserve original evidence: %v", err)
			}
			// A later read or mutation must not replace the failed snapshot's times.
			snapshot.DatabaseTime = now.Add(time.Hour)
			snapshot.CredentialCreatedAt = now.Add(time.Hour)
			snapshot.FamilyAuthenticatedAt = now.Add(time.Hour)
			snapshot.CredentialExpiresAt = now.Add(time.Hour)
			snapshot.FamilyExpiresAt = now.Add(time.Hour)
			if snapshot.FamilyStepUpExpiresAt != nil {
				*snapshot.FamilyStepUpExpiresAt = now.Add(time.Hour)
			}
			d := validation.diagnostic
			if d.condition != test.condition || !d.databaseTime.Equal(original.DatabaseTime) ||
				!d.credentialCreatedAt.Equal(original.CredentialCreatedAt) || !d.familyAuthenticatedAt.Equal(original.FamilyAuthenticatedAt) ||
				!d.credentialExpiresAt.Equal(original.CredentialExpiresAt) || !d.familyExpiresAt.Equal(original.FamilyExpiresAt) {
				t.Fatalf("original evidence was replaced: %#v", d)
			}
			if test.condition == "step_up_exceeds_family" && !d.stepUpExpiresAt.Equal(now.Add(2*time.Minute+time.Nanosecond)) {
				t.Fatalf("step-up expiry aliases source snapshot: %v", d.stepUpExpiresAt)
			}
			body, marshalErr := json.Marshal(validation)
			if marshalErr != nil || string(body) != `{"Reason":"context_invalid"}` || validation.Error() != commonapi.ErrUnauthorized.Error() || !errors.Is(validation, commonapi.ErrUnauthorized) {
				t.Fatalf("diagnostics changed public error semantics: body=%s error=%v", body, marshalErr)
			}
		})
	}
}

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
