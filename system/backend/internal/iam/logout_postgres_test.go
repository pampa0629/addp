package iam

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLogoutServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}

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
		t.Fatalf("reset logout test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"manager", "standard"},
	}, nil, now)
	if err != nil {
		t.Fatalf("create TokenFamilyService: %v", err)
	}
	selectionService, err := NewContextSelectionService(repository, tokenService)
	if err != nil {
		t.Fatalf("create ContextSelectionService: %v", err)
	}
	switchService, err := NewContextSwitchService(repository, tokenService)
	if err != nil {
		t.Fatalf("create ContextSwitchService: %v", err)
	}
	logoutService, err := NewLogoutService(repository, tokenService)
	if err != nil {
		t.Fatalf("create LogoutService: %v", err)
	}
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)

	t.Run("logout revokes the complete family exactly once", func(t *testing.T) {
		fixture := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-normal", "logout-normal-a", "logout-normal-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		accessToken := readAccessTokenByHash(t, db, fixture.Session.AccessToken)
		delegatedID := insertDelegatedToken(t, db, accessToken, currentTime, "logout-normal")

		currentTime = currentTime.Add(time.Second)
		err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
			AccessToken:  fixture.Session.AccessToken,
			RefreshToken: fixture.Session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("logout-normal")},
		})
		if err != nil {
			t.Fatalf("logout browser session: %v", err)
		}
		assertLogoutFamilyRevoked(t, db, fixture.Session.FamilyID, delegatedID)
		assertAuditEventCount(
			t, db, fixture.Session.FamilyID, "iam.browser_session.logged_out", AuditRiskMedium, 1,
		)

		err = logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
			AccessToken:  fixture.Session.AccessToken,
			RefreshToken: fixture.Session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("logout-repeat")},
		})
		if !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("repeated logout error = %v, want unauthorized", err)
		}
		assertAuditEventCount(
			t, db, fixture.Session.FamilyID, "iam.browser_session.logged_out", AuditRiskMedium, 1,
		)
	})

	t.Run("logout requires the current access and refresh pair", func(t *testing.T) {
		first := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-pair-a", "logout-pair-a1", "logout-pair-a2",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		second := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-pair-b", "logout-pair-b1", "logout-pair-b2",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)

		err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
			AccessToken:  first.Session.AccessToken,
			RefreshToken: second.Session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("logout-cross-family")},
		})
		if !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("cross-family logout error = %v, want unauthorized", err)
		}
		if readRefreshFamily(t, db, first.Session.FamilyID).RevokedAt != nil ||
			readRefreshFamily(t, db, second.Session.FamilyID).RevokedAt != nil {
			t.Fatal("cross-family logout revoked a family")
		}

		err = logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
			AccessToken:  "invalid",
			RefreshToken: second.Session.RefreshToken,
		})
		if !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("malformed logout token error = %v, want unauthorized", err)
		}
	})

	t.Run("audit failure rolls back family revocation", func(t *testing.T) {
		fixture := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-audit", "logout-audit-a", "logout-audit-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		accessToken := readAccessTokenByHash(t, db, fixture.Session.AccessToken)
		delegatedID := insertDelegatedToken(t, db, accessToken, currentTime, "logout-audit")

		err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
			AccessToken:  fixture.Session.AccessToken,
			RefreshToken: fixture.Session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer(" ")},
		})
		if !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("logout with rejected audit error = %v, want bad request", err)
		}
		if readRefreshFamily(t, db, fixture.Session.FamilyID).RevokedAt != nil {
			t.Fatal("audit rollback left family revoked")
		}
		assertAccessTokenStillActive(t, db, accessToken.ID)
		assertRefreshTokenStillCurrent(t, db, readRefreshTokenByHash(t, db, fixture.Session.RefreshToken).ID)
		assertDelegatedTokenActive(t, db, delegatedID)
		if got := countActiveFamilyRows(t, db, "system.resource_access_tickets", fixture.Session.FamilyID); got != 2 {
			t.Fatalf("active resource tickets after audit rollback = %d, want 2", got)
		}
	})

	t.Run("logout and refresh serialize both winner orders", func(t *testing.T) {
		refreshFirst := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-refresh-first", "logout-refresh-first-a", "logout-refresh-first-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		principalID := readRefreshFamily(t, db, refreshFirst.Session.FamilyID).PrincipalID
		firstResult, secondResult := runLogoutCompetition(
			t, db, principalID,
			func() logoutCompetitionResult {
				session, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
					RefreshToken: refreshFirst.Session.RefreshToken,
					Audit:        AuditMetadata{RequestID: stringPointer("logout-refresh-winner")},
				})
				return logoutCompetitionResult{session: session, err: err}
			},
			func() logoutCompetitionResult {
				err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
					AccessToken:  refreshFirst.Session.AccessToken,
					RefreshToken: refreshFirst.Session.RefreshToken,
					Audit:        AuditMetadata{RequestID: stringPointer("logout-refresh-loser")},
				})
				return logoutCompetitionResult{err: err}
			},
		)
		if firstResult.err != nil || firstResult.session == nil ||
			!errors.Is(secondResult.err, commonapi.ErrUnauthorized) {
			t.Fatalf("refresh-first results: first=%#v second=%#v", firstResult, secondResult)
		}
		if readRefreshFamily(t, db, refreshFirst.Session.FamilyID).RevokedAt != nil {
			t.Fatal("refresh-first logout competition revoked the family")
		}

		logoutFirst := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-refresh-logout-first", "logout-refresh-logout-first-a", "logout-refresh-logout-first-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		principalID = readRefreshFamily(t, db, logoutFirst.Session.FamilyID).PrincipalID
		firstResult, secondResult = runLogoutCompetition(
			t, db, principalID,
			func() logoutCompetitionResult {
				err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
					AccessToken:  logoutFirst.Session.AccessToken,
					RefreshToken: logoutFirst.Session.RefreshToken,
					Audit:        AuditMetadata{RequestID: stringPointer("logout-first")},
				})
				return logoutCompetitionResult{err: err}
			},
			func() logoutCompetitionResult {
				session, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
					RefreshToken: logoutFirst.Session.RefreshToken,
					Audit:        AuditMetadata{RequestID: stringPointer("refresh-after-logout")},
				})
				return logoutCompetitionResult{session: session, err: err}
			},
		)
		if firstResult.err != nil || !errors.Is(secondResult.err, commonapi.ErrUnauthorized) {
			t.Fatalf("logout-first refresh results: first=%#v second=%#v", firstResult, secondResult)
		}
		assertLogoutFamilyRevoked(t, db, logoutFirst.Session.FamilyID, 0)
		assertAuditEventCount(
			t, db, logoutFirst.Session.FamilyID, "iam.refresh_token.reuse_detected", AuditRiskHigh, 0,
		)
	})

	t.Run("logout and context switch serialize both winner orders", func(t *testing.T) {
		switchFirst := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-switch-first", "logout-switch-first-a", "logout-switch-first-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		principalID := readRefreshFamily(t, db, switchFirst.Session.FamilyID).PrincipalID
		familiesBefore := countContextSelectionRows(t, db, "system.refresh_token_families")
		firstResult, secondResult := runLogoutCompetition(
			t, db, principalID,
			func() logoutCompetitionResult {
				session, err := switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
					AccessToken:  switchFirst.Session.AccessToken,
					RefreshToken: switchFirst.Session.RefreshToken,
					Target: ContextSelectionChoice{
						Type:               ContextTypeTenant,
						TenantMembershipID: &switchFirst.TargetMembershipID,
					},
					Audit: AuditMetadata{RequestID: stringPointer("switch-before-logout")},
				})
				return logoutCompetitionResult{session: session, err: err}
			},
			func() logoutCompetitionResult {
				err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
					AccessToken:  switchFirst.Session.AccessToken,
					RefreshToken: switchFirst.Session.RefreshToken,
					Audit:        AuditMetadata{RequestID: stringPointer("logout-after-switch")},
				})
				return logoutCompetitionResult{err: err}
			},
		)
		if firstResult.err != nil || firstResult.session == nil ||
			!errors.Is(secondResult.err, commonapi.ErrUnauthorized) {
			t.Fatalf("switch-first results: first=%#v second=%#v", firstResult, secondResult)
		}
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBefore+1 {
			t.Fatalf("switch-first family count = %d, want %d", got, familiesBefore+1)
		}

		logoutFirst := createContextSwitchFixture(
			t, ctx, identityService, membershipService, selectionService,
			"logout-switch-logout-first", "logout-switch-logout-first-a", "logout-switch-logout-first-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		principalID = readRefreshFamily(t, db, logoutFirst.Session.FamilyID).PrincipalID
		familiesBefore = countContextSelectionRows(t, db, "system.refresh_token_families")
		firstResult, secondResult = runLogoutCompetition(
			t, db, principalID,
			func() logoutCompetitionResult {
				err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
					AccessToken:  logoutFirst.Session.AccessToken,
					RefreshToken: logoutFirst.Session.RefreshToken,
					Audit:        AuditMetadata{RequestID: stringPointer("logout-before-switch")},
				})
				return logoutCompetitionResult{err: err}
			},
			func() logoutCompetitionResult {
				session, err := switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
					AccessToken:  logoutFirst.Session.AccessToken,
					RefreshToken: logoutFirst.Session.RefreshToken,
					Target: ContextSelectionChoice{
						Type:               ContextTypeTenant,
						TenantMembershipID: &logoutFirst.TargetMembershipID,
					},
					Audit: AuditMetadata{RequestID: stringPointer("switch-after-logout")},
				})
				return logoutCompetitionResult{session: session, err: err}
			},
		)
		if firstResult.err != nil || !errors.Is(secondResult.err, commonapi.ErrUnauthorized) {
			t.Fatalf("logout-first switch results: first=%#v second=%#v", firstResult, secondResult)
		}
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBefore {
			t.Fatalf("logout-first family count = %d, want %d", got, familiesBefore)
		}
		assertLogoutFamilyRevoked(t, db, logoutFirst.Session.FamilyID, 0)
	})
}

type logoutCompetitionResult struct {
	session *IssuedBrowserSession
	err     error
}

func runLogoutCompetition(
	t *testing.T,
	db *gorm.DB,
	principalID int64,
	first func() logoutCompetitionResult,
	second func() logoutCompetitionResult,
) (logoutCompetitionResult, logoutCompetitionResult) {
	t.Helper()
	blocker := lockContextSwitchPrincipal(t, db, principalID)
	defer func() { _ = blocker.Rollback().Error }()

	firstResults := make(chan logoutCompetitionResult, 1)
	secondResults := make(chan logoutCompetitionResult, 1)
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		firstResults <- first()
	}()
	waitForPrincipalLockWaiters(t, db, 1)
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		secondResults <- second()
	}()
	waitForPrincipalLockWaiters(t, db, 2)
	if err := blocker.Commit().Error; err != nil {
		t.Fatalf("release logout competition blocker: %v", err)
	}
	waitGroup.Wait()
	return <-firstResults, <-secondResults
}

func assertLogoutFamilyRevoked(t *testing.T, db *gorm.DB, familyID int64, delegatedID int64) {
	t.Helper()
	family := readRefreshFamily(t, db, familyID)
	if family.RevokedAt == nil || family.RevokedReason == nil || *family.RevokedReason != browserLogoutRevocationReason {
		t.Fatalf("logged-out family = %#v", family)
	}
	for _, table := range []string{
		"system.access_tokens",
		"system.refresh_tokens",
		"system.resource_access_tickets",
	} {
		if got := countActiveFamilyRows(t, db, table, familyID); got != 0 {
			t.Fatalf("active %s rows after logout = %d", table, got)
		}
	}
	if delegatedID != 0 {
		assertDelegatedTokenRevoked(t, db, delegatedID)
	}
}
