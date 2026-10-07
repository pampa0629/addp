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
	passwordutils "github.com/addp/system/pkg/utils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserSelfServiceAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset user self test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(-10 * time.Second).Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"manager", "standard"},
	}, nil, testDatabaseTime(now))
	if err != nil {
		t.Fatalf("create TokenFamilyService: %v", err)
	}
	selectionService, err := NewContextSelectionService(repository, tokenService)
	if err != nil {
		t.Fatalf("create ContextSelectionService: %v", err)
	}
	identityService := NewIdentityService(repository, testDatabaseTime(now))
	membershipService := NewTenantMembershipService(repository, now)
	mfaCipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("create MFA cipher: %v", err)
	}
	mfaService, err := NewMFAService(repository, mfaCipher, MFAServiceConfig{}, nil, testDatabaseTime(now))
	if err != nil {
		t.Fatalf("create MFA service: %v", err)
	}
	loginService, err := NewBrowserLoginService(identityService, mfaService, selectionService)
	if err != nil {
		t.Fatalf("create BrowserLoginService: %v", err)
	}
	selfService, err := NewUserSelfService(repository, identityService)
	if err != nil {
		t.Fatalf("create UserSelfService: %v", err)
	}
	logoutService, err := NewLogoutService(repository, tokenService)
	if err != nil {
		t.Fatalf("create LogoutService: %v", err)
	}

	audit := AuditMetadata{RequestID: stringPointer("user-self")}
	email := "alice@example.com"
	locale := "zh-cn"
	created, err := identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username:     "alice-self",
		Password:     "initial-password",
		DisplayName:  "Alice Self",
		PrimaryEmail: &email,
		Locale:       &locale,
		Audit:        audit,
	})
	if err != nil {
		t.Fatalf("create local self user: %v", err)
	}
	tenant := createContextSelectionTenant(t, ctx, membershipService, "user-self", audit)
	establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, created.PrincipalID, audit)
	firstSession := loginLocalUserSession(
		t, ctx, loginService, "alice-self", "initial-password", audit,
	)

	profile, err := selfService.ResolveCurrentUserProfile(ctx, firstSession.AccessToken)
	if err != nil {
		t.Fatalf("resolve current local user profile: %v", err)
	}
	if profile.ID != created.PrincipalID || profile.DisplayName != "Alice Self" ||
		profile.PrimaryEmail == nil || *profile.PrimaryEmail != email ||
		profile.Locale == nil || *profile.Locale != locale ||
		profile.LocalAccount == nil || profile.LocalAccount.Username != "alice-self" {
		t.Fatalf("current local user profile = %#v", profile)
	}

	t.Run("organization is scoped to current member and current effective facts", func(t *testing.T) {
		user := createContextSelectionUser(t, ctx, identityService, "organization-self", audit)
		otherUser := createContextSelectionUser(t, ctx, identityService, "organization-other", audit)
		currentTenant := createContextSelectionTenant(t, ctx, membershipService, "organization-self", audit)
		foreignTenant := createContextSelectionTenant(t, ctx, membershipService, "organization-foreign", audit)
		member := establishContextSelectionMembership(t, ctx, membershipService, currentTenant.ID, user.PrincipalID, audit).Membership
		otherMember := establishContextSelectionMembership(t, ctx, membershipService, currentTenant.ID, otherUser.PrincipalID, audit).Membership
		orgService := NewOrganizationService(repository, now)
		createDepartment := func(tenantID int64, code string, parentID *int64) *Department {
			t.Helper()
			department, err := orgService.CreateDepartment(ctx, CreateDepartmentInput{TenantID: tenantID, ActorPrincipalID: user.PrincipalID, Code: code, Name: code, ParentID: parentID, Audit: audit})
			if err != nil {
				t.Fatal(err)
			}
			return department
		}
		root := createDepartment(currentTenant.ID, "root", nil)
		child := createDepartment(currentTenant.ID, "outdoor", &root.ID)
		additional := createDepartment(currentTenant.ID, "additional", nil)
		foreign := createDepartment(foreignTenant.ID, "foreign", nil)
		if _, err := orgService.CreateDepartmentMembership(ctx, CreateDepartmentMembershipInput{TenantID: currentTenant.ID, DepartmentID: child.ID, TenantMembershipID: member.ID, ActorPrincipalID: user.PrincipalID, MembershipType: DepartmentMembershipTypePrimary, RelationRole: DepartmentRelationRoleLeader, Audit: audit}); err != nil {
			t.Fatal(err)
		}
		additionalMembership, err := orgService.CreateDepartmentMembership(ctx, CreateDepartmentMembershipInput{TenantID: currentTenant.ID, DepartmentID: additional.ID, TenantMembershipID: member.ID, ActorPrincipalID: user.PrincipalID, MembershipType: DepartmentMembershipTypeAdditional, RelationRole: DepartmentRelationRoleMember, Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		// Another account's relationship cannot enter this user's result, even in the same tenant.
		if _, err := orgService.CreateDepartmentMembership(ctx, CreateDepartmentMembershipInput{TenantID: currentTenant.ID, DepartmentID: root.ID, TenantMembershipID: otherMember.ID, ActorPrincipalID: user.PrincipalID, MembershipType: DepartmentMembershipTypePrimary, RelationRole: DepartmentRelationRoleMember, Audit: audit}); err != nil {
			t.Fatal(err)
		}
		group, err := orgService.CreateProjectGroup(ctx, CreateProjectGroupInput{TenantID: currentTenant.ID, ActorPrincipalID: user.PrincipalID, Code: "research", Name: "Research", Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := orgService.CreateProjectGroupMembership(ctx, CreateProjectGroupMembershipInput{TenantID: currentTenant.ID, ProjectGroupID: group.ID, TenantMembershipID: member.ID, ActorPrincipalID: user.PrincipalID, RelationRole: ProjectGroupRelationRoleCoordinator, Audit: audit}); err != nil {
			t.Fatal(err)
		}
		login := func() *IssuedBrowserSession {
			return loginLocalUserSession(t, ctx, loginService, "organization-self", "context-selection-password", audit)
		}
		session := login()
		organization, err := selfService.ResolveCurrentUserOrganization(ctx, session.AccessToken)
		if err != nil || organization.Tenant.ID != currentTenant.ID || len(organization.Departments) != 2 || len(organization.ProjectGroups) != 1 {
			t.Fatalf("organization=%#v err=%v", organization, err)
		}
		for _, department := range organization.Departments {
			if department.ID == foreign.ID || department.ID == root.ID {
				t.Fatal("foreign or ancestor department exposed as membership")
			}
			if department.ID == child.ID && (len(department.Path) != 2 || department.Path[0].ID != root.ID || department.Path[1].Name != "outdoor" || department.MembershipType != "primary" || department.RelationRole != "leader") {
				t.Fatalf("department = %#v", department)
			}
		}
		if organization.ProjectGroups[0].RelationRole != "coordinator" {
			t.Fatalf("project role = %#v", organization.ProjectGroups)
		}
		if _, err := orgService.DisableDepartment(ctx, ChangeDepartmentStatusInput{TenantID: currentTenant.ID, DepartmentID: child.ID, Version: child.Version, ActorPrincipalID: user.PrincipalID, Reason: "disabled", Audit: audit}); err != nil {
			t.Fatal(err)
		}
		if _, err := selfService.ResolveCurrentUserOrganization(ctx, session.AccessToken); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("stale authorization accepted: %v", err)
		}
		if _, err := orgService.CloseProjectGroup(ctx, CloseProjectGroupInput{TenantID: currentTenant.ID, ProjectGroupID: group.ID, Version: group.Version, ActorPrincipalID: user.PrincipalID, Reason: "completed", Audit: audit}); err != nil {
			t.Fatal(err)
		}
		if _, err := orgService.CloseDepartmentMembership(ctx, CloseOrganizationMembershipInput{TenantID: currentTenant.ID, OrganizationID: additional.ID, MembershipID: additionalMembership.ID, Version: additionalMembership.Version, ActorPrincipalID: user.PrincipalID, Reason: "ended", Audit: audit}); err != nil {
			t.Fatal(err)
		}
		organization, err = selfService.ResolveCurrentUserOrganization(ctx, login().AccessToken)
		if err != nil || len(organization.Departments) != 0 || len(organization.ProjectGroups) != 0 {
			t.Fatalf("inactive relationships leaked: organization=%#v err=%v", organization, err)
		}
		foreignMember := establishContextSelectionMembership(t, ctx, membershipService, foreignTenant.ID, user.PrincipalID, audit).Membership
		if _, err := orgService.CreateDepartmentMembership(ctx, CreateDepartmentMembershipInput{TenantID: foreignTenant.ID, DepartmentID: foreign.ID, TenantMembershipID: foreignMember.ID, ActorPrincipalID: user.PrincipalID, MembershipType: DepartmentMembershipTypePrimary, RelationRole: DepartmentRelationRoleMember, Audit: audit}); err != nil {
			t.Fatal(err)
		}
		for _, membershipID := range []int64{member.ID, foreignMember.ID} {
			challenge := beginMultiContextSelection(t, ctx, selectionService, user.PrincipalID, SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: currentTime}, audit)
			selected, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{SelectionTicket: challenge.Challenge.SelectionTicket, Choice: ContextSelectionChoice{Type: ContextTypeTenant, TenantMembershipID: &membershipID}, Audit: audit})
			if err != nil {
				t.Fatal(err)
			}
			organization, err := selfService.ResolveCurrentUserOrganization(ctx, selected.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			if membershipID == member.ID && (organization.Tenant.ID != currentTenant.ID || len(organization.Departments) != 0) {
				t.Fatalf("another tenant's relationship leaked: %#v", organization)
			}
			if membershipID == foreignMember.ID && (organization.Tenant.ID != foreignTenant.ID || len(organization.Departments) != 1 || organization.Departments[0].ID != foreign.ID) {
				t.Fatalf("context was not respected: %#v", organization)
			}
		}
	})

	t.Run("external user profile does not require a local account", func(t *testing.T) {
		var externalPrincipalID int64
		err := repository.Transaction(ctx, func(tx *Repository) error {
			principal := &Principal{PrincipalType: PrincipalTypeUser, Status: PrincipalStatusActive}
			if err := tx.CreatePrincipal(ctx, principal); err != nil {
				return err
			}
			externalPrincipalID = principal.ID
			return tx.CreateUser(ctx, &User{
				ID:          principal.ID,
				DisplayName: "External Self",
			})
		})
		if err != nil {
			t.Fatalf("create external user profile: %v", err)
		}
		externalTenant := createContextSelectionTenant(t, ctx, membershipService, "external-self", audit)
		establishContextSelectionMembership(
			t, ctx, membershipService, externalTenant.ID, externalPrincipalID, audit,
		)
		selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID: externalPrincipalID,
			Authentication: SessionAuthentication{
				Methods:         []string{"external_idp"},
				AssuranceLevel:  AssuranceLevelAAL1,
				AuthenticatedAt: currentTime,
			},
			Audit: audit,
		})
		if err != nil || selection.Session == nil {
			t.Fatalf("issue external browser session: result=%#v err=%v", selection, err)
		}
		externalProfile, err := selfService.ResolveCurrentUserProfile(ctx, selection.Session.AccessToken)
		if err != nil {
			t.Fatalf("resolve external current profile: %v", err)
		}
		if externalProfile.ID != externalPrincipalID || externalProfile.DisplayName != "External Self" ||
			externalProfile.LocalAccount != nil {
			t.Fatalf("external current profile = %#v", externalProfile)
		}
	})

	secondSession := loginLocalUserSession(
		t, ctx, loginService, "alice-self", "initial-password", audit,
	)
	if firstSession.FamilyID == secondSession.FamilyID {
		t.Fatal("two local browser logins reused one family")
	}
	authorizationVersionBefore := readRefreshFamily(t, db, firstSession.FamilyID).IssuedAuthorizationVersion

	if _, err := selfService.RotateCurrentPassword(
		ctx, firstSession.AccessToken, "wrong-password", "new-password", audit,
	); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("wrong current password error = %v", err)
	}
	assertUserSelfFamiliesActive(t, db, firstSession.FamilyID, secondSession.FamilyID)
	assertIAMServiceAuthorizationVersion(t, db, created.PrincipalID, authorizationVersionBefore)

	if _, err := selfService.RotateCurrentPassword(
		ctx, firstSession.AccessToken, "initial-password", "initial-password", audit,
	); !errors.Is(err, ErrPasswordUnchanged) {
		t.Fatalf("unchanged password error = %v", err)
	}
	assertUserSelfFamiliesActive(t, db, firstSession.FamilyID, secondSession.FamilyID)

	if _, err := selfService.RotateCurrentPassword(
		ctx,
		firstSession.AccessToken,
		"initial-password",
		"audit-rollback-password",
		AuditMetadata{RequestID: stringPointer(" ")},
	); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("password audit rollback error = %v, want bad request", err)
	}
	assertUserSelfFamiliesActive(t, db, firstSession.FamilyID, secondSession.FamilyID)
	accountAfterRollback := readIAMServiceLocalAccount(t, db, created.PrincipalID)
	if !passwordutils.CheckPassword("initial-password", accountAfterRollback.PasswordHash) {
		t.Fatal("password changed despite rejected audit")
	}

	currentTime = currentTime.Add(time.Second)
	rotated, err := selfService.RotateCurrentPassword(
		ctx, firstSession.AccessToken, "initial-password", "new-password", audit,
	)
	if err != nil {
		t.Fatalf("rotate current user password: %v", err)
	}
	if rotated.RevokedFamilyCount != 2 || rotated.AuthorizationVersion != authorizationVersionBefore+1 {
		t.Fatalf("current password rotation result = %#v", rotated)
	}
	for _, familyID := range []int64{firstSession.FamilyID, secondSession.FamilyID} {
		assertIAMServiceFamilyAndDerivativesRevoked(t, db, familyID)
	}
	if _, err := selfService.ResolveCurrentUserProfile(ctx, firstSession.AccessToken); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("old access token after password rotation error = %v", err)
	}
	rotatedAccount := readIAMServiceLocalAccount(t, db, created.PrincipalID)
	if !passwordutils.CheckPassword("new-password", rotatedAccount.PasswordHash) ||
		passwordutils.CheckPassword("initial-password", rotatedAccount.PasswordHash) {
		t.Fatal("current password rotation did not persist only the new password")
	}
	assertIAMServiceAuditCount(t, db, "iam.password.rotated", AuditResultSucceeded, 1)

	t.Run("logout winning the principal lock prevents password rotation", func(t *testing.T) {
		competitionAudit := AuditMetadata{RequestID: stringPointer("user-self-competition")}
		competitionUser := createContextSelectionUser(
			t, ctx, identityService, "user-self-competition", competitionAudit,
		)
		competitionTenant := createContextSelectionTenant(
			t, ctx, membershipService, "user-self-competition", competitionAudit,
		)
		establishContextSelectionMembership(
			t, ctx, membershipService, competitionTenant.ID, competitionUser.PrincipalID, competitionAudit,
		)
		competitionSession := loginLocalUserSession(
			t,
			ctx,
			loginService,
			"user-self-competition",
			"context-selection-password",
			competitionAudit,
		)

		first, second := runLogoutCompetition(
			t,
			db,
			competitionUser.PrincipalID,
			func() logoutCompetitionResult {
				err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
					AccessToken:  competitionSession.AccessToken,
					RefreshToken: competitionSession.RefreshToken,
					Audit:        AuditMetadata{RequestID: stringPointer("logout-before-password")},
				})
				return logoutCompetitionResult{err: err}
			},
			func() logoutCompetitionResult {
				_, err := selfService.RotateCurrentPassword(
					ctx,
					competitionSession.AccessToken,
					"context-selection-password",
					"competition-new-password",
					AuditMetadata{RequestID: stringPointer("password-after-logout")},
				)
				return logoutCompetitionResult{err: err}
			},
		)
		if first.err != nil || !errors.Is(second.err, commonapi.ErrUnauthorized) {
			t.Fatalf("logout/password competition: first=%#v second=%#v", first, second)
		}
		account := readIAMServiceLocalAccount(t, db, competitionUser.PrincipalID)
		if !passwordutils.CheckPassword("context-selection-password", account.PasswordHash) ||
			passwordutils.CheckPassword("competition-new-password", account.PasswordHash) {
			t.Fatal("password changed after logout won the principal lock")
		}
	})
}

func loginLocalUserSession(
	t *testing.T,
	ctx context.Context,
	service *BrowserLoginService,
	username string,
	password string,
	audit AuditMetadata,
) *IssuedBrowserSession {
	t.Helper()
	result, err := service.LoginLocalBrowser(ctx, LoginLocalBrowserInput{
		Username: username,
		Password: password,
		Audit:    audit,
	})
	if err != nil || result.Session == nil || result.Challenge != nil {
		t.Fatalf("login local user %s: result=%#v err=%v", username, result, err)
	}
	return result.Session
}

func assertUserSelfFamiliesActive(t *testing.T, db *gorm.DB, familyIDs ...int64) {
	t.Helper()
	for _, familyID := range familyIDs {
		family := readRefreshFamily(t, db, familyID)
		if family.RevokedAt != nil {
			t.Fatalf("family %d was unexpectedly revoked: %#v", familyID, family)
		}
	}
}
