package iam

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTenantServiceAccountServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to addp_iam_test")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset service account test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply System migrations: %v", err)
	}

	repository := NewRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	identityService := NewIdentityService(repository, func() time.Time { return now })
	membershipService := NewTenantMembershipService(repository, func() time.Time { return now })
	service := NewTenantServiceAccountService(repository)
	externalOAuthService := NewOAuthClientManagementService(repository)
	bootstrapAudit := AuditMetadata{RequestID: stringPointer("service-account-bootstrap")}
	user := createContextSelectionUser(t, ctx, identityService, "service-account-admin", bootstrapAudit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "service-account", bootstrapAudit)
	otherTenant := createContextSelectionTenant(t, ctx, membershipService, "service-account-other", bootstrapAudit)
	contextType := ContextTypeTenant
	principalType := PrincipalTypeUser
	audit := AuditMetadata{
		PrincipalID: &user.PrincipalID, PrincipalType: &principalType,
		ContextType: &contextType, TenantID: &tenant.ID,
		RequestID: stringPointer("service-account-management"),
	}
	establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, audit)
	var platformRuntimeID int64
	if err := db.Raw(`
		SELECT service_principal.id
		FROM system.service_principals service_principal
		JOIN system.oauth_clients oauth_client
		  ON oauth_client.service_principal_id = service_principal.id
		 AND oauth_client.owner_scope = 'platform'
		WHERE service_principal.owner_scope = 'platform'
		ORDER BY service_principal.id
		LIMIT 1
	`).Scan(&platformRuntimeID).Error; err != nil || platformRuntimeID == 0 {
		t.Fatalf("find platform runtime identity: id=%d error=%v", platformRuntimeID, err)
	}
	if err := db.Exec(`
		INSERT INTO system.tenant_memberships (
			tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
		) VALUES (?, ?, 'active', 'bootstrap', ?, ?)
	`, tenant.ID, platformRuntimeID, now, user.PrincipalID).Error; err != nil {
		t.Fatalf("establish platform runtime membership: %v", err)
	}

	created, err := service.Create(ctx, CreateTenantServiceAccountInput{
		TenantID: tenant.ID, ActorPrincipalID: user.PrincipalID,
		Name: "Nightly Loader", Description: "Imports research files", Audit: audit,
	})
	if err != nil {
		t.Fatalf("create service account: %v", err)
	}
	if created.Account.Status != PrincipalStatusActive || created.Account.MembershipStatus != TenantMembershipStatusActive ||
		created.Account.CredentialStatus != OAuthClientStatusActive || created.Account.Version != 1 ||
		!strings.HasPrefix(created.Account.ClientID, tenantServiceClientIDPrefix) || len(created.ClientSecret) < 32 {
		t.Fatalf("created service account = %#v", created)
	}
	var secretHash string
	if err := db.Raw(`SELECT client_secret_hash FROM system.oauth_clients WHERE service_principal_id = ?`, created.Account.ID).Scan(&secretHash).Error; err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(secretHash), []byte(created.ClientSecret)) != nil {
		t.Fatal("created Client Secret does not match stored BCrypt hash")
	}
	if _, err := service.Get(ctx, otherTenant.ID, created.Account.ID); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("cross-tenant get error = %v, want not found", err)
	}
	if err := db.Exec(`
		INSERT INTO system.tenant_memberships (
			tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id
		) VALUES (?, ?, 'active', 'manual', ?, ?)
	`, otherTenant.ID, created.Account.ID, now, user.PrincipalID).Error; err == nil {
		t.Fatal("tenant-owned service principal joined a second tenant")
	}

	updated, err := service.Update(ctx, UpdateTenantServiceAccountInput{
		TenantID: tenant.ID, AccountID: created.Account.ID, Version: created.Account.Version,
		ActorPrincipalID: user.PrincipalID, Name: "Research Nightly Loader",
		Description: "Imports governed research files", Audit: audit,
	})
	if err != nil || updated.Version != 2 || updated.Name != "Research Nightly Loader" {
		t.Fatalf("updated service account = %#v, error = %v", updated, err)
	}
	if _, err := service.Update(ctx, UpdateTenantServiceAccountInput{
		TenantID: tenant.ID, AccountID: created.Account.ID, Version: created.Account.Version,
		ActorPrincipalID: user.PrincipalID, Name: "Stale", Audit: audit,
	}); !errors.Is(err, ErrServiceAccountVersionConflict) {
		t.Fatalf("stale update error = %v, want version conflict", err)
	}

	suspended, err := service.Suspend(ctx, ChangeTenantServiceAccountStatusInput{
		TenantID: tenant.ID, AccountID: created.Account.ID, Version: updated.Version,
		ActorPrincipalID: user.PrincipalID, Reason: "pipeline paused", Audit: audit,
	})
	if err != nil || suspended.Version != 3 || suspended.Status != PrincipalStatusSuspended ||
		suspended.MembershipStatus != TenantMembershipStatusSuspended || suspended.CredentialStatus != OAuthClientStatusDisabled {
		t.Fatalf("suspended service account = %#v, error = %v", suspended, err)
	}

	rotated, err := service.RotateSecret(ctx, RotateTenantServiceAccountSecretInput{
		TenantID: tenant.ID, AccountID: created.Account.ID, Version: suspended.Version,
		ActorPrincipalID: user.PrincipalID, Reason: "scheduled rotation", Audit: audit,
	})
	if err != nil || rotated.Account.Version != 4 || rotated.ClientSecret == created.ClientSecret ||
		rotated.Account.CredentialStatus != OAuthClientStatusDisabled {
		t.Fatalf("rotated service account = %#v, error = %v", rotated, err)
	}

	restored, err := service.Restore(ctx, ChangeTenantServiceAccountStatusInput{
		TenantID: tenant.ID, AccountID: created.Account.ID, Version: rotated.Account.Version,
		ActorPrincipalID: user.PrincipalID, Reason: "pipeline resumed", Audit: audit,
	})
	if err != nil || restored.Version != 5 || restored.Status != PrincipalStatusActive ||
		restored.MembershipStatus != TenantMembershipStatusActive || restored.CredentialStatus != OAuthClientStatusActive {
		t.Fatalf("restored service account = %#v, error = %v", restored, err)
	}

	accounts, total, err := service.List(ctx, tenant.ID, 1, 20, "Research", nil, nil)
	if err != nil || total != 1 || len(accounts) != 1 || accounts[0].ID != restored.ID {
		t.Fatalf("list service accounts = %#v total=%d error=%v", accounts, total, err)
	}
	runtimeScope := "platform"
	runtimeAccounts, runtimeTotal, err := service.List(ctx, tenant.ID, 1, 20, "", nil, &runtimeScope)
	if err != nil || runtimeTotal != 1 || len(runtimeAccounts) != 1 ||
		runtimeAccounts[0].ID != platformRuntimeID || runtimeAccounts[0].OwnerScope != "platform" {
		t.Fatalf("list platform runtime accounts = %#v total=%d error=%v", runtimeAccounts, runtimeTotal, err)
	}
	if _, err := service.Get(ctx, tenant.ID, platformRuntimeID); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("platform runtime get error = %v, want not found", err)
	}
	tenantScope := "tenant"
	tenantAccounts, tenantTotal, err := service.List(ctx, tenant.ID, 1, 20, "", nil, &tenantScope)
	if err != nil || tenantTotal != 1 || len(tenantAccounts) != 1 ||
		tenantAccounts[0].ID != restored.ID || tenantAccounts[0].OwnerScope != "tenant" {
		t.Fatalf("list tenant-managed accounts = %#v total=%d error=%v", tenantAccounts, tenantTotal, err)
	}
	allAccounts, allTotal, err := service.List(ctx, tenant.ID, 1, 20, "", nil, nil)
	if err != nil || allTotal != 2 || len(allAccounts) != 2 || allAccounts[0].OwnerScope != "tenant" || allAccounts[1].OwnerScope != "platform" {
		t.Fatalf("list all machine identities = %#v total=%d error=%v", allAccounts, allTotal, err)
	}
	otherRuntimeAccounts, otherRuntimeTotal, err := service.List(ctx, otherTenant.ID, 1, 20, "", nil, &runtimeScope)
	if err != nil || otherRuntimeTotal != 0 || len(otherRuntimeAccounts) != 0 {
		t.Fatalf("platform runtime membership leaked across tenants: accounts=%#v total=%d error=%v", otherRuntimeAccounts, otherRuntimeTotal, err)
	}
	externalClients, externalTotal, err := externalOAuthService.List(ctx, tenant.ID, 1, 20, "", nil)
	if err != nil || externalTotal != 0 || len(externalClients) != 0 {
		t.Fatalf("external OAuth list leaked service credential: clients=%#v total=%d error=%v", externalClients, externalTotal, err)
	}
	var auditCount int64
	if err := db.Raw(`SELECT count(*) FROM system.audit_logs WHERE entity_type = 'service_account' AND entity_id = ?`, strconv.FormatInt(restored.ID, 10)).Scan(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 5 {
		t.Fatalf("service account audit count = %d, want 5", auditCount)
	}
}
