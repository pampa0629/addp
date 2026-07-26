package iam

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBootstrapServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Truncate(30 * time.Second).Add(30 * time.Second)
	now := func() time.Time { return currentTime }
	identityService := NewIdentityService(repository, now)
	cipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrapService, err := NewBootstrapService(
		repository, identityService, cipher, time.Hour,
		func(prefix string) (string, error) { return prefix + "bootstrap-postgres-test", nil },
		now,
	)
	if err != nil {
		t.Fatal(err)
	}

	secret, expiresAt, err := bootstrapService.Prepare(ctx)
	if err != nil {
		t.Fatalf("prepare bootstrap: %v", err)
	}
	if secret != "addp_bs_bootstrap-postgres-test" || !expiresAt.Equal(currentTime.Add(time.Hour)) {
		t.Fatalf("prepared bootstrap secret=%q expires=%s", secret, expiresAt)
	}
	if _, _, err := bootstrapService.Prepare(ctx); err == nil {
		t.Fatal("bootstrap prepare succeeded twice")
	}

	administrators := []BootstrapAdministratorInput{
		bootstrapAdministratorTestInput(t, "platform.system_administrator", "system-admin", "System Administrator", "JBSWY3DPEHPK3PXP", currentTime),
		bootstrapAdministratorTestInput(t, "platform.security_administrator", "security-admin", "Security Administrator", "KRSXG5DSNFXGOIDB", currentTime),
		bootstrapAdministratorTestInput(t, "platform.audit_administrator", "audit-admin", "Audit Administrator", "MFRGGZDFMZTWQ2LK", currentTime),
	}
	result, err := bootstrapService.Apply(ctx, BootstrapApplyInput{
		BootstrapSecret: secret, Administrators: administrators,
	})
	if err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	if len(result.PrincipalIDs) != 3 || !result.CompletedAt.Equal(currentTime) {
		t.Fatalf("bootstrap result = %#v", result)
	}
	if _, err := bootstrapService.Apply(ctx, BootstrapApplyInput{
		BootstrapSecret: secret, Administrators: administrators,
	}); err == nil {
		t.Fatal("completed bootstrap applied twice")
	}

	for table, want := range map[string]int64{
		"system.principals": 3, "system.users": 3, "system.local_accounts": 3,
		"system.mfa_credentials": 3, "system.role_assignments": 3,
	} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != want {
			t.Fatalf("%s count=%d want=%d", table, count, want)
		}
	}
	var completedStatus string
	var storedSecret *string
	if err := db.Table("system.iam_bootstrap_state").
		Select("status, secret_hash").Row().Scan(&completedStatus, &storedSecret); err != nil {
		t.Fatalf("read bootstrap state: %v", err)
	}
	if completedStatus != "completed" || storedSecret != nil {
		t.Fatalf("bootstrap state status=%q secret=%v", completedStatus, storedSecret)
	}
	var roleCount int64
	if err := db.Table("system.role_assignments assignment").
		Joins("JOIN system.roles role ON role.id = assignment.role_id").
		Where("assignment.source_type = ? AND assignment.scope_type = ? AND role.role_key IN ?",
			"bootstrap", "platform", bootstrapRoleOrder).
		Count(&roleCount).Error; err != nil {
		t.Fatalf("count bootstrap administrator roles: %v", err)
	}
	if roleCount != 3 {
		t.Fatalf("bootstrap administrator role count=%d", roleCount)
	}
	var leakedAuditCount int64
	if err := db.Table("system.audit_logs").
		Where("details::text LIKE ? OR details::text LIKE ?", "%JBSWY3DPEHPK3PXP%", "%Bootstrap-password%").
		Count(&leakedAuditCount).Error; err != nil {
		t.Fatalf("inspect bootstrap audit leakage: %v", err)
	}
	if leakedAuditCount != 0 {
		t.Fatalf("bootstrap audit leaked secret material in %d rows", leakedAuditCount)
	}
}

func bootstrapAdministratorTestInput(
	t *testing.T,
	roleKey string,
	username string,
	displayName string,
	secret string,
	now time.Time,
) BootstrapAdministratorInput {
	t.Helper()
	firstAt := now.Add(-30 * time.Second)
	firstCode, err := totp.GenerateCode(secret, firstAt)
	if err != nil {
		t.Fatalf("generate first bootstrap TOTP: %v", err)
	}
	secondCode, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatalf("generate second bootstrap TOTP: %v", err)
	}
	email := username + "@example.test"
	locale := "zh-cn"
	return BootstrapAdministratorInput{
		RoleKey: roleKey, Username: username,
		Password: "Bootstrap-password-" + username + "!", DisplayName: displayName,
		PrimaryEmail: &email, Locale: &locale, TOTPSecret: secret,
		TOTPProofs: []BootstrapTOTPProof{
			{Code: firstCode, VerifiedAt: firstAt},
			{Code: secondCode, VerifiedAt: now},
		},
	}
}
