package iam

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	passwordutils "github.com/addp/system/pkg/utils"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRecoveryServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(-time.Minute).Truncate(30 * time.Second)
	now := func() time.Time { return currentTime }
	cipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrapService, err := NewBootstrapService(
		repository, NewIdentityService(repository, now), cipher, time.Hour,
		func(prefix string) (string, error) { return prefix + "recovery-bootstrap-test", nil }, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapSecret, _, err := bootstrapService.Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare bootstrap: %v", err)
	}
	bootstrapAdministrators := []BootstrapAdministratorInput{
		bootstrapAdministratorTestInput(t, bootstrapRoleOrder[0], "system-admin", "System Administrator", "JBSWY3DPEHPK3PXP", currentTime),
		bootstrapAdministratorTestInput(t, bootstrapRoleOrder[1], "security-admin", "Security Administrator", "KRSXG5DSNFXGOIDB", currentTime),
		bootstrapAdministratorTestInput(t, bootstrapRoleOrder[2], "audit-admin", "Audit Administrator", "MFRGGZDFMZTWQ2LK", currentTime),
	}
	bootstrapResult, err := bootstrapService.Apply(ctx, BootstrapApplyInput{
		BootstrapSecret: bootstrapSecret, Administrators: bootstrapAdministrators,
	})
	if err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	initialAuthorizationVersions := make(map[string]int64, len(bootstrapRoleOrder))
	for roleKey, principalID := range bootstrapResult.PrincipalIDs {
		initialAuthorizationVersions[roleKey] = createRecoverySessionFacts(t, db, principalID, currentTime)
	}

	recoveryService, err := NewRecoveryService(
		repository, cipher, time.Hour,
		func(prefix string) (string, error) { return prefix + "recovery-postgres-test", nil }, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	recoverySecret, expiresAt, err := recoveryService.Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare recovery: %v", err)
	}
	if recoverySecret != "addp_ir_recovery-postgres-test" || !expiresAt.Equal(currentTime.Add(time.Hour)) {
		t.Fatalf("prepared recovery secret=%q expires=%s", recoverySecret, expiresAt)
	}
	if _, _, err := recoveryService.Prepare(ctx); err == nil {
		t.Fatal("second recovery prepare succeeded while an attempt is active")
	}
	targets, err := recoveryService.Validate(ctx, recoverySecret)
	if err != nil || len(targets) != 3 {
		t.Fatalf("validate recovery secret targets=%d error=%v", len(targets), err)
	}
	if _, err := recoveryService.Validate(ctx, "addp_ir_invalid"); err == nil {
		t.Fatal("invalid recovery secret was accepted")
	}

	inputs := []RecoveryAdministratorInput{
		recoveryAdministratorTestInput(t, bootstrapRoleOrder[0], "Recovered-password-system-admin!", "ONSWG4TFOQ======", currentTime),
		recoveryAdministratorTestInput(t, bootstrapRoleOrder[1], "Recovered-password-security-admin!", "ORSXG5A=", currentTime),
		recoveryAdministratorTestInput(t, bootstrapRoleOrder[2], "Recovered-password-audit-admin!", "MZXW6YTBOI======", currentTime),
	}
	installRecoveryCompletionAuditFailure(t, db)
	if _, err := recoveryService.Apply(ctx, RecoveryApplyInput{
		RecoverySecret: recoverySecret, Administrators: inputs,
	}); err == nil {
		t.Fatal("recovery succeeded despite forced completion-audit failure")
	}
	removeRecoveryCompletionAuditFailure(t, db)
	assertRecoveryRolledBack(t, db, bootstrapResult.PrincipalIDs[bootstrapRoleOrder[0]])

	result, err := recoveryService.Apply(ctx, RecoveryApplyInput{
		RecoverySecret: recoverySecret, Administrators: inputs,
	})
	if err != nil {
		t.Fatalf("apply recovery: %v", err)
	}
	if result.AttemptID <= 0 || len(result.Principals) != 3 || !result.CompletedAt.Equal(currentTime) {
		t.Fatalf("recovery result = %#v", result)
	}
	if _, err := recoveryService.Apply(ctx, RecoveryApplyInput{
		RecoverySecret: recoverySecret, Administrators: inputs,
	}); err == nil {
		t.Fatal("completed recovery applied twice")
	}

	for index, roleKey := range bootstrapRoleOrder {
		principalID := result.Principals[roleKey]
		var account LocalAccount
		if err := db.Where("user_id = ?", principalID).Take(&account).Error; err != nil {
			t.Fatalf("read recovered account %s: %v", roleKey, err)
		}
		if !passwordutils.CheckPassword(inputs[index].Password, account.PasswordHash) ||
			account.Status != LocalAccountStatusActive || account.LockedUntil != nil {
			t.Fatalf("account %s was not recovered", roleKey)
		}
		var activeCredential MFACredential
		if err := db.Where("user_id = ? AND method = 'totp' AND status = 'active'", principalID).
			Take(&activeCredential).Error; err != nil {
			t.Fatalf("read active recovery MFA %s: %v", roleKey, err)
		}
		decryptedSecret, err := cipher.DecryptTOTPSecret(&activeCredential)
		if err != nil || decryptedSecret != inputs[index].TOTPSecret {
			t.Fatalf("replacement MFA %s does not contain the new secret", roleKey)
		}
		var disabledCredentialCount, authorizationVersion, activeFamilyCount int64
		if err := db.Model(&MFACredential{}).
			Where("user_id = ? AND method = 'totp' AND status = 'disabled'", principalID).
			Count(&disabledCredentialCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.principals").Select("authorization_version").
			Where("id = ?", principalID).Row().Scan(&authorizationVersion); err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.refresh_token_families").
			Where("principal_id = ? AND revoked_at IS NULL", principalID).
			Count(&activeFamilyCount).Error; err != nil {
			t.Fatal(err)
		}
		if disabledCredentialCount != 1 ||
			authorizationVersion != initialAuthorizationVersions[roleKey]+1 || activeFamilyCount != 0 {
			t.Fatalf("recovery facts %s disabled_mfa=%d version=%d active_families=%d",
				roleKey, disabledCredentialCount, authorizationVersion, activeFamilyCount)
		}
		assertRecoveryPendingFactsConsumed(t, db, principalID)
	}

	var status string
	var storedSecret *string
	if err := db.Table("system.iam_recovery_attempts").
		Select("status, secret_hash").Where("id = ?", result.AttemptID).
		Row().Scan(&status, &storedSecret); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || storedSecret != nil {
		t.Fatalf("recovery attempt status=%q secret=%v", status, storedSecret)
	}
	for eventName, want := range map[string]int64{
		"iam.recovery.prepared":                           1,
		"iam.recovery.administrator_credentials_replaced": 3,
		"iam.recovery.completed":                          1,
	} {
		var count int64
		if err := db.Table("system.audit_logs").Where("event_name = ?", eventName).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("audit %s count=%d want=%d", eventName, count, want)
		}
	}
	var leakedAuditCount int64
	if err := db.Table("system.audit_logs").
		Where("details::text LIKE ? OR details::text LIKE ?", "%Recovered-password%", "%ONSWG4TFOQ%").
		Count(&leakedAuditCount).Error; err != nil {
		t.Fatal(err)
	}
	if leakedAuditCount != 0 {
		t.Fatalf("recovery audit leaked secret material in %d rows", leakedAuditCount)
	}
}

func recoveryAdministratorTestInput(
	t *testing.T,
	roleKey string,
	password string,
	secret string,
	now time.Time,
) RecoveryAdministratorInput {
	t.Helper()
	firstAt := now.Add(-30 * time.Second)
	firstCode, err := totp.GenerateCode(secret, firstAt)
	if err != nil {
		t.Fatal(err)
	}
	secondCode, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	return RecoveryAdministratorInput{
		RoleKey: roleKey, Password: password, TOTPSecret: secret,
		TOTPProofs: []BootstrapTOTPProof{
			{Code: firstCode, VerifiedAt: firstAt},
			{Code: secondCode, VerifiedAt: now},
		},
	}
}

func createRecoverySessionFacts(t *testing.T, db *gorm.DB, principalID int64, now time.Time) int64 {
	t.Helper()
	createdAt := now.Add(-time.Minute)
	var authorizationVersion int64
	if err := db.Table("system.principals").Select("authorization_version").
		Where("id = ?", principalID).Row().Scan(&authorizationVersion); err != nil {
		t.Fatalf("read recovery principal authorization version: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.refresh_token_families
		    (principal_id, context_type, issued_authorization_version, client_id, auth_type,
		     audiences, scopes, authentication_methods, assurance_level, authenticated_at,
		     expires_at, created_at, updated_at)
		VALUES (?, 'platform', ?, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[],
		        ARRAY['password','totp'], 'aal2', ?, ?, ?, ?)
	`, principalID, authorizationVersion, createdAt, now.Add(time.Hour), createdAt, createdAt).Error; err != nil {
		t.Fatalf("create recovery token family: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.mfa_challenges
		    (token_hash, principal_id, issued_authorization_version, authentication_methods,
		     authenticated_at, expires_at, created_at, purpose)
		VALUES (?, ?, ?, ARRAY['password'], ?, ?, ?, 'login')
	`, hashOpaqueToken("mfa-challenge-"+strconv.FormatInt(principalID, 10)), principalID,
		authorizationVersion, createdAt, now.Add(time.Hour), createdAt).Error; err != nil {
		t.Fatalf("create recovery MFA challenge: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, client_id,
		     authentication_methods, assurance_level, authenticated_at, expires_at, created_at)
		VALUES (?, ?, ?, 'addp-web', ARRAY['password','totp'], 'aal2', ?, ?, ?)
	`, hashOpaqueToken("context-ticket-"+strconv.FormatInt(principalID, 10)), principalID,
		authorizationVersion, createdAt, now.Add(time.Hour), createdAt).Error; err != nil {
		t.Fatalf("create recovery context ticket: %v", err)
	}
	return authorizationVersion
}

func installRecoveryCompletionAuditFailure(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`
		CREATE FUNCTION system.reject_recovery_completion_audit()
		RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		    RAISE EXCEPTION 'forced recovery completion audit failure';
		END;
		$$;
		CREATE TRIGGER trg_reject_recovery_completion_audit
		BEFORE INSERT ON system.audit_logs
		FOR EACH ROW WHEN (NEW.event_name = 'iam.recovery.completed')
		EXECUTE FUNCTION system.reject_recovery_completion_audit();
	`).Error; err != nil {
		t.Fatalf("install recovery audit failure: %v", err)
	}
}

func removeRecoveryCompletionAuditFailure(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`
		DROP TRIGGER trg_reject_recovery_completion_audit ON system.audit_logs;
		DROP FUNCTION system.reject_recovery_completion_audit();
	`).Error; err != nil {
		t.Fatalf("remove recovery audit failure: %v", err)
	}
}

func assertRecoveryRolledBack(t *testing.T, db *gorm.DB, principalID int64) {
	t.Helper()
	var account LocalAccount
	if err := db.Where("user_id = ?", principalID).Take(&account).Error; err != nil {
		t.Fatal(err)
	}
	if !passwordutils.CheckPassword("Bootstrap-password-system-admin!", account.PasswordHash) {
		t.Fatal("failed recovery changed the account password")
	}
	var activeCredentialCount, disabledCredentialCount, activeFamilyCount int64
	db.Model(&MFACredential{}).Where("user_id = ? AND status = 'active'", principalID).Count(&activeCredentialCount)
	db.Model(&MFACredential{}).Where("user_id = ? AND status = 'disabled'", principalID).Count(&disabledCredentialCount)
	db.Table("system.refresh_token_families").Where("principal_id = ? AND revoked_at IS NULL", principalID).Count(&activeFamilyCount)
	if activeCredentialCount != 1 || disabledCredentialCount != 0 || activeFamilyCount != 1 {
		t.Fatalf("failed recovery was not atomic: active_mfa=%d disabled_mfa=%d active_families=%d",
			activeCredentialCount, disabledCredentialCount, activeFamilyCount)
	}
}

func assertRecoveryPendingFactsConsumed(t *testing.T, db *gorm.DB, principalID int64) {
	t.Helper()
	for table := range map[string]struct{}{
		"system.mfa_challenges": {}, "system.context_selection_tickets": {},
	} {
		var pending int64
		if err := db.Table(table).Where("principal_id = ? AND consumed_at IS NULL", principalID).
			Count(&pending).Error; err != nil {
			t.Fatal(err)
		}
		if pending != 0 {
			t.Fatalf("%s still has %d pending facts", table, pending)
		}
	}
}
