package iam

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBrowserAuthenticationClockAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(12)
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db)
	identity := NewIdentityService(repository, nil)
	fixture := func(name string, lifetime time.Duration) (*TokenFamilyService, resourceTicketInvalidationFixture) {
		t.Helper()
		tokens, err := NewTokenFamilyService(repository, BrowserSessionConfig{
			ResourceTicketOwners: []string{"manager"}, RefreshTokenFamilyTTL: lifetime,
		}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		selection, err := NewContextSelectionService(repository, tokens)
		if err != nil {
			t.Fatal(err)
		}
		now, err := repository.CurrentDatabaseTime(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return tokens, issueResourceTicketInvalidationFixture(t, ctx, identity,
			NewTenantMembershipService(repository, func() time.Time { return now.Add(-time.Second) }),
			selection, db, now, name)
	}

	t.Run("new credential is valid within its issuing transaction", func(t *testing.T) {
		tokens, f := fixture("database-clock-transaction", time.Hour)
		err := repository.Transaction(ctx, func(tx *Repository) error {
			principal, err := tx.LockPrincipal(ctx, f.PrincipalID)
			if err != nil {
				return err
			}
			var transactionStart time.Time
			if err := tx.db.Raw(`SELECT transaction_timestamp()`).Scan(&transactionStart).Error; err != nil {
				return err
			}
			if err := tx.db.Exec(`SELECT pg_sleep(0.03)`).Error; err != nil {
				return err
			}
			session, err := tokens.createBrowserSessionTx(ctx, tx, browserSessionIssueInput{
				Principal: principal,
				Context: ResolvedSessionContext{Type: ContextTypeTenant, TenantID: &f.TenantID,
					TenantMembershipID: &f.TenantMembershipID},
				Authentication: SessionAuthentication{Methods: []string{"password"},
					AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: transactionStart},
			})
			if err != nil {
				return err
			}
			snapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, tx, session.AccessToken)
			if err != nil {
				logCredentialValidationBoundaries(t, err)
				return err
			}
			if !snapshot.CredentialCreatedAt.After(transactionStart) || snapshot.DatabaseTime.Before(snapshot.CredentialCreatedAt) {
				t.Fatal("validation did not use the current database statement time")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("refresh that waits beyond family expiry is rejected without rotation", func(t *testing.T) {
		tokens, f := fixture("database-clock-expiry", time.Second)
		result := make(chan error, 1)
		err := repository.Transaction(ctx, func(tx *Repository) error {
			if _, err := tx.LockPrincipal(ctx, f.PrincipalID); err != nil {
				return err
			}
			go func() {
				_, err := tokens.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{RefreshToken: f.Session.RefreshToken})
				result <- err
			}()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for {
				var waiting int
				if err := db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
					AND wait_event_type='Lock' AND query LIKE '%principals%'`).Scan(&waiting).Error; err != nil {
					return err
				}
				if waiting > 0 {
					break
				}
				select {
				case err := <-result:
					t.Fatalf("refresh did not wait for principal lock: %v", err)
				case <-deadline.C:
					t.Fatal("refresh lock wait was not observed")
				case <-time.After(10 * time.Millisecond):
				}
			}
			return tx.db.Exec(`SELECT pg_sleep(GREATEST(0,
				EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))))`, f.Session.RefreshTokenFamilyExpiresAt.Add(time.Millisecond)).Error
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("expired family was accepted after lock wait: %v", err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		stored := readRefreshTokenByHash(t, db, f.Session.RefreshToken)
		if stored.UsedAt != nil || stored.ReplacedByTokenID != nil {
			t.Fatal("rejected refresh changed immutable rotation facts")
		}
	})

	t.Run("database time failures roll back authentication and session mutations", func(t *testing.T) {
		tokens, f := fixture("database-clock-failure", time.Hour)
		sentinel := errors.New("database clock unavailable")
		brokenTime := func(context.Context, *Repository) (time.Time, error) { return time.Time{}, sentinel }
		brokenTokens, err := NewTokenFamilyService(repository, tokens.config, nil, brokenTime)
		if err != nil {
			t.Fatal(err)
		}
		brokenSelection, err := NewContextSelectionService(repository, brokenTokens)
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewIdentityService(repository, brokenTime).AuthenticateLocalAccount(ctx,
			"resource-ticket-database-clock-failure", "context-selection-password", AuditMetadata{})
		if !errors.Is(err, sentinel) {
			t.Fatalf("authentication time failure lost: %v", err)
		}
		account, err := repository.GetLocalAccountByUserID(ctx, f.PrincipalID)
		if err != nil || account.LastAuthenticatedAt != nil {
			t.Fatalf("failed authentication changed account: %v", err)
		}
		now, err := repository.CurrentDatabaseTime(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = brokenSelection.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID: f.PrincipalID, Authentication: SessionAuthentication{
				Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: now.Add(-time.Minute)},
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("selection time failure lost: %v", err)
		}
		_, err = brokenTokens.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{RefreshToken: f.Session.RefreshToken})
		if !errors.Is(err, sentinel) || readRefreshTokenByHash(t, db, f.Session.RefreshToken).UsedAt != nil {
			t.Fatalf("clock failure did not preserve source refresh: %v", err)
		}
		cipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatal(err)
		}
		const secret = "JBSWY3DPEHPK3PXP"
		ciphertext, nonce, version, err := cipher.EncryptTOTPSecret(f.PrincipalID, secret)
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.CreateMFACredential(ctx, &MFACredential{
			UserID: f.PrincipalID, Method: "totp", Status: MFACredentialStatusActive,
			SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: version,
		}); err != nil {
			t.Fatal(err)
		}
		authenticated, err := identity.AuthenticateLocalAccount(ctx,
			"resource-ticket-database-clock-failure", "context-selection-password", AuditMetadata{})
		if err != nil {
			t.Fatal(err)
		}
		mfa, err := NewMFAService(repository, cipher, MFAServiceConfig{StepUpTTL: 30 * time.Minute}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		challenge, err := mfa.BeginChallenge(ctx, authenticated, AuditMetadata{})
		if err != nil {
			t.Fatal(err)
		}
		now, err = repository.CurrentDatabaseTime(ctx)
		if err != nil {
			t.Fatal(err)
		}
		code, err := totp.GenerateCode(secret, now)
		if err != nil {
			t.Fatal(err)
		}
		verified, err := mfa.VerifyChallenge(ctx, VerifyMFAChallengeInput{ChallengeToken: challenge.ChallengeToken, Code: code})
		if err != nil {
			t.Fatal(err)
		}
		selection, err := NewContextSelectionService(repository, tokens)
		if err != nil {
			t.Fatal(err)
		}
		selected, err := selection.BeginContextSelection(ctx, *verified)
		if err != nil || selected.Session == nil {
			t.Fatalf("database-timed MFA authentication could not issue session: %v", err)
		}
		if _, err := resolveDelegationSourceAccessTokenSnapshot(ctx, repository, selected.Session.AccessToken); err != nil {
			t.Fatal(err)
		}
		if _, err := mfa.VerifyChallenge(ctx, VerifyMFAChallengeInput{ChallengeToken: challenge.ChallengeToken, Code: code}); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("MFA challenge replay was accepted: %v", err)
		}
		brokenMFA, err := NewMFAService(repository, cipher, MFAServiceConfig{}, nil, brokenTime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := brokenMFA.BeginChallenge(ctx, authenticated, AuditMetadata{}); !errors.Is(err, sentinel) {
			t.Fatalf("MFA time failure lost: %v", err)
		}
		brokenMFASession, err := NewMFASessionService(repository, cipher, tokens, MFAServiceConfig{}, nil, brokenTime)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := brokenMFASession.BeginStepUp(ctx, BeginMFAStepUpInput{
			AccessToken: f.Session.AccessToken, RefreshToken: f.Session.RefreshToken,
		}); !errors.Is(err, sentinel) {
			t.Fatalf("MFA session time failure lost: %v", err)
		}
	})
}
