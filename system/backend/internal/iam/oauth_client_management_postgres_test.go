package iam

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOAuthClientManagementServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset OAuth client management test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	identityService := NewIdentityService(repository, func() time.Time { return now })
	membershipService := NewTenantMembershipService(repository, func() time.Time { return now })
	service := NewOAuthClientManagementService(repository)
	bootstrapAudit := AuditMetadata{RequestID: stringPointer("oauth-client-bootstrap")}
	user := createContextSelectionUser(t, ctx, identityService, "oauth-client-admin", bootstrapAudit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "oauth-client", bootstrapAudit)
	otherTenant := createContextSelectionTenant(t, ctx, membershipService, "oauth-client-other", bootstrapAudit)
	contextType := ContextTypeTenant
	principalType := PrincipalTypeUser
	audit := AuditMetadata{
		PrincipalID: &user.PrincipalID, PrincipalType: &principalType,
		ContextType: &contextType, TenantID: &tenant.ID,
		RequestID: stringPointer("oauth-client-management"),
	}
	membership := establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, audit)

	created, err := service.Create(ctx, CreateManagedOAuthClientInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID,
		DisplayName: "Research BI", RedirectURIs: []string{"https://bi.example.com/oauth/callback"}, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create managed OAuth client: %v", err)
	}
	if created.Status != OAuthClientStatusActive || created.Version != 1 || !strings.HasPrefix(created.ClientID, managedOAuthClientIDPrefix) {
		t.Fatalf("created client = %#v", created)
	}
	if _, err := service.Get(ctx, otherTenant.ID, created.ClientID); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("cross-tenant get error = %v, want not found", err)
	}

	updated, err := service.Update(ctx, UpdateManagedOAuthClientInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID, ClientID: created.ClientID, Version: created.Version,
		DisplayName: "Research BI Desktop", RedirectURIs: []string{"https://bi.example.com/oauth/callback", "http://127.0.0.1:49152/callback"}, Audit: audit,
	})
	if err != nil || updated.Version != 2 || len(updated.RedirectURIs) != 2 {
		t.Fatalf("updated client = %#v, error = %v", updated, err)
	}
	if _, err := service.Update(ctx, UpdateManagedOAuthClientInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID, ClientID: created.ClientID, Version: created.Version,
		DisplayName: "Stale", RedirectURIs: created.RedirectURIs, Audit: audit,
	}); !errors.Is(err, ErrOAuthClientVersionConflict) {
		t.Fatalf("stale update error = %v, want version conflict", err)
	}

	requestID := uuid.New()
	if err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests (
			id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
			requested_scopes, requested_audiences, status, requested_at, expires_at
		) VALUES (?, ?, ?, ?, ARRAY['code']::text[], 'query', ARRAY['addp.api']::text[],
			ARRAY['addp.api']::text[], 'pending', ?, ?)
	`, requestID, strings.Repeat("a", 64), created.ClientID, updated.RedirectURIs[0], now.Add(-time.Minute), now.Add(4*time.Minute)).Error; err != nil {
		t.Fatalf("insert pending authorization request: %v", err)
	}
	var authorizationVersion int64
	if err := db.Raw(`SELECT authorization_version FROM system.principals WHERE id = ?`, user.PrincipalID).Scan(&authorizationVersion).Error; err != nil {
		t.Fatal(err)
	}
	var familyID int64
	if err := db.Raw(`
		INSERT INTO system.refresh_token_families (
			protocol_request_id, principal_id, context_type, tenant_membership_id,
			issued_authorization_version, client_id, auth_type, audiences, scopes,
			authentication_methods, assurance_level, authenticated_at, expires_at
		) VALUES (?, ?, 'tenant', ?, ?, ?, 'oauth', ARRAY['addp.api']::text[],
			ARRAY['addp.api']::text[], ARRAY['password']::text[], 'aal1', ?, ?)
		RETURNING id
	`, uuid.New(), user.PrincipalID, membership.Membership.ID, authorizationVersion, created.ClientID, now.Add(-time.Minute), now.Add(time.Hour)).Scan(&familyID).Error; err != nil {
		t.Fatalf("insert active token family: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at)
		VALUES (?, ?, ?)
	`, strings.Repeat("b", 64), familyID, now.Add(15*time.Minute)).Error; err != nil {
		t.Fatalf("insert active access token: %v", err)
	}

	suspended, err := service.Disable(ctx, ChangeManagedOAuthClientStatusInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID, ClientID: created.ClientID,
		Version: updated.Version, Reason: "connector retired", Audit: audit,
	})
	if err != nil || suspended.Status != OAuthClientStatusDisabled || suspended.Version != 3 {
		t.Fatalf("suspended client = %#v, error = %v", suspended, err)
	}
	var pendingStatus string
	if err := db.Raw(`SELECT status FROM system.oauth_authorization_requests WHERE id = ?`, requestID).Scan(&pendingStatus).Error; err != nil || pendingStatus != "cancelled" {
		t.Fatalf("authorization request status = %q, error = %v", pendingStatus, err)
	}
	var revokedFamilies, revokedAccessTokens int64
	if err := db.Raw(`SELECT count(*) FROM system.refresh_token_families WHERE id = ? AND revoked_at IS NOT NULL`, familyID).Scan(&revokedFamilies).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT count(*) FROM system.access_tokens WHERE family_id = ? AND revoked_at IS NOT NULL`, familyID).Scan(&revokedAccessTokens).Error; err != nil {
		t.Fatal(err)
	}
	if revokedFamilies != 1 || revokedAccessTokens != 1 {
		t.Fatalf("revoked families=%d access_tokens=%d", revokedFamilies, revokedAccessTokens)
	}

	restored, err := service.Restore(ctx, ChangeManagedOAuthClientStatusInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID, ClientID: created.ClientID,
		Version: suspended.Version, Reason: "connector approved again", Audit: audit,
	})
	if err != nil || restored.Status != OAuthClientStatusActive || restored.Version != 4 {
		t.Fatalf("restored client = %#v, error = %v", restored, err)
	}

	clients, total, err := service.List(ctx, tenant.ID, 1, 20, "Research", nil)
	if err != nil || total != 1 || len(clients) != 1 || clients[0].ClientID != created.ClientID {
		t.Fatalf("list clients = %#v total=%d error=%v", clients, total, err)
	}
}
