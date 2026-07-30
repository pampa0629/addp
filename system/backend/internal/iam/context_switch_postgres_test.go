package iam

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestContextSwitchServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
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
		t.Fatalf("reset context switch test schema: %v", err)
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
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)

	t.Run("tenant switch is atomic and preserves family deadline", func(t *testing.T) {
		stepUpExpiresAt := currentTime.Add(10 * time.Minute)
		fixture := createContextSwitchFixture(
			t,
			ctx,
			identityService,
			membershipService,
			selectionService,
			"switch-normal",
			"switch-normal-a",
			"switch-normal-b",
			SessionAuthentication{
				Methods:         []string{"password", "totp"},
				AssuranceLevel:  AssuranceLevelAAL2,
				AuthenticatedAt: currentTime.Add(-time.Minute),
				StepUpExpiresAt: &stepUpExpiresAt,
			},
		)
		sourceFamily := readRefreshFamily(t, db, fixture.Session.FamilyID)
		familiesBefore := countContextSelectionRows(t, db, "system.refresh_token_families")
		currentTime = currentTime.Add(time.Second)
		switched, err := switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
			AccessToken:  fixture.Session.AccessToken,
			RefreshToken: fixture.Session.RefreshToken,
			Target: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &fixture.TargetMembershipID,
			},
			Audit: AuditMetadata{RequestID: stringPointer("switch-normal")},
		})
		if err != nil {
			t.Fatalf("switch tenant context: %v", err)
		}
		if switched.FamilyID == fixture.Session.FamilyID || switched.Context.TenantID == nil ||
			*switched.Context.TenantID != fixture.TargetTenantID ||
			switched.Context.TenantMembershipID == nil ||
			*switched.Context.TenantMembershipID != fixture.TargetMembershipID ||
			!switched.RefreshTokenFamilyExpiresAt.Equal(sourceFamily.ExpiresAt) {
			t.Fatalf("switched browser session = %#v", switched)
		}
		replacementFamily := readRefreshFamily(t, db, switched.FamilyID)
		if !reflect.DeepEqual(replacementFamily.AuthenticationMethods, sourceFamily.AuthenticationMethods) ||
			replacementFamily.AssuranceLevel != sourceFamily.AssuranceLevel ||
			!replacementFamily.AuthenticatedAt.Equal(sourceFamily.AuthenticatedAt) ||
			replacementFamily.StepUpExpiresAt == nil || sourceFamily.StepUpExpiresAt == nil ||
			!replacementFamily.StepUpExpiresAt.Equal(*sourceFamily.StepUpExpiresAt) {
			t.Fatalf("replacement authentication facts: source=%#v replacement=%#v", sourceFamily, replacementFamily)
		}
		assertContextSwitchSourceRevoked(t, db, fixture.Session.FamilyID)
		assertIssuedBrowserSession(t, db, switched, fixture.TargetMembershipID, 2)
		assertPlainSessionSecretsAbsent(t, db, switched)
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBefore+1 {
			t.Fatalf("families after context switch = %d, want %d", got, familiesBefore+1)
		}
		assertAuditEventCount(t, db, fixture.Session.FamilyID, "iam.browser_context.switched", AuditRiskMedium, 1)
		assertAuditEventCount(t, db, switched.FamilyID, "iam.browser_session.issued", AuditRiskMedium, 0)

		_, err = switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
			AccessToken:  switched.AccessToken,
			RefreshToken: switched.RefreshToken,
			Target: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &fixture.TargetMembershipID,
			},
			Audit: AuditMetadata{RequestID: stringPointer("switch-same-context")},
		})
		if !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("switch to current context error = %v, want conflict", err)
		}
		if readRefreshFamily(t, db, switched.FamilyID).RevokedAt != nil {
			t.Fatal("same-context request revoked the current family")
		}
	})

	t.Run("step-up and audit failures preserve source family", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("switch-guard")}
		user := createContextSelectionUser(t, ctx, identityService, "switch-guard", audit)
		tenant := createContextSelectionTenant(t, ctx, membershipService, "switch-guard", audit)
		membership := establishContextSelectionMembership(
			t, ctx, membershipService, tenant.ID, user.PrincipalID, audit,
		)
		insertRoleAssignment(
			t,
			db,
			user.PrincipalID,
			"platform.audit_administrator",
			"platform",
			nil,
			nil,
			nil,
			currentTime.Add(-time.Hour),
			nil,
			"bootstrap",
		)
		selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID: user.PrincipalID,
			Authentication: SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
			Audit: audit,
		})
		if err != nil || selection.Session == nil {
			t.Fatalf("issue AAL1 tenant session: result=%#v err=%v", selection, err)
		}
		if selection.Session.Context.TenantMembershipID == nil ||
			*selection.Session.Context.TenantMembershipID != membership.Membership.ID {
			t.Fatalf("AAL1 tenant context = %#v", selection.Session.Context)
		}

		_, err = switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
			AccessToken:  selection.Session.AccessToken,
			RefreshToken: selection.Session.RefreshToken,
			Target:       ContextSelectionChoice{Type: ContextTypePlatform},
			Audit:        AuditMetadata{RequestID: stringPointer("switch-step-up")},
		})
		if !errors.Is(err, ErrStepUpRequired) {
			t.Fatalf("AAL1 platform switch error = %v, want step-up", err)
		}
		if readRefreshFamily(t, db, selection.Session.FamilyID).RevokedAt != nil {
			t.Fatal("step-up failure revoked source family")
		}

		platformAudit := AuditMetadata{RequestID: stringPointer("switch-platform-success")}
		platformUser := createContextSelectionUser(t, ctx, identityService, "switch-platform-success", platformAudit)
		platformTenant := createContextSelectionTenant(t, ctx, membershipService, "switch-platform-success", platformAudit)
		platformMembership := establishContextSelectionMembership(
			t, ctx, membershipService, platformTenant.ID, platformUser.PrincipalID, platformAudit,
		)
		insertRoleAssignment(
			t,
			db,
			platformUser.PrincipalID,
			"platform.audit_administrator",
			"platform",
			nil,
			nil,
			nil,
			currentTime.Add(-time.Hour),
			nil,
			"bootstrap",
		)
		platformSelection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID: platformUser.PrincipalID,
			Authentication: SessionAuthentication{
				Methods:         []string{"password", "totp"},
				AssuranceLevel:  AssuranceLevelAAL2,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
			Audit: platformAudit,
		})
		if err != nil || platformSelection.Challenge == nil {
			t.Fatalf("begin AAL2 platform switch selection: result=%#v err=%v", platformSelection, err)
		}
		platformSource, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
			SelectionTicket: platformSelection.Challenge.SelectionTicket,
			Choice: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &platformMembership.Membership.ID,
			},
			Audit: platformAudit,
		})
		if err != nil {
			t.Fatalf("issue AAL2 tenant source session: %v", err)
		}
		platformSourceFamily := readRefreshFamily(t, db, platformSource.FamilyID)
		platformSwitched, err := switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
			AccessToken:  platformSource.AccessToken,
			RefreshToken: platformSource.RefreshToken,
			Target:       ContextSelectionChoice{Type: ContextTypePlatform},
			Audit:        platformAudit,
		})
		if err != nil {
			t.Fatalf("switch AAL2 tenant session to platform: %v", err)
		}
		if platformSwitched.Context.Type != ContextTypePlatform || platformSwitched.Context.TenantID != nil ||
			!platformSwitched.RefreshTokenFamilyExpiresAt.Equal(platformSourceFamily.ExpiresAt) {
			t.Fatalf("platform-switched session = %#v", platformSwitched)
		}
		assertContextSwitchSourceRevoked(t, db, platformSource.FamilyID)

		fixture := createContextSwitchFixture(
			t,
			ctx,
			identityService,
			membershipService,
			selectionService,
			"switch-audit-rollback",
			"switch-audit-a",
			"switch-audit-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		familiesBeforeRollback := countContextSelectionRows(t, db, "system.refresh_token_families")
		_, err = switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
			AccessToken:  fixture.Session.AccessToken,
			RefreshToken: fixture.Session.RefreshToken,
			Target: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &fixture.TargetMembershipID,
			},
			Audit: AuditMetadata{RequestID: stringPointer(" ")},
		})
		if !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("switch with rejected audit error = %v, want bad request", err)
		}
		if readRefreshFamily(t, db, fixture.Session.FamilyID).RevokedAt != nil {
			t.Fatal("audit rollback left source family revoked")
		}
		assertRefreshTokenStillCurrent(t, db, readRefreshTokenByHash(t, db, fixture.Session.RefreshToken).ID)
		assertAccessTokenStillActive(t, db, readAccessTokenByHash(t, db, fixture.Session.AccessToken).ID)
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBeforeRollback {
			t.Fatalf("families after switch audit rollback = %d, want %d", got, familiesBeforeRollback)
		}
	})

	t.Run("refresh and switch serialize both winner orders", func(t *testing.T) {
		fixture := createContextSwitchFixture(
			t,
			ctx,
			identityService,
			membershipService,
			selectionService,
			"switch-concurrent",
			"switch-concurrent-a",
			"switch-concurrent-b",
			SessionAuthentication{
				Methods:         []string{"password"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime.Add(-time.Minute),
			},
		)
		principalID := readRefreshFamily(t, db, fixture.Session.FamilyID).PrincipalID
		familiesBefore := countContextSelectionRows(t, db, "system.refresh_token_families")

		refreshWinner, switchErr := runRefreshSwitchCompetition(
			t,
			ctx,
			db,
			principalID,
			tokenService,
			switchService,
			fixture.Session,
			fixture.TargetMembershipID,
			true,
		)
		if refreshWinner == nil || !errors.Is(switchErr, ErrBrowserContextSwitchConflict) {
			t.Fatalf("refresh-first competition: refresh=%#v switchErr=%v", refreshWinner, switchErr)
		}
		if readRefreshFamily(t, db, fixture.Session.FamilyID).RevokedAt != nil {
			t.Fatal("refresh-first competition revoked the family")
		}
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBefore {
			t.Fatalf("refresh-first family count = %d, want %d", got, familiesBefore)
		}

		switchWinner, refreshErr := runSwitchRefreshCompetition(
			t,
			ctx,
			db,
			principalID,
			tokenService,
			switchService,
			refreshWinner,
			fixture.TargetMembershipID,
		)
		if switchWinner == nil || !errors.Is(refreshErr, commonapi.ErrUnauthorized) {
			t.Fatalf("switch-first competition: switch=%#v refreshErr=%v", switchWinner, refreshErr)
		}
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBefore+1 {
			t.Fatalf("switch-first family count = %d, want %d", got, familiesBefore+1)
		}
		assertContextSwitchSourceRevoked(t, db, refreshWinner.FamilyID)
		assertAuditEventCount(t, db, refreshWinner.FamilyID, "iam.browser_context.switched", AuditRiskMedium, 1)
		assertAuditEventCount(t, db, refreshWinner.FamilyID, "iam.refresh_token.reuse_detected", AuditRiskHigh, 0)
	})
}

type contextSwitchFixture struct {
	Session            *IssuedBrowserSession
	TargetTenantID     int64
	TargetMembershipID int64
}

func createContextSwitchFixture(
	t *testing.T,
	ctx context.Context,
	identityService *IdentityService,
	membershipService *TenantMembershipService,
	selectionService *ContextSelectionService,
	username string,
	sourceTenantCode string,
	targetTenantCode string,
	authentication SessionAuthentication,
) contextSwitchFixture {
	t.Helper()
	audit := AuditMetadata{RequestID: stringPointer("issue-" + username)}
	user := createContextSelectionUser(t, ctx, identityService, username, audit)
	sourceTenant := createContextSelectionTenant(t, ctx, membershipService, sourceTenantCode, audit)
	targetTenant := createContextSelectionTenant(t, ctx, membershipService, targetTenantCode, audit)
	sourceMembership := establishContextSelectionMembership(
		t, ctx, membershipService, sourceTenant.ID, user.PrincipalID, audit,
	)
	targetMembership := establishContextSelectionMembership(
		t, ctx, membershipService, targetTenant.ID, user.PrincipalID, audit,
	)
	selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID:    user.PrincipalID,
		Authentication: authentication,
		Audit:          audit,
	})
	if err != nil || selection.Challenge == nil {
		t.Fatalf("begin context switch fixture selection: result=%#v err=%v", selection, err)
	}
	session, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
		SelectionTicket: selection.Challenge.SelectionTicket,
		Choice: ContextSelectionChoice{
			Type:               ContextTypeTenant,
			TenantMembershipID: &sourceMembership.Membership.ID,
		},
		Audit: audit,
	})
	if err != nil {
		t.Fatalf("consume context switch fixture selection: %v", err)
	}
	return contextSwitchFixture{
		Session:            session,
		TargetTenantID:     targetTenant.ID,
		TargetMembershipID: targetMembership.Membership.ID,
	}
}

func runRefreshSwitchCompetition(
	t *testing.T,
	ctx context.Context,
	db *gorm.DB,
	principalID int64,
	tokenService *TokenFamilyService,
	switchService *ContextSwitchService,
	session *IssuedBrowserSession,
	targetMembershipID int64,
	refreshFirst bool,
) (*IssuedBrowserSession, error) {
	t.Helper()
	blocker := lockContextSwitchPrincipal(t, db, principalID)
	defer func() { _ = blocker.Rollback().Error }()

	type refreshResult struct {
		session *IssuedBrowserSession
		err     error
	}
	refreshResults := make(chan refreshResult, 1)
	switchResults := make(chan error, 1)
	var waitGroup sync.WaitGroup
	startRefresh := func() {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			rotated, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
				RefreshToken: session.RefreshToken,
				Audit:        AuditMetadata{RequestID: stringPointer("competition-refresh-first")},
			})
			refreshResults <- refreshResult{session: rotated, err: err}
		}()
	}
	startSwitch := func() {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
				AccessToken:  session.AccessToken,
				RefreshToken: session.RefreshToken,
				Target: ContextSelectionChoice{
					Type:               ContextTypeTenant,
					TenantMembershipID: &targetMembershipID,
				},
				Audit: AuditMetadata{RequestID: stringPointer("competition-switch-second")},
			})
			switchResults <- err
		}()
	}
	if refreshFirst {
		startRefresh()
		waitForPrincipalLockWaiters(t, db, 1)
		startSwitch()
	} else {
		startSwitch()
		waitForPrincipalLockWaiters(t, db, 1)
		startRefresh()
	}
	waitForPrincipalLockWaiters(t, db, 2)
	if err := blocker.Commit().Error; err != nil {
		t.Fatalf("release refresh/switch blocker: %v", err)
	}
	waitGroup.Wait()
	refreshResultValue := <-refreshResults
	if refreshResultValue.err != nil {
		t.Fatalf("refresh-first rotation error = %v", refreshResultValue.err)
	}
	return refreshResultValue.session, <-switchResults
}

func runSwitchRefreshCompetition(
	t *testing.T,
	ctx context.Context,
	db *gorm.DB,
	principalID int64,
	tokenService *TokenFamilyService,
	switchService *ContextSwitchService,
	session *IssuedBrowserSession,
	targetMembershipID int64,
) (*IssuedBrowserSession, error) {
	t.Helper()
	blocker := lockContextSwitchPrincipal(t, db, principalID)
	defer func() { _ = blocker.Rollback().Error }()

	type switchResult struct {
		session *IssuedBrowserSession
		err     error
	}
	switchResults := make(chan switchResult, 1)
	refreshResults := make(chan error, 1)
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		switched, err := switchService.SwitchBrowserContext(ctx, SwitchBrowserContextInput{
			AccessToken:  session.AccessToken,
			RefreshToken: session.RefreshToken,
			Target: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &targetMembershipID,
			},
			Audit: AuditMetadata{RequestID: stringPointer("competition-switch-first")},
		})
		switchResults <- switchResult{session: switched, err: err}
	}()
	waitForPrincipalLockWaiters(t, db, 1)
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		_, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
			RefreshToken: session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("competition-refresh-second")},
		})
		refreshResults <- err
	}()
	waitForPrincipalLockWaiters(t, db, 2)
	if err := blocker.Commit().Error; err != nil {
		t.Fatalf("release switch/refresh blocker: %v", err)
	}
	waitGroup.Wait()
	switchResultValue := <-switchResults
	if switchResultValue.err != nil {
		t.Fatalf("switch-first context switch error = %v", switchResultValue.err)
	}
	return switchResultValue.session, <-refreshResults
}

func lockContextSwitchPrincipal(t *testing.T, db *gorm.DB, principalID int64) *gorm.DB {
	t.Helper()
	blocker := db.Begin()
	if blocker.Error != nil {
		t.Fatalf("begin principal blocker: %v", blocker.Error)
	}
	if err := blocker.Exec(`SELECT id FROM system.principals WHERE id = ? FOR UPDATE`, principalID).Error; err != nil {
		_ = blocker.Rollback().Error
		t.Fatalf("lock context switch principal: %v", err)
	}
	return blocker
}

func assertContextSwitchSourceRevoked(t *testing.T, db *gorm.DB, familyID int64) {
	t.Helper()
	family := readRefreshFamily(t, db, familyID)
	if family.RevokedAt == nil || family.RevokedReason == nil || *family.RevokedReason != browserContextSwitchRevocationReason {
		t.Fatalf("context switch source family = %#v", family)
	}
	for _, table := range []string{
		"system.access_tokens",
		"system.refresh_tokens",
		"system.resource_access_tickets",
	} {
		if got := countActiveFamilyRows(t, db, table, familyID); got != 0 {
			t.Fatalf("active %s rows after context switch = %d", table, got)
		}
	}
}
