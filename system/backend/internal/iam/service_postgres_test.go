package iam

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	passwordutils "github.com/addp/system/pkg/utils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIAMServicesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset IAM service test schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)
	validAudit := AuditMetadata{RequestID: stringPointer("iam-service-test")}

	created, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username:    " Alice ",
		Password:    "initial-password",
		DisplayName: "Alice",
		Audit:       validAudit,
	})
	if err != nil {
		t.Fatalf("create local user: %v", err)
	}
	if created.AuthorizationVersion != 1 {
		t.Fatalf("created authorization version = %d, want 1", created.AuthorizationVersion)
	}
	assertIAMServiceAuditCount(t, db, "iam.identity.created", AuditResultSucceeded, 1)

	principalCountBeforeRollback := countIAMServiceRows(t, db, "system.principals")
	_, err = identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username:    "rollback-user",
		Password:    "rollback-password",
		DisplayName: "Rollback User",
		Audit:       AuditMetadata{RequestID: stringPointer(" ")},
	})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("create user with rejected audit error = %v, want bad request", err)
	}
	if got := countIAMServiceRows(t, db, "system.principals"); got != principalCountBeforeRollback {
		t.Fatalf("principal count after rejected audit = %d, want %d", got, principalCountBeforeRollback)
	}
	assertIAMServiceTableCount(t, db, "system.local_accounts", 1)

	if _, err := identityService.AuthenticateLocalAccount(
		ctx, "alice", "wrong-password", validAudit,
	); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("wrong password error = %v, want unauthorized", err)
	}
	assertIAMServiceAuditCount(t, db, "iam.authentication.failed", AuditResultDenied, 1)
	if _, err := identityService.AuthenticateLocalAccount(
		ctx, "missing-user", "wrong-password", validAudit,
	); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("missing username error = %v, want unauthorized", err)
	}
	assertIAMServiceAuditCount(t, db, "iam.authentication.failed", AuditResultDenied, 2)

	authenticated, err := identityService.AuthenticateLocalAccount(
		ctx, " ＡＬＩＣＥ ", "initial-password", validAudit,
	)
	if err != nil {
		t.Fatalf("authenticate local account: %v", err)
	}
	if authenticated.PrincipalID != created.PrincipalID || authenticated.AccountID != created.AccountID {
		t.Fatalf("authenticated identity = %#v, want principal %d account %d", authenticated, created.PrincipalID, created.AccountID)
	}
	assertIAMServiceAuditCount(t, db, "iam.authentication.succeeded", AuditResultSucceeded, 1)

	currentTime = currentTime.Add(time.Second)
	tenant, err := membershipService.CreateTenant(ctx, CreateTenantInput{
		Code:        " Research-Lab ",
		Name:        "Research Lab",
		Description: "IAM service integration tenant",
		Audit:       validAudit,
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if tenant.Code != "research-lab" {
		t.Fatalf("tenant code = %q, want research-lab", tenant.Code)
	}

	currentTime = currentTime.Add(time.Second)
	established, err := membershipService.EstablishMembership(ctx, EstablishTenantMembershipInput{
		TenantID:             tenant.ID,
		PrincipalID:          created.PrincipalID,
		SourceType:           TenantMembershipSourceManual,
		CreatedByPrincipalID: &created.PrincipalID,
		Audit:                validAudit,
	})
	if err != nil {
		t.Fatalf("establish membership: %v", err)
	}
	if established.AuthorizationVersion != 2 || established.Membership.Status != TenantMembershipStatusActive {
		t.Fatalf("established membership result = %#v", established)
	}
	assertIAMServiceAuditCount(t, db, "iam.tenant_membership.established", AuditResultSucceeded, 1)

	familyBeforeRejectedSuspend := createIAMServiceTokenFamily(
		t, db, established.Membership, established.AuthorizationVersion, 'a', currentTime,
	)
	_, err = membershipService.SuspendMembership(ctx, ChangeTenantMembershipInput{
		TenantID:    tenant.ID,
		PrincipalID: created.PrincipalID,
		Audit:       AuditMetadata{RequestID: stringPointer(" ")},
	})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("suspend with rejected audit error = %v, want bad request", err)
	}
	assertIAMServiceMembershipState(t, db, established.Membership.ID, TenantMembershipStatusActive, nil)
	assertIAMServiceAuthorizationVersion(t, db, created.PrincipalID, 2)
	assertIAMServiceFamilyRevoked(t, db, familyBeforeRejectedSuspend, false)
	assertIAMServiceAuditCount(t, db, "iam.tenant_membership.suspended", AuditResultSucceeded, 0)

	currentTime = currentTime.Add(time.Second)
	suspended, err := membershipService.SuspendMembership(ctx, ChangeTenantMembershipInput{
		TenantID:    tenant.ID,
		PrincipalID: created.PrincipalID,
		Audit:       validAudit,
	})
	if err != nil {
		t.Fatalf("suspend membership: %v", err)
	}
	if suspended.AuthorizationVersion != 3 || suspended.RevokedFamilyCount != 1 {
		t.Fatalf("suspended membership result = %#v", suspended)
	}
	assertIAMServiceFamilyAndDerivativesRevoked(t, db, familyBeforeRejectedSuspend)

	currentTime = currentTime.Add(time.Second)
	restored, err := membershipService.RestoreMembership(ctx, ChangeTenantMembershipInput{
		TenantID:    tenant.ID,
		PrincipalID: created.PrincipalID,
		Audit:       validAudit,
	})
	if err != nil {
		t.Fatalf("restore membership: %v", err)
	}
	if restored.AuthorizationVersion != 4 || restored.Membership.Status != TenantMembershipStatusActive {
		t.Fatalf("restored membership result = %#v", restored)
	}

	familyBeforeRejectedRotation := createIAMServiceTokenFamily(
		t, db, restored.Membership, restored.AuthorizationVersion, '5', currentTime,
	)
	accountBeforeRejectedRotation := readIAMServiceLocalAccount(t, db, created.PrincipalID)
	_, err = identityService.RotatePassword(ctx, RotatePasswordInput{
		UserID:          created.PrincipalID,
		CurrentPassword: "initial-password",
		NewPassword:     "rejected-new-password",
		Audit:           AuditMetadata{RequestID: stringPointer(" ")},
	})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("rotate password with rejected audit error = %v, want bad request", err)
	}
	accountAfterRejectedRotation := readIAMServiceLocalAccount(t, db, created.PrincipalID)
	if accountAfterRejectedRotation.PasswordHash != accountBeforeRejectedRotation.PasswordHash {
		t.Fatal("password hash changed despite rejected audit")
	}
	assertIAMServiceAuthorizationVersion(t, db, created.PrincipalID, 4)
	assertIAMServiceFamilyRevoked(t, db, familyBeforeRejectedRotation, false)

	currentTime = currentTime.Add(time.Second)
	rotated, err := identityService.RotatePassword(ctx, RotatePasswordInput{
		UserID:          created.PrincipalID,
		CurrentPassword: "initial-password",
		NewPassword:     "new-password",
		Audit:           validAudit,
	})
	if err != nil {
		t.Fatalf("rotate password: %v", err)
	}
	if rotated.AuthorizationVersion != 5 || rotated.RevokedFamilyCount != 1 {
		t.Fatalf("password rotation result = %#v", rotated)
	}
	assertIAMServiceFamilyAndDerivativesRevoked(t, db, familyBeforeRejectedRotation)
	rotatedAccount := readIAMServiceLocalAccount(t, db, created.PrincipalID)
	if !passwordutils.CheckPassword("new-password", rotatedAccount.PasswordHash) ||
		passwordutils.CheckPassword("initial-password", rotatedAccount.PasswordHash) {
		t.Fatal("password rotation did not persist only the new password")
	}
	assertIAMServiceAuditCount(t, db, "iam.password.rotated", AuditResultSucceeded, 1)

	currentTime = currentTime.Add(time.Second)
	familyBeforeEnd := createIAMServiceTokenFamily(
		t, db, restored.Membership, rotated.AuthorizationVersion, '9', currentTime,
	)
	ended, err := membershipService.EndMembership(ctx, ChangeTenantMembershipInput{
		TenantID:    tenant.ID,
		PrincipalID: created.PrincipalID,
		Audit:       validAudit,
	})
	if err != nil {
		t.Fatalf("end membership: %v", err)
	}
	if ended.AuthorizationVersion != 6 || ended.RevokedFamilyCount != 1 || ended.Membership.EndedAt == nil {
		t.Fatalf("ended membership result = %#v", ended)
	}
	assertIAMServiceFamilyAndDerivativesRevoked(t, db, familyBeforeEnd)
	assertIAMServiceAuditCount(t, db, "iam.tenant_membership.ended", AuditResultSucceeded, 1)
}

func createIAMServiceTokenFamily(
	t *testing.T,
	db *gorm.DB,
	membership TenantMembership,
	authorizationVersion int64,
	hashStart byte,
	now time.Time,
) int64 {
	t.Helper()
	var familyID int64
	err := db.Raw(`
		INSERT INTO system.refresh_token_families
		    (principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    (?, 'tenant', ?, ?, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[],
		     ARRAY['password'], 'aal1', ?, ?)
		RETURNING id
	`, membership.PrincipalID, membership.ID, authorizationVersion, now.Add(-time.Minute), now.Add(time.Hour)).Scan(&familyID).Error
	if err != nil {
		t.Fatalf("create token family: %v", err)
	}

	accessHash := strings.Repeat(string(hashStart), 64)
	refreshHash := strings.Repeat(string(nextHexByte(hashStart, 1)), 64)
	delegatedHash := strings.Repeat(string(nextHexByte(hashStart, 2)), 64)
	ticketHash := strings.Repeat(string(nextHexByte(hashStart, 3)), 64)
	var accessTokenID int64
	if err := db.Raw(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at)
		VALUES (?, ?, ?) RETURNING id
	`, accessHash, familyID, now.Add(15*time.Minute)).Scan(&accessTokenID).Error; err != nil {
		t.Fatalf("create access token: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.refresh_tokens (token_hash, family_id, issued_access_token_id, expires_at)
		VALUES (?, ?, ?, ?)
	`, refreshHash, familyID, accessTokenID, now.Add(59*time.Minute)).Error; err != nil {
		t.Fatalf("create refresh token: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.delegated_access_tokens
		    (token_hash, source_access_token_id, audience, scopes, agent_run_id, tool_call_id, expires_at)
		VALUES (?, ?, 'develop', ARRAY['workflow.run'], ?, ?, ?)
	`, delegatedHash, accessTokenID, "run-"+accessHash[:4], "call-"+accessHash[:4], now.Add(2*time.Minute)).Error; err != nil {
		t.Fatalf("create delegated access token: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.resource_access_tickets (token_hash, family_id, owner, expires_at)
		VALUES (?, ?, 'manager', ?)
	`, ticketHash, familyID, now.Add(15*time.Minute)).Error; err != nil {
		t.Fatalf("create resource access ticket: %v", err)
	}
	return familyID
}

func nextHexByte(start byte, offset int) byte {
	hex := "0123456789abcdef"
	index := strings.IndexByte(hex, start)
	if index < 0 {
		index = 0
	}
	return hex[(index+offset)%len(hex)]
}

func readIAMServiceLocalAccount(t *testing.T, db *gorm.DB, userID int64) LocalAccount {
	t.Helper()
	var account LocalAccount
	if err := db.Where("user_id = ?", userID).Take(&account).Error; err != nil {
		t.Fatalf("read local account: %v", err)
	}
	return account
}

func assertIAMServiceAuthorizationVersion(t *testing.T, db *gorm.DB, principalID int64, want int64) {
	t.Helper()
	var version int64
	if err := db.Table("system.principals").Select("authorization_version").Where("id = ?", principalID).Scan(&version).Error; err != nil {
		t.Fatalf("read authorization version: %v", err)
	}
	if version != want {
		t.Fatalf("authorization version = %d, want %d", version, want)
	}
}

func assertIAMServiceMembershipState(
	t *testing.T,
	db *gorm.DB,
	membershipID int64,
	wantStatus TenantMembershipStatus,
	wantEndedAt *time.Time,
) {
	t.Helper()
	var membership TenantMembership
	if err := db.First(&membership, membershipID).Error; err != nil {
		t.Fatalf("read membership: %v", err)
	}
	if membership.Status != wantStatus || (membership.EndedAt == nil) != (wantEndedAt == nil) {
		t.Fatalf("membership state = status:%s ended_at:%v", membership.Status, membership.EndedAt)
	}
}

func assertIAMServiceFamilyRevoked(t *testing.T, db *gorm.DB, familyID int64, want bool) {
	t.Helper()
	var revokedAt sql.NullTime
	if err := db.Table("system.refresh_token_families").Select("revoked_at").Where("id = ?", familyID).Scan(&revokedAt).Error; err != nil {
		t.Fatalf("read token family revocation: %v", err)
	}
	if revokedAt.Valid != want {
		t.Fatalf("family %d revoked = %t, want %t", familyID, revokedAt.Valid, want)
	}
}

func assertIAMServiceFamilyAndDerivativesRevoked(t *testing.T, db *gorm.DB, familyID int64) {
	t.Helper()
	assertIAMServiceFamilyRevoked(t, db, familyID, true)
	for _, assertion := range []struct {
		table string
		join  string
	}{
		{table: "system.access_tokens", join: "family_id = ?"},
		{table: "system.refresh_tokens", join: "family_id = ?"},
		{table: "system.resource_access_tickets", join: "family_id = ?"},
		{table: "system.delegated_access_tokens", join: "source_access_token_id IN (SELECT id FROM system.access_tokens WHERE family_id = ?)"},
	} {
		var activeCount int64
		if err := db.Table(assertion.table).Where(assertion.join+" AND revoked_at IS NULL", familyID).Count(&activeCount).Error; err != nil {
			t.Fatalf("count active derivatives in %s: %v", assertion.table, err)
		}
		if activeCount != 0 {
			t.Fatalf("active derivative count in %s = %d, want 0", assertion.table, activeCount)
		}
	}
}

func assertIAMServiceAuditCount(
	t *testing.T,
	db *gorm.DB,
	eventName string,
	result AuditResult,
	want int64,
) {
	t.Helper()
	var count int64
	if err := db.Table("system.audit_logs").
		Where("event_name = ? AND result = ?", eventName, result).
		Count(&count).Error; err != nil {
		t.Fatalf("count %s audit events: %v", eventName, err)
	}
	if count != want {
		t.Fatalf("%s/%s audit count = %d, want %d", eventName, result, count, want)
	}
}

func countIAMServiceRows(t *testing.T, db *gorm.DB, tableName string) int64 {
	t.Helper()
	var count int64
	if err := db.Table(tableName).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	return count
}

func assertIAMServiceTableCount(t *testing.T, db *gorm.DB, tableName string, want int64) {
	t.Helper()
	if got := countIAMServiceRows(t, db, tableName); got != want {
		t.Fatalf("%s count = %d, want %d", tableName, got, want)
	}
}

func stringPointer(value string) *string { return &value }
