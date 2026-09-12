package api

import (
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
)

func TestMapIAMManagedTenantRoleAssignmentPreservesPrincipalIdentity(t *testing.T) {
	serviceName := "addp-model"
	grantReason := "runtime initialization"
	grantActorType := iam.PrincipalTypeUser
	grantActorName := "Tenant Administrator"
	grantActorIdentifier := "tenant-admin"
	grantActorStatus := iam.PrincipalStatusActive
	grantActorID := int64(8)
	createdAt := time.Date(2026, 8, 12, 0, 1, 0, 0, time.UTC)
	assignment := iam.ManagedTenantRoleAssignment{
		RoleAssignment: iam.RoleAssignment{
			ID:                   21,
			PrincipalID:          21,
			RoleID:               9,
			ScopeType:            "tenant",
			Status:               "active",
			ValidFrom:            time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
			SourceType:           "manual",
			CreatedByPrincipalID: &grantActorID,
			GrantReason:          &grantReason,
			CreatedAt:            createdAt,
		},
		MembershipID:           21,
		PrincipalType:          iam.PrincipalTypeServicePrincipal,
		DisplayName:            serviceName,
		ServicePrincipalName:   &serviceName,
		RoleKey:                "tenant.model_runtime",
		EffectiveState:         "effective",
		GrantedByPrincipalType: &grantActorType,
		GrantedByDisplayName:   &grantActorName,
		GrantedByIdentifier:    &grantActorIdentifier,
		GrantedByStatus:        &grantActorStatus,
	}

	response := mapIAMManagedTenantRoleAssignment(assignment)
	if response.PrincipalType != string(iam.PrincipalTypeServicePrincipal) {
		t.Fatalf("principal_type = %q, want %q", response.PrincipalType, iam.PrincipalTypeServicePrincipal)
	}
	if response.DisplayName != serviceName || response.ServicePrincipalName == nil || *response.ServicePrincipalName != serviceName {
		t.Fatalf("service principal identity = %#v", response)
	}
	if response.SourceType != "manual" || response.GrantReason == nil || *response.GrantReason != grantReason || response.EffectiveState != "effective" {
		t.Fatalf("assignment lifecycle = %#v", response)
	}
	if !response.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %s, want %s", response.CreatedAt, createdAt)
	}
	if response.GrantedBy == nil ||
		response.GrantedBy.PrincipalID != "8" ||
		response.GrantedBy.PrincipalType != string(grantActorType) ||
		response.GrantedBy.DisplayName == nil || *response.GrantedBy.DisplayName != grantActorName ||
		response.GrantedBy.Identifier == nil || *response.GrantedBy.Identifier != grantActorIdentifier ||
		response.GrantedBy.Status != string(grantActorStatus) {
		t.Fatalf("grant actor = %#v", response.GrantedBy)
	}
}
