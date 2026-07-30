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

func TestMFASessionClosureAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset MFA session test schema: %v", err)
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
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{ResourceTicketOwners: []string{"system"}}, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewMFASessionService(repository, cipher, tokenService, MFAServiceConfig{}, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	roleService := NewTenantRoleService(repository, now)

	created, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username: "mfa-session-user", Password: "MFA-session-password-2026!", DisplayName: "MFA Session User",
		Audit: AuditMetadata{RequestID: stringPointer("mfa-session-user")},
	})
	if err != nil {
		t.Fatalf("create MFA session user: %v", err)
	}
	tenant := &Tenant{Code: "mfa-session", Name: "MFA Session", Status: TenantStatusActive}
	if err := db.Create(tenant).Error; err != nil {
		t.Fatalf("create MFA session tenant: %v", err)
	}
	membership := &TenantMembership{
		TenantID: tenant.ID, PrincipalID: created.PrincipalID, Status: TenantMembershipStatusActive,
		SourceType: TenantMembershipSourceManual, JoinedAt: currentTime, CreatedByPrincipalID: &created.PrincipalID,
	}
	if err := db.Create(membership).Error; err != nil {
		t.Fatalf("create MFA session membership: %v", err)
	}
	principal, err := repository.GetPrincipal(ctx, created.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	var initial *IssuedBrowserSession
	err = repository.Transaction(ctx, func(tx *Repository) error {
		var issueErr error
		tenantID := tenant.ID
		membershipID := membership.ID
		initial, issueErr = tokenService.createBrowserSessionTx(ctx, tx, browserSessionIssueInput{
			Principal:      principal,
			Context:        ResolvedSessionContext{Type: ContextTypeTenant, TenantID: &tenantID, TenantMembershipID: &membershipID},
			Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: currentTime},
			Mode:           BrowserSessionIssueModeDirect,
		})
		return issueErr
	})
	if err != nil {
		t.Fatalf("issue initial browser session: %v", err)
	}

	roles, err := roleService.ListRoles(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	var infrastructureRoleID int64
	for _, role := range roles {
		if role.RoleKey == "tenant.infrastructure_administrator" {
			infrastructureRoleID = role.ID
		}
	}
	if infrastructureRoleID == 0 {
		t.Fatal("infrastructure administrator role is missing")
	}
	_, err = roleService.CreateAssignment(ctx, CreateTenantRoleAssignmentInput{
		TenantID: tenant.ID, MembershipID: membership.ID, RoleID: infrastructureRoleID,
		ScopeType: "tenant", ActorPrincipalID: created.PrincipalID, AssuranceLevel: AssuranceLevelAAL1,
	})
	if !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("AAL1 high-risk self assignment error = %v, want step-up required", err)
	}

	if _, err := service.BeginEnrollment(ctx, BeginMFAEnrollmentInput{
		AccessToken: initial.AccessToken, RefreshToken: initial.RefreshToken, CurrentPassword: "wrong-password",
	}); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("invalid password enrollment error = %v", err)
	}
	enrollment, err := service.BeginEnrollment(ctx, BeginMFAEnrollmentInput{
		AccessToken: initial.AccessToken, RefreshToken: initial.RefreshToken,
		CurrentPassword: "MFA-session-password-2026!", Audit: AuditMetadata{RequestID: stringPointer("mfa-enrollment")},
	})
	if err != nil {
		t.Fatalf("begin enrollment: %v", err)
	}
	code, err := totp.GenerateCode(enrollment.Secret, currentTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteEnrollment(ctx, CompleteMFAEnrollmentInput{
		AccessToken: initial.AccessToken, RefreshToken: initial.RefreshToken,
		EnrollmentToken: enrollment.EnrollmentToken, Code: "000000",
	}); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("invalid enrollment code error = %v", err)
	}
	enrolledSession, err := service.CompleteEnrollment(ctx, CompleteMFAEnrollmentInput{
		AccessToken: initial.AccessToken, RefreshToken: initial.RefreshToken,
		EnrollmentToken: enrollment.EnrollmentToken, Code: code,
		Audit: AuditMetadata{RequestID: stringPointer("mfa-enrollment-complete")},
	})
	if err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}
	assertMFAReplacementFamily(t, db, initial.FamilyID, enrolledSession.FamilyID, mfaEnrollmentRevocationReason)
	if _, err := service.CompleteEnrollment(ctx, CompleteMFAEnrollmentInput{
		AccessToken: initial.AccessToken, RefreshToken: initial.RefreshToken,
		EnrollmentToken: enrollment.EnrollmentToken, Code: code,
	}); err == nil {
		t.Fatal("consumed enrollment succeeded again")
	}

	currentTime = currentTime.Add(30 * time.Second)
	challenge, err := service.BeginStepUp(ctx, BeginMFAStepUpInput{
		AccessToken: enrolledSession.AccessToken, RefreshToken: enrolledSession.RefreshToken,
		Audit: AuditMetadata{RequestID: stringPointer("mfa-step-up")},
	})
	if err != nil {
		t.Fatalf("begin step-up: %v", err)
	}
	stepUpCode, err := totp.GenerateCode(enrollment.Secret, currentTime)
	if err != nil {
		t.Fatal(err)
	}
	steppedUpSession, err := service.CompleteStepUp(ctx, CompleteMFAStepUpInput{
		AccessToken: enrolledSession.AccessToken, RefreshToken: enrolledSession.RefreshToken,
		ChallengeToken: challenge.ChallengeToken, Code: stepUpCode,
		Audit: AuditMetadata{RequestID: stringPointer("mfa-step-up-complete")},
	})
	if err != nil {
		t.Fatalf("complete step-up: %v", err)
	}
	assertMFAReplacementFamily(t, db, enrolledSession.FamilyID, steppedUpSession.FamilyID, mfaStepUpRevocationReason)
	stepUpExpiresAt := currentTime.Add(defaultMFAStepUpTTL)
	assignment, err := roleService.CreateAssignment(ctx, CreateTenantRoleAssignmentInput{
		TenantID: tenant.ID, MembershipID: membership.ID, RoleID: infrastructureRoleID,
		ScopeType: "tenant", ActorPrincipalID: created.PrincipalID, AssuranceLevel: AssuranceLevelAAL2,
		StepUpExpiresAt: &stepUpExpiresAt,
	})
	if err != nil || assignment.RoleKey != "tenant.infrastructure_administrator" {
		t.Fatalf("AAL2 high-risk self assignment = %#v err=%v", assignment, err)
	}
	refreshedSession, err := tokenService.RotateBrowserRefreshToken(ctx, RotateBrowserRefreshTokenInput{
		RefreshToken: steppedUpSession.RefreshToken,
		Audit:        AuditMetadata{RequestID: stringPointer("mfa-step-up-authorization-refresh")},
	})
	if err != nil {
		t.Fatalf("refresh authorization after self assignment: %v", err)
	}

	failedChallenge, err := service.BeginStepUp(ctx, BeginMFAStepUpInput{
		AccessToken: refreshedSession.AccessToken, RefreshToken: refreshedSession.RefreshToken,
		Audit: AuditMetadata{RequestID: stringPointer("mfa-step-up-failed")},
	})
	if err != nil {
		t.Fatalf("begin failed step-up: %v", err)
	}
	for attempt := 1; attempt <= maxMFAFailedAttempts; attempt++ {
		if _, err := service.CompleteStepUp(ctx, CompleteMFAStepUpInput{
			AccessToken: refreshedSession.AccessToken, RefreshToken: refreshedSession.RefreshToken,
			ChallengeToken: failedChallenge.ChallengeToken, Code: stepUpCode,
			Audit: AuditMetadata{RequestID: stringPointer("mfa-step-up-failed")},
		}); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("failed step-up attempt %d error = %v", attempt, err)
		}
	}
	var failedChallengeRow MFAChallenge
	if err := db.Where("token_hash = ?", hashOpaqueToken(failedChallenge.ChallengeToken)).First(&failedChallengeRow).Error; err != nil {
		t.Fatalf("read failed step-up challenge: %v", err)
	}
	if failedChallengeRow.FailedAttempts != maxMFAFailedAttempts || failedChallengeRow.ConsumedAt == nil {
		t.Fatalf("failed step-up challenge attempts=%d consumed_at=%v", failedChallengeRow.FailedAttempts, failedChallengeRow.ConsumedAt)
	}
	var revokedFamily RefreshTokenFamily
	if err := db.First(&revokedFamily, steppedUpSession.FamilyID).Error; err != nil {
		t.Fatalf("read failed step-up source family: %v", err)
	}
	if revokedFamily.RevokedAt == nil || revokedFamily.RevokedReason == nil || *revokedFamily.RevokedReason != "mfa_step_up_failed" {
		t.Fatalf("failed step-up source family revocation = %#v", revokedFamily)
	}

	var leaked int64
	if err := db.Table("system.audit_logs").Where("details::text LIKE ? OR details::text LIKE ?", "%"+enrollment.Secret+"%", "%"+code+"%").Count(&leaked).Error; err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("MFA enrollment secret or code leaked to %d audit rows", leaked)
	}
}

func assertMFAReplacementFamily(t *testing.T, db *gorm.DB, sourceFamilyID, replacementFamilyID int64, reason string) {
	t.Helper()
	var source, replacement RefreshTokenFamily
	if err := db.First(&source, sourceFamilyID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&replacement, replacementFamilyID).Error; err != nil {
		t.Fatal(err)
	}
	if source.RevokedAt == nil || source.RevokedReason == nil || *source.RevokedReason != reason {
		t.Fatalf("source family revocation = %#v", source)
	}
	if replacement.AssuranceLevel != AssuranceLevelAAL2 || replacement.StepUpExpiresAt == nil ||
		replacement.ContextType != source.ContextType || replacement.TenantMembershipID == nil ||
		source.TenantMembershipID == nil || *replacement.TenantMembershipID != *source.TenantMembershipID ||
		!replacement.ExpiresAt.Equal(source.ExpiresAt) {
		t.Fatalf("replacement family = %#v, source = %#v", replacement, source)
	}
}
