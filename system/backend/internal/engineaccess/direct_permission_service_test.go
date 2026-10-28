package engineaccess

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
)

func TestDirectPermissionQueryCapacityDoesNotBecomeBadRequestOrEmptySuccess(t *testing.T) {
	observation := &directPermissionObservation{
		Settings:   make(map[string]directPermissionSetting),
		Deployment: &catalogDeploymentDeclaration{Enabled: true, Version: 1},
	}
	for i := 0; i <= authorizationfacts.MaxIdentityQueryFingerprints; i++ {
		observation.Settings[fmt.Sprintf("%064x", i)] = directPermissionSetting{Allowed: true, Version: 1}
	}
	if _, err := prepareDirectPermissionIdentityQuery(2, 1, 20, DirectPermissionFilter{State: "allowed"}, observation); !errors.Is(err, ErrAuthorizationFactsUnavailable) || errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("complete oversized filter must fail as unavailable: %v", err)
	}
	if _, err := prepareDirectPermissionIdentityQuery(2, 1, 20, DirectPermissionFilter{State: "invalid"}, observation); !errors.Is(err, commonapi.ErrBadRequest) || errors.Is(err, ErrAuthorizationFactsUnavailable) {
		t.Fatalf("invalid filter must remain bad request: %v", err)
	}
	fingerprint := fmt.Sprintf("%064x", authorizationfacts.MaxIdentityQueryFingerprints)
	query, err := prepareDirectPermissionIdentityQuery(2, 1, 1, DirectPermissionFilter{ItemFingerprint: fingerprint}, observation)
	if err != nil || query.Validate() != nil || query.FingerprintFilter != "include" || len(query.Fingerprints) != 1 || query.Fingerprints[0] != fingerprint {
		t.Fatalf("single-item query must remain complete: %+v %v", query, err)
	}
}

func TestDirectPermissionRejectsInvalidCommandBeforeStorage(t *testing.T) {
	service := &Service{}
	path := engineplugin.TabularItemPath(1, "schema", "public", "table")
	binding := factstest.Binding(path)
	valid := SetDirectPermissionInput{EngineID: 1, CatalogPath: path, ItemFingerprint: binding.ItemFingerprint, ExpectedBoundaryHash: binding.BoundaryHash, ExpectedSource: "explicit", ExpectedVersion: 1, Reason: "Explicit change"}
	for name, mutate := range map[string]func(*SetDirectPermissionInput){
		"zero explicit version": func(i *SetDirectPermissionInput) { i.ExpectedVersion = 0 },
		"negative version":      func(i *SetDirectPermissionInput) { i.ExpectedVersion = -1 },
		"missing engine":        func(i *SetDirectPermissionInput) { i.EngineID = 0 },
		"missing fingerprint":   func(i *SetDirectPermissionInput) { i.ItemFingerprint = "" },
		"missing boundary":      func(i *SetDirectPermissionInput) { i.ExpectedBoundaryHash = "" },
		"unknown source":        func(i *SetDirectPermissionInput) { i.ExpectedSource = "independent" },
		"missing reason":        func(i *SetDirectPermissionInput) { i.Reason = " " },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := service.SetDirectPermission(context.Background(), input); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatal(err)
			}
		})
	}
}

func TestApprovalRequirementPermissionUsesCurrentTenantScopeAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	tenantID := int64(7)
	permission := "system.engine_access_direct_permission.update"
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
		authorization.PermissionSystemEngineAccessDirectPermissionUpdate,
		authorization.PermissionSystemEngineAccessGrantCreate,
		authorization.PermissionSystemEngineAccessDirectPermissionRead,
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
