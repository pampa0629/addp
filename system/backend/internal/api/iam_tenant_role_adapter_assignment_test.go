package api

import (
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
)

func TestMapIAMManagedTenantRoleAssignmentPreservesPrincipalIdentity(t *testing.T) {
	serviceName := "addp-model"
	assignment := iam.ManagedTenantRoleAssignment{
		RoleAssignment: iam.RoleAssignment{
			ID:          21,
			PrincipalID: 21,
			RoleID:      9,
			ScopeType:   "tenant",
			Status:      "active",
			ValidFrom:   time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
		},
		MembershipID:         21,
		PrincipalType:        iam.PrincipalTypeServicePrincipal,
		DisplayName:          serviceName,
		ServicePrincipalName: &serviceName,
		RoleKey:              "tenant.model_runtime",
	}

	response := mapIAMManagedTenantRoleAssignment(assignment)
	if response.PrincipalType != string(iam.PrincipalTypeServicePrincipal) {
		t.Fatalf("principal_type = %q, want %q", response.PrincipalType, iam.PrincipalTypeServicePrincipal)
	}
	if response.DisplayName != serviceName || response.ServicePrincipalName == nil || *response.ServicePrincipalName != serviceName {
		t.Fatalf("service principal identity = %#v", response)
	}
}
