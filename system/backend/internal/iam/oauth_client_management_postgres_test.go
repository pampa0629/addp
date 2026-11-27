package iam

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/infra/database/dbtypes"
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

	exerciseManagedOAuthClient(t, ctx, db)
}

func TestOAuthClientManagementServiceAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_OAUTH_CLIENT_MANAGEMENT") != "1" {
		t.Skip("requires isolated System MySQL installation")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Schema: "system"}, "system", &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	exerciseManagedOAuthClient(t, t.Context(), db)
}

func exerciseManagedOAuthClient(t *testing.T, ctx context.Context, db *gorm.DB) {
	t.Helper()
	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	identityService := NewIdentityService(repository, testDatabaseTime(func() time.Time { return now }))
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

	redirectURIs := []string{"https://bi.example.com/oauth/callback", "http://127.0.0.1:49152/callback"}
	updated, err := service.Update(ctx, UpdateManagedOAuthClientInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID, ClientID: created.ClientID, Version: created.Version,
		DisplayName: "Research BI Desktop", RedirectURIs: redirectURIs, Audit: audit,
	})
	if err != nil || updated.Version != 2 || !slices.Equal(updated.RedirectURIs, redirectURIs) ||
		!slices.Equal(updated.AllowedScopes, []string{"addp.api"}) || !slices.Equal(updated.AllowedAudiences, []string{"addp.api"}) {
		t.Fatalf("updated client = %#v, error = %v", updated, err)
	}
	if _, err := service.Update(ctx, UpdateManagedOAuthClientInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID, ClientID: created.ClientID, Version: created.Version,
		DisplayName: "Stale", RedirectURIs: created.RedirectURIs, Audit: audit,
	}); !errors.Is(err, ErrOAuthClientVersionConflict) {
		t.Fatalf("stale update error = %v, want version conflict", err)
	}

	insertRequest := func(id uuid.UUID, scopes dbtypes.StringArray) error {
		return db.Exec(`
    INSERT INTO system.oauth_authorization_requests (
      id,request_secret_hash,client_id,redirect_uri,response_types,response_mode,requested_scopes,requested_audiences,status,requested_at,expires_at
    ) VALUES (?,?,?,?,?,'query',?,?,'pending',?,?)
  `, id, strings.Repeat(strings.ReplaceAll(id.String(), "-", ""), 2), created.ClientID, updated.RedirectURIs[0],
			dbtypes.StringArray{"code"}, scopes, dbtypes.StringArray{"addp.api"}, now.Add(-time.Minute), now.Add(4*time.Minute)).Error
	}
	requestID := uuid.New()
	if err := insertRequest(requestID, dbtypes.StringArray{"addp.api"}); err != nil {
		t.Fatalf("insert pending authorization request: %v", err)
	}
	var authorizationVersion int64
	if err := db.Raw(`SELECT authorization_version FROM system.principals WHERE id = ?`, user.PrincipalID).Scan(&authorizationVersion).Error; err != nil {
		t.Fatal(err)
	}
	t.Run("registered scope and terminal approval", func(t *testing.T) {
		if err := insertRequest(uuid.New(), dbtypes.StringArray{"other"}); err == nil {
			t.Fatal("request exceeded registered scope")
		}
		id := uuid.New()
		if err := insertRequest(id, dbtypes.StringArray{"addp.api"}); err != nil {
			t.Fatal(err)
		}
		approve := func(version int64, scopes dbtypes.StringArray) error {
			return db.Exec(`
     UPDATE system.oauth_authorization_requests
     SET status='approved',principal_id=?,context_type='tenant',tenant_membership_id=?,issued_authorization_version=?,
       granted_scopes=?,granted_audiences=?,authentication_methods=?,assurance_level='aal1',authenticated_at=?,completed_at=CURRENT_TIMESTAMP(6)
     WHERE id=?
   `, user.PrincipalID, membership.Membership.ID, version, scopes, dbtypes.StringArray{"addp.api"}, dbtypes.StringArray{"password"}, now, id).Error
		}
		if err := approve(authorizationVersion+1, dbtypes.StringArray{"addp.api"}); err == nil {
			t.Fatal("stale identity approved")
		}
		if err := approve(authorizationVersion, dbtypes.StringArray{"other"}); err == nil {
			t.Fatal("scope outside request approved")
		}
		if err := approve(authorizationVersion, dbtypes.StringArray{"addp.api"}); err != nil {
			t.Fatalf("approve pending request: %v", err)
		}
		if err := approve(authorizationVersion, dbtypes.StringArray{"addp.api"}); err == nil {
			t.Fatal("terminal approval was rewritten")
		}
	})
	protocolID := uuid.New()
	family := RefreshTokenFamily{
		ProtocolRequestID: &protocolID, PrincipalID: user.PrincipalID, ContextType: ContextTypeTenant,
		TenantMembershipID: &membership.Membership.ID, IssuedAuthorizationVersion: authorizationVersion,
		ClientID: created.ClientID, AuthType: "oauth", Audiences: DBStringList{"addp.api"}, Scopes: DBStringList{"addp.api"},
		AuthenticationMethods: DBStringList{"password"}, AssuranceLevel: AssuranceLevelAAL1,
		AuthenticatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := db.Create(&family).Error; err != nil {
		t.Fatalf("insert active token family: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at)
		VALUES (?, ?, ?)
	`, strings.Repeat("b", 64), family.ID, now.Add(15*time.Minute)).Error; err != nil {
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
	if err := db.Raw(`SELECT count(*) FROM system.refresh_token_families WHERE id = ? AND revoked_at IS NOT NULL`, family.ID).Scan(&revokedFamilies).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT count(*) FROM system.access_tokens WHERE family_id = ? AND revoked_at IS NOT NULL`, family.ID).Scan(&revokedAccessTokens).Error; err != nil {
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
