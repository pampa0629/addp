package iam

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
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
		repository, NewIdentityService(repository, testDatabaseTime(now)), cipher, time.Hour,
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
	governance := NewAdministratorChangeService(repository)
	secondSystem, err := NewIdentityService(repository, testDatabaseTime(now)).CreateLocalUser(ctx, CreateLocalUserInput{
		Username: "second-system-admin", DisplayName: "Second System Administrator", Password: "Original-second-system-admin-password!",
	})
	if err != nil {
		t.Fatal(err)
	}
	createManagementMFACredential(t, ctx, repository, secondSystem.PrincipalID)
	grant, err := governance.Create(ctx, CreateAdministratorChangeInput{
		ChangeType: PrivilegedChangePlatformRoleGrant, TargetPrincipalID: secondSystem.PrincipalID,
		TargetRoleKey: "platform.system_administrator", RequestedByPrincipalID: bootstrapResult.PrincipalIDs["platform.security_administrator"],
		Reason: "recover every current holder of the same role",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := governance.Approve(ctx, ReviewAdministratorChangeInput{
		RequestID: grant.ID, Version: grant.Version, ReviewerPrincipalID: bootstrapResult.PrincipalIDs["platform.audit_administrator"], Reason: "independent approval",
	}); err != nil {
		t.Fatal(err)
	}
	principalIDs := []int64{secondSystem.PrincipalID}
	for _, principalID := range bootstrapResult.PrincipalIDs {
		principalIDs = append(principalIDs, principalID)
	}
	initialAuthorizationVersions := make(map[int64]int64, len(principalIDs))
	for _, principalID := range principalIDs {
		initialAuthorizationVersions[principalID] = createRecoverySessionFacts(t, db, principalID, currentTime)
	}

	suspendedID := bootstrapResult.PrincipalIDs["platform.audit_administrator"]
	suspension, err := governance.Create(ctx, CreateAdministratorChangeInput{ChangeType: PrivilegedChangePlatformIdentitySuspend, TargetPrincipalID: suspendedID, RequestedByPrincipalID: bootstrapResult.PrincipalIDs["platform.security_administrator"], Reason: "suspension must survive offline credential recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := governance.Approve(ctx, ReviewAdministratorChangeInput{RequestID: suspension.ID, Version: suspension.Version, ReviewerPrincipalID: bootstrapResult.PrincipalIDs["platform.system_administrator"], Reason: "independent approval"}); err != nil {
		t.Fatal(err)
	}
	suspendedPrincipal, err := repository.GetPrincipal(ctx, suspendedID)
	if err != nil {
		t.Fatal(err)
	}
	initialAuthorizationVersions[suspendedID] = suspendedPrincipal.AuthorizationVersion

	// Governance uses the database clock. Observe its grant before freezing
	// recovery time so the fourth assignment is current at validation.
	currentTime = time.Now().UTC()
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
	if err != nil || len(targets) != len(principalIDs) {
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
	for i := range inputs {
		inputs[i].PrincipalID = bootstrapResult.PrincipalIDs[inputs[i].RoleKey]
	}
	secondInput := recoveryAdministratorTestInput(t, "platform.system_administrator", "Recovered-password-second-system-admin!", "GEZDGNBVGY3TQOJQ", currentTime)
	secondInput.PrincipalID = secondSystem.PrincipalID
	inputs = append(inputs, secondInput)
	swapped := append([]RecoveryAdministratorInput(nil), inputs...)
	swapped[0].PrincipalID, swapped[1].PrincipalID = swapped[1].PrincipalID, swapped[0].PrincipalID
	if _, err := recoveryService.Apply(ctx, RecoveryApplyInput{RecoverySecret: recoverySecret, Administrators: swapped}); err == nil {
		t.Fatal("recovery used a role name instead of the bound principal identity")
	}
	t.Run("completion_audit_failure_rolls_back_all_four_holders", func(t *testing.T) {
		before := snapshotRecoveryState(t, db, principalIDs)
		installRecoveryCompletionAuditFailure(t, db)
		defer removeRecoveryCompletionAuditFailure(t, db)
		failed, err := recoveryService.Apply(ctx, RecoveryApplyInput{RecoverySecret: recoverySecret, Administrators: inputs})
		if err == nil || !strings.Contains(err.Error(), "forced recovery completion audit failure") || failed != nil {
			t.Fatal("recovery did not fail at the injected completion audit boundary")
		}
		after := snapshotRecoveryState(t, db, principalIDs)
		for table, baseline := range before {
			if after[table] != baseline {
				// Snapshots contain password hashes and encrypted MFA material.
				// Never render either side in a test failure or CI log.
				t.Fatalf("failed recovery changed persisted state in %s", table)
			}
		}
		if targets, err := recoveryService.Validate(ctx, recoverySecret); err != nil || len(targets) != len(principalIDs) {
			t.Fatal("failed recovery consumed its secret or lost a current holder")
		}
		t.Log("all four holders and ten persisted state sets are unchanged after the final audit write fails")
	})

	result, err := recoveryService.Apply(ctx, RecoveryApplyInput{
		RecoverySecret: recoverySecret, Administrators: inputs,
	})
	if err != nil {
		t.Fatalf("apply recovery: %v", err)
	}
	if result.AttemptID <= 0 || len(result.Principals) != len(principalIDs) || !result.CompletedAt.Equal(currentTime) {
		t.Fatalf("recovery result = %#v", result)
	}
	if _, err := recoveryService.Apply(ctx, RecoveryApplyInput{
		RecoverySecret: recoverySecret, Administrators: inputs,
	}); err == nil {
		t.Fatal("completed recovery applied twice")
	}

	for index, input := range inputs {
		roleKey, principalID := input.RoleKey, input.PrincipalID
		if result.Principals[principalID] != roleKey {
			t.Fatal("recovered identity does not match role")
		}
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
			authorizationVersion != initialAuthorizationVersions[principalID]+1 || activeFamilyCount != 0 {
			t.Fatalf("recovery facts %s disabled_mfa=%d version=%d active_families=%d",
				roleKey, disabledCredentialCount, authorizationVersion, activeFamilyCount)
		}
		assertRecoveryPendingFactsConsumed(t, db, principalID)
	}

	afterRecovery, err := repository.GetPrincipal(ctx, suspendedID)
	if err != nil || afterRecovery.Status != PrincipalStatusSuspended {
		t.Fatal("offline recovery changed suspension")
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
		"iam.recovery.administrator_credentials_replaced": int64(len(principalIDs)),
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

func snapshotRecoveryState(t *testing.T, db *gorm.DB, principalIDs []int64) map[string]string {
	t.Helper()
	state := make(map[string]string)
	capture := func(table, predicate string, args ...any) {
		t.Helper()
		var value string
		// Sort by the complete JSONB row: singleton state has no id column.
		query := fmt.Sprintf("SELECT COALESCE(jsonb_agg(to_jsonb(recovery_row) ORDER BY to_jsonb(recovery_row)), '[]'::jsonb)::text FROM %s AS recovery_row WHERE %s", table, predicate)
		if err := db.Raw(query, args...).Scan(&value).Error; err != nil {
			t.Fatalf("snapshot recovery state in %s: %v", table, err)
		}
		state[table] = value
	}
	// Compare whole rows, not just row counts: failed recovery must restore
	// exact hashes/ciphertexts, status/version, family revocation and ticket use.
	for table, principalColumn := range map[string]string{
		"system.principals": "id", "system.local_accounts": "user_id",
		"system.mfa_credentials": "user_id", "system.role_assignments": "principal_id",
		"system.refresh_token_families": "principal_id", "system.mfa_challenges": "principal_id",
		"system.context_selection_tickets": "principal_id",
	} {
		capture(table, principalColumn+" IN ?", principalIDs)
	}
	capture("system.iam_recovery_attempts", "TRUE")
	capture("system.iam_bootstrap_state", "TRUE")
	capture("system.audit_logs", "entity_type = ?", "iam_recovery_attempt")
	return state
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
