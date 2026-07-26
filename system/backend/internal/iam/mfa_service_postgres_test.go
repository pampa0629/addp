package iam

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMFAServiceAgainstPostgres(t *testing.T) {
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
	currentTime := time.Now().UTC().Truncate(time.Second)
	now := func() time.Time { return currentTime }
	identityService := NewIdentityService(repository, now)
	cipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	mfaService, err := NewMFAService(repository, cipher, MFAServiceConfig{}, nil, now)
	if err != nil {
		t.Fatal(err)
	}

	created, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username: "mfa-user", Password: "MFA-runtime-password-2026!", DisplayName: "MFA User",
		Audit: AuditMetadata{RequestID: stringPointer("mfa-create-user")},
	})
	if err != nil {
		t.Fatalf("create MFA user: %v", err)
	}
	const secret = "JBSWY3DPEHPK3PXP"
	ciphertext, nonce, keyVersion, err := cipher.EncryptTOTPSecret(created.PrincipalID, secret)
	if err != nil {
		t.Fatalf("encrypt TOTP secret: %v", err)
	}
	if err := repository.CreateMFACredential(ctx, &MFACredential{
		UserID: created.PrincipalID, Method: "totp", Status: MFACredentialStatusActive,
		SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: keyVersion,
	}); err != nil {
		t.Fatalf("create TOTP credential: %v", err)
	}
	authenticated, err := identityService.AuthenticateLocalAccount(
		ctx, "mfa-user", "MFA-runtime-password-2026!", AuditMetadata{RequestID: stringPointer("mfa-password")},
	)
	if err != nil {
		t.Fatalf("authenticate local account: %v", err)
	}
	challenge, err := mfaService.BeginChallenge(ctx, authenticated, AuditMetadata{RequestID: stringPointer("mfa-challenge")})
	if err != nil {
		t.Fatalf("begin MFA challenge: %v", err)
	}
	code, err := totp.GenerateCode(secret, currentTime)
	if err != nil {
		t.Fatalf("generate current TOTP: %v", err)
	}
	verified, err := mfaService.VerifyChallenge(ctx, VerifyMFAChallengeInput{
		ChallengeToken: challenge.ChallengeToken, Code: code,
		Audit: AuditMetadata{RequestID: stringPointer("mfa-verify")},
	})
	if err != nil {
		t.Fatalf("verify MFA challenge: %v", err)
	}
	if verified.PrincipalID != created.PrincipalID ||
		verified.Authentication.AssuranceLevel != AssuranceLevelAAL2 ||
		strings.Join(verified.Authentication.Methods, ",") != "password,totp" ||
		verified.Authentication.StepUpExpiresAt == nil ||
		!verified.Authentication.StepUpExpiresAt.Equal(currentTime.Add(defaultMFAStepUpTTL)) {
		t.Fatalf("verified MFA authentication = %#v", verified)
	}
	if _, err := mfaService.VerifyChallenge(ctx, VerifyMFAChallengeInput{
		ChallengeToken: challenge.ChallengeToken, Code: code,
	}); err == nil {
		t.Fatal("consumed MFA challenge was accepted again")
	}

	replayChallenge, err := mfaService.BeginChallenge(ctx, authenticated, AuditMetadata{})
	if err != nil {
		t.Fatalf("begin replay challenge: %v", err)
	}
	if _, err := mfaService.VerifyChallenge(ctx, VerifyMFAChallengeInput{
		ChallengeToken: replayChallenge.ChallengeToken, Code: code,
	}); err == nil {
		t.Fatal("same TOTP counter was accepted twice")
	}

	failedChallenge, err := mfaService.BeginChallenge(ctx, authenticated, AuditMetadata{})
	if err != nil {
		t.Fatalf("begin failed-attempt challenge: %v", err)
	}
	for attempt := 0; attempt < maxMFAFailedAttempts; attempt++ {
		if _, err := mfaService.VerifyChallenge(ctx, VerifyMFAChallengeInput{
			ChallengeToken: failedChallenge.ChallengeToken, Code: "000000",
		}); err == nil {
			t.Fatalf("invalid TOTP attempt %d succeeded", attempt+1)
		}
	}
	var failedAttempts int
	var consumedAt *time.Time
	if err := db.Table("system.mfa_challenges").
		Select("failed_attempts, consumed_at").
		Where("token_hash = ?", hashOpaqueToken(failedChallenge.ChallengeToken)).
		Row().Scan(&failedAttempts, &consumedAt); err != nil {
		t.Fatalf("read failed MFA challenge: %v", err)
	}
	if failedAttempts != maxMFAFailedAttempts || consumedAt == nil {
		t.Fatalf("failed MFA challenge attempts=%d consumed_at=%v", failedAttempts, consumedAt)
	}

	var leaked int64
	if err := db.Table("system.audit_logs").
		Where("details::text LIKE ? OR details::text LIKE ?", "%"+secret+"%", "%"+code+"%").
		Count(&leaked).Error; err != nil {
		t.Fatalf("inspect MFA audit leakage: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("MFA audit leaked secret material in %d rows", leaked)
	}
}
