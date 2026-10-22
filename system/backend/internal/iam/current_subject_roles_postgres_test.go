package iam

import (
	"context"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"
)

// Part of the existing real PostgreSQL execution-subject-roles gate. Every
// mutation is confined to its rollback transaction; no custom database exists.
func testCurrentSubjectRoles(t *testing.T, ctx context.Context, db *gorm.DB, token string) {
	t.Helper()
	t.Run("own_current_subject_roles", func(t *testing.T) {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		repo := NewRepository(tx)
		svc, _ := NewAuthContextService(repo)
		initial, err := svc.ResolveCurrentSubjectRoles(ctx, token)
		if err != nil || initial == nil {
			t.Fatalf("own roles: %v", err)
		}
		principalID, tenantID := mustSelfRoleID(t, initial.ActorPrincipalID), mustSelfRoleID(t, initial.TenantID)
		if err := tx.Exec(`INSERT INTO system.roles (tenant_id,role_key,name,role_type,allowed_scope_types,allowed_principal_types,immutable,created_by_principal_id) VALUES (?,'custom.self_role_probe','Self role probe','tenant_custom',ARRAY['tenant'],ARRAY['user'],false,?)`, tenantID, principalID).Error; err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(1500 * time.Millisecond)
		insertRoleAssignment(t, tx, principalID, "custom.self_role_probe", "tenant", &tenantID, nil, nil, time.Now().Add(-time.Minute), &deadline, "manual")
		if _, err := svc.ResolveCurrentSubjectRoles(ctx, token); err == nil {
			t.Fatal("old token survived changed authorization version")
		}
		tokens, err := NewTokenFamilyService(repo, BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		selectionService, err := NewContextSelectionService(repo, tokens)
		if err != nil {
			t.Fatal(err)
		}
		selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{PrincipalID: principalID, Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: time.Now()}})
		if err != nil || selection.Session == nil {
			t.Fatalf("fresh session: %v", err)
		}
		fresh := selection.Session.AccessToken
		roles, err := svc.ResolveCurrentSubjectRoles(ctx, fresh)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, key := range roles.RoleKeys {
			if key == "custom.self_role_probe" {
				found = true
			}
		}
		if !found || roles.ExpiresAt.After(deadline.Add(time.Millisecond)) || roles.ActorPrincipalID != initial.ActorPrincipalID || roles.TenantID != initial.TenantID {
			t.Fatalf("missing no-permission role or incorrect binding: %+v", roles)
		}
		current, err := svc.ResolveAuthContext(ctx, fresh)
		if err != nil {
			t.Fatal(err)
		}
		for _, assignment := range current.Authorization.RoleAssignments {
			if assignment.RoleKey == "custom.self_role_probe" {
				t.Fatal("functional AuthContext contract was broadened")
			}
		}
		if err := roles.ValidateFor(current, time.Now()); err != nil {
			t.Fatal(err)
		}
		for time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		after, err := svc.ResolveCurrentSubjectRoles(ctx, fresh)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range after.RoleKeys {
			if key == "custom.self_role_probe" {
				t.Fatal("naturally expired role returned")
			}
		}
		if _, err := svc.ResolveCurrentSubjectRoles(ctx, "addp_dat_fixture"); err == nil {
			t.Fatal("delegated credential accepted")
		}
		if _, err := svc.ResolveCurrentSubjectRoles(ctx, "addp_rat_fixture"); err == nil {
			t.Fatal("resource ticket accepted")
		}
		logout, err := NewLogoutService(repo, tokens)
		if err != nil {
			t.Fatal(err)
		}
		if err := logout.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{AccessToken: fresh, RefreshToken: selection.Session.RefreshToken}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ResolveCurrentSubjectRoles(ctx, fresh); err == nil {
			t.Fatal("revoked session accepted")
		}
	})
}

func mustSelfRoleID(t *testing.T, value string) int64 {
	t.Helper()
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
