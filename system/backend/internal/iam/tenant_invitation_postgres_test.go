package iam

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTenantInvitationServiceAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset IAM invitation test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ContextSelectionTicketTTL: 5 * time.Minute,
		AccessTokenTTL:            15 * time.Minute,
		RefreshTokenFamilyTTL:     30 * 24 * time.Hour,
		ResourceAccessTicketTTL:   15 * time.Minute,
		ResourceTicketOwners:      []string{"manager"},
	}, nil, now)
	if err != nil {
		t.Fatalf("create token family service: %v", err)
	}
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)
	tenantService := NewPlatformTenantService(repository, now)
	invitationService, err := NewTenantInvitationService(repository, identityService, tokenService, TenantInvitationServiceConfig{
		InvitationTTL: 7 * 24 * time.Hour, EnrollmentTicketTTL: 5 * time.Minute, Now: now,
	})
	if err != nil {
		t.Fatalf("create tenant invitation service: %v", err)
	}
	audit := AuditMetadata{RequestID: stringPointer("tenant-invitation-e2e")}
	creatorEmail := "creator@example.test"
	creator, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username: "invitation-creator", Password: "creator-password", DisplayName: "Invitation Creator",
		PrimaryEmail: &creatorEmail, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create invitation creator: %v", err)
	}
	tenantA, err := tenantService.Create(ctx, CreateTenantInput{Code: "invitation-a", Name: "Invitation A", Audit: audit})
	if err != nil {
		t.Fatalf("create invitation tenant A: %v", err)
	}
	tenantB, err := tenantService.Create(ctx, CreateTenantInput{Code: "invitation-b", Name: "Invitation B", Audit: audit})
	if err != nil {
		t.Fatalf("create invitation tenant B: %v", err)
	}

	registered := createInvitationForTest(t, ctx, invitationService, tenantA.ID, creator.PrincipalID, "new.user@example.test", audit)
	if registered.Invitation.SecretHash == registered.Secret || !strings.HasPrefix(registered.Secret, "addp_ti_") {
		t.Fatalf("invitation secret storage = %#v", registered)
	}
	registration, err := invitationService.Register(ctx, RegisterTenantInvitationInput{
		InvitationSecret: registered.Secret, Username: "new-user", Password: "new-user-password",
		DisplayName: "New User", Audit: audit,
	})
	if err != nil {
		t.Fatalf("register with tenant invitation: %v", err)
	}
	assertAcceptedInvitationResult(t, registration, tenantA.ID)
	if _, err := invitationService.Register(ctx, RegisterTenantInvitationInput{
		InvitationSecret: registered.Secret, Username: "replay-user", Password: "replay-password",
		DisplayName: "Replay User", Audit: audit,
	}); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("registration replay error = %v, want unauthorized", err)
	}

	enrollmentEmail := "enrollment.user@example.test"
	enrollmentUser, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username: "enrollment-user", Password: "enrollment-password", DisplayName: "Enrollment User",
		PrimaryEmail: &enrollmentEmail, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create enrollment user: %v", err)
	}
	enrollmentInvitation := createInvitationForTest(t, ctx, invitationService, tenantA.ID, creator.PrincipalID, enrollmentEmail, audit)
	issuedTicket, err := invitationService.IssueEnrollmentTicket(ctx, IssueEnrollmentTicketInput{
		InvitationSecret: enrollmentInvitation.Secret, Username: "enrollment-user",
		Password: "enrollment-password", Audit: audit,
	})
	if err != nil || !strings.HasPrefix(issuedTicket.EnrollmentTicket, "addp_et_") {
		t.Fatalf("issued enrollment ticket = %#v err=%v", issuedTicket, err)
	}
	enrollmentAccepted, err := invitationService.Accept(ctx, AcceptTenantInvitationInput{
		InvitationSecret: enrollmentInvitation.Secret, EnrollmentTicket: issuedTicket.EnrollmentTicket, Audit: audit,
	})
	if err != nil {
		t.Fatalf("accept invitation with enrollment ticket: %v", err)
	}
	assertAcceptedInvitationResult(t, enrollmentAccepted, tenantA.ID)
	if enrollmentAccepted.Membership.PrincipalID != enrollmentUser.PrincipalID {
		t.Fatalf("enrollment membership principal = %d, want %d", enrollmentAccepted.Membership.PrincipalID, enrollmentUser.PrincipalID)
	}
	if _, err := invitationService.Accept(ctx, AcceptTenantInvitationInput{
		InvitationSecret: enrollmentInvitation.Secret, EnrollmentTicket: issuedTicket.EnrollmentTicket, Audit: audit,
	}); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("enrollment replay error = %v, want unauthorized", err)
	}

	browserEmail := "browser.user@example.test"
	browserUser, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username: "browser-user", Password: "browser-password", DisplayName: "Browser User",
		PrimaryEmail: &browserEmail, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create browser user: %v", err)
	}
	if _, err := membershipService.EstablishMembership(ctx, EstablishTenantMembershipInput{
		TenantID: tenantA.ID, PrincipalID: browserUser.PrincipalID, SourceType: TenantMembershipSourceManual,
		CreatedByPrincipalID: &creator.PrincipalID, Audit: audit,
	}); err != nil {
		t.Fatalf("establish browser user's first context: %v", err)
	}
	principal, err := repository.GetPrincipal(ctx, browserUser.PrincipalID)
	if err != nil {
		t.Fatalf("read browser principal: %v", err)
	}
	firstMembership, err := repository.LockTenantMembership(ctx, tenantA.ID, browserUser.PrincipalID)
	if err != nil {
		t.Fatalf("read browser membership: %v", err)
	}
	authentication := SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: currentTime}
	var oldSession *IssuedBrowserSession
	if err := repository.Transaction(ctx, func(tx *Repository) error {
		lockedPrincipal, err := tx.LockPrincipal(ctx, principal.ID)
		if err != nil {
			return err
		}
		resolved, err := resolveTenantSessionContext(ctx, tx, principal.ID, firstMembership.ID, currentTime)
		if err != nil {
			return err
		}
		oldSession, err = tokenService.issueBrowserSessionTx(ctx, tx, browserSessionIssueInput{
			Principal: lockedPrincipal, Context: resolved, Authentication: authentication,
			Mode: BrowserSessionIssueModeDirect, Audit: audit,
		})
		return err
	}); err != nil {
		t.Fatalf("issue old browser session: %v", err)
	}
	browserInvitation := createInvitationForTest(t, ctx, invitationService, tenantB.ID, creator.PrincipalID, browserEmail, audit)
	browserAccepted, err := invitationService.Accept(ctx, AcceptTenantInvitationInput{
		InvitationSecret: browserInvitation.Secret, PrincipalID: browserUser.PrincipalID,
		Authentication: authentication, Audit: audit,
	})
	if err != nil {
		t.Fatalf("accept invitation with browser session facts: %v", err)
	}
	assertAcceptedInvitationResult(t, browserAccepted, tenantB.ID)
	var oldRevokedAt *time.Time
	if err := db.Raw(`SELECT revoked_at FROM system.refresh_token_families WHERE id = ?`, oldSession.FamilyID).Scan(&oldRevokedAt).Error; err != nil || oldRevokedAt == nil {
		t.Fatalf("old browser family revoked_at = %v err=%v", oldRevokedAt, err)
	}

	expiredInvitation := createInvitationForTest(t, ctx, invitationService, tenantB.ID, creator.PrincipalID, "expired@example.test", audit)
	currentTime = currentTime.Add(8 * 24 * time.Hour)
	if _, err := invitationService.Register(ctx, RegisterTenantInvitationInput{
		InvitationSecret: expiredInvitation.Secret, Username: "expired-user", Password: "expired-password",
		DisplayName: "Expired User", Audit: audit,
	}); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("expired registration error = %v, want unauthorized", err)
	}
	var expiredStatus TenantInvitationStatus
	if err := db.Raw(`SELECT status FROM system.tenant_invitations WHERE id = ?`, expiredInvitation.Invitation.ID).Scan(&expiredStatus).Error; err != nil || expiredStatus != TenantInvitationStatusExpired {
		t.Fatalf("expired invitation status = %q err=%v", expiredStatus, err)
	}

	endedEmail := "ended.user@example.test"
	endedUser, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username: "ended-user", Password: "ended-password", DisplayName: "Ended User",
		PrimaryEmail: &endedEmail, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create ended membership user: %v", err)
	}
	if _, err := membershipService.EstablishMembership(ctx, EstablishTenantMembershipInput{
		TenantID: tenantB.ID, PrincipalID: endedUser.PrincipalID, SourceType: TenantMembershipSourceManual,
		CreatedByPrincipalID: &creator.PrincipalID, Audit: audit,
	}); err != nil {
		t.Fatalf("establish ended membership: %v", err)
	}
	if _, err := membershipService.EndMembership(ctx, ChangeTenantMembershipInput{
		TenantID: tenantB.ID, PrincipalID: endedUser.PrincipalID, Reason: "ended before invite", Audit: audit,
	}); err != nil {
		t.Fatalf("end membership before invite: %v", err)
	}
	endedInvitation := createInvitationForTest(t, ctx, invitationService, tenantB.ID, creator.PrincipalID, endedEmail, audit)
	if _, err := invitationService.Accept(ctx, AcceptTenantInvitationInput{
		InvitationSecret: endedInvitation.Secret, PrincipalID: endedUser.PrincipalID,
		Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: currentTime},
		Audit:          audit,
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("ended membership invitation acceptance error = %v, want conflict", err)
	}
	var endedInvitationStatus TenantInvitationStatus
	if err := db.Raw(`SELECT status FROM system.tenant_invitations WHERE id = ?`, endedInvitation.Invitation.ID).Scan(&endedInvitationStatus).Error; err != nil || endedInvitationStatus != TenantInvitationStatusPending {
		t.Fatalf("ended membership invitation status = %q err=%v", endedInvitationStatus, err)
	}
}

func createInvitationForTest(
	t *testing.T,
	ctx context.Context,
	service *TenantInvitationService,
	tenantID int64,
	creatorPrincipalID int64,
	email string,
	audit AuditMetadata,
) *CreatedTenantInvitation {
	t.Helper()
	created, err := service.Create(ctx, CreateTenantInvitationInput{
		TenantID: tenantID, Email: email, CreatedByPrincipalID: creatorPrincipalID, Audit: audit,
	})
	if err != nil {
		t.Fatalf("create invitation for %s: %v", email, err)
	}
	return created
}

func assertAcceptedInvitationResult(t *testing.T, accepted *AcceptedTenantInvitation, tenantID int64) {
	t.Helper()
	if accepted == nil || accepted.Invitation.Status != TenantInvitationStatusAccepted ||
		accepted.Membership.TenantID != tenantID || accepted.Membership.SourceType != TenantMembershipSourceInvitation ||
		accepted.Session.Context.Type != ContextTypeTenant || accepted.Session.Context.TenantID == nil ||
		*accepted.Session.Context.TenantID != tenantID || !strings.HasPrefix(accepted.Session.AccessToken, "addp_at_") ||
		!strings.HasPrefix(accepted.Session.RefreshToken, "addp_rt_") {
		t.Fatalf("accepted invitation result = %#v", accepted)
	}
}
