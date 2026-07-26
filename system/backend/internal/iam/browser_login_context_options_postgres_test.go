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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBrowserLoginAndContextOptionsAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset browser login test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
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
	identityService := NewIdentityService(repository, now)
	membershipService := NewTenantMembershipService(repository, now)
	mfaCipher, err := NewMFACredentialCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("create MFA cipher: %v", err)
	}
	mfaService, err := NewMFAService(repository, mfaCipher, MFAServiceConfig{}, nil, now)
	if err != nil {
		t.Fatalf("create MFA service: %v", err)
	}
	loginService, err := NewBrowserLoginService(identityService, mfaService, selectionService)
	if err != nil {
		t.Fatalf("create BrowserLoginService: %v", err)
	}
	optionsService, err := NewContextOptionsService(repository)
	if err != nil {
		t.Fatalf("create ContextOptionsService: %v", err)
	}
	logoutService, err := NewLogoutService(repository, tokenService)
	if err != nil {
		t.Fatalf("create LogoutService: %v", err)
	}

	t.Run("local password login directly issues the only tenant context", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("browser-login-direct")}
		user := createContextSelectionUser(t, ctx, identityService, "browser-login-direct", audit)
		tenant := createContextSelectionTenant(t, ctx, membershipService, "browser-login-direct", audit)
		membership := establishContextSelectionMembership(
			t, ctx, membershipService, tenant.ID, user.PrincipalID, audit,
		)
		grantBootstrapPlatformRole(t, db, user.PrincipalID, currentTime.Add(-time.Minute))

		result, err := loginService.LoginLocalBrowser(ctx, LoginLocalBrowserInput{
			Username: "browser-login-direct",
			Password: "context-selection-password",
			Audit:    audit,
		})
		if err != nil {
			t.Fatalf("login local browser: %v", err)
		}
		if result.NextAction != ContextSelectionNextActionSessionIssued || result.Session == nil || result.Challenge != nil {
			t.Fatalf("direct browser login result = %#v", result)
		}
		if result.Session.Context.TenantMembershipID == nil ||
			*result.Session.Context.TenantMembershipID != membership.Membership.ID {
			t.Fatalf("direct browser login context = %#v", result.Session.Context)
		}
		family := readRefreshFamily(t, db, result.Session.FamilyID)
		if family.AssuranceLevel != AssuranceLevelAAL1 ||
			len(family.AuthenticationMethods) != 1 || family.AuthenticationMethods[0] != "password" {
			t.Fatalf("direct browser authentication facts = %#v", family)
		}
		assertIAMEventCount(t, db, user.PrincipalID, "iam.authentication.succeeded", 1)

		options, err := optionsService.ListBrowserContextOptions(ctx, result.Session.AccessToken)
		if err != nil {
			t.Fatalf("list direct browser context options: %v", err)
		}
		if len(options) != 2 || options[0].Type != ContextTypePlatform || options[0].Current ||
			!options[0].RequiresStepUp || options[1].Type != ContextTypeTenant || !options[1].Current ||
			options[1].RequiresStepUp || options[1].TenantMembershipID == nil ||
			*options[1].TenantMembershipID != membership.Membership.ID {
			t.Fatalf("direct browser context options = %#v", options)
		}

		if err := logoutService.LogoutBrowserSession(ctx, LogoutBrowserSessionInput{
			AccessToken:  result.Session.AccessToken,
			RefreshToken: result.Session.RefreshToken,
			Audit:        AuditMetadata{RequestID: stringPointer("browser-login-options-revoked")},
		}); err != nil {
			t.Fatalf("logout direct browser session: %v", err)
		}
		if _, err := optionsService.ListBrowserContextOptions(ctx, result.Session.AccessToken); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("revoked context options error = %v, want unauthorized", err)
		}
	})

	t.Run("multiple contexts return a selection ticket before any family", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("browser-login-multi")}
		user := createContextSelectionUser(t, ctx, identityService, "browser-login-multi", audit)
		tenantA := createContextSelectionTenant(t, ctx, membershipService, "browser-login-a", audit)
		tenantB := createContextSelectionTenant(t, ctx, membershipService, "browser-login-b", audit)
		membershipA := establishContextSelectionMembership(
			t, ctx, membershipService, tenantA.ID, user.PrincipalID, audit,
		)
		membershipB := establishContextSelectionMembership(
			t, ctx, membershipService, tenantB.ID, user.PrincipalID, audit,
		)
		familiesBefore := countContextSelectionRows(t, db, "system.refresh_token_families")

		result, err := loginService.LoginLocalBrowser(ctx, LoginLocalBrowserInput{
			Username: "browser-login-multi",
			Password: "context-selection-password",
			Audit:    audit,
		})
		if err != nil {
			t.Fatalf("begin multi-context browser login: %v", err)
		}
		if result.NextAction != ContextSelectionNextActionSelectContext || result.Session != nil ||
			result.Challenge == nil || len(result.Challenge.Contexts) != 2 {
			t.Fatalf("multi-context browser login result = %#v", result)
		}
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBefore {
			t.Fatalf("families before context choice = %d, want %d", got, familiesBefore)
		}
		assertSelectionTicketStoredAsHash(
			t, db, result.Challenge.SelectionTicket, user.PrincipalID, 2,
		)

		session, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
			SelectionTicket: result.Challenge.SelectionTicket,
			Choice: ContextSelectionChoice{
				Type:               ContextTypeTenant,
				TenantMembershipID: &membershipB.Membership.ID,
			},
			Audit: audit,
		})
		if err != nil {
			t.Fatalf("consume browser login context: %v", err)
		}
		options, err := optionsService.ListBrowserContextOptions(ctx, session.AccessToken)
		if err != nil {
			t.Fatalf("list multi-context options: %v", err)
		}
		if len(options) != 2 || options[0].TenantMembershipID == nil ||
			*options[0].TenantMembershipID != membershipA.Membership.ID || options[0].Current ||
			options[1].TenantMembershipID == nil ||
			*options[1].TenantMembershipID != membershipB.Membership.ID || !options[1].Current {
			t.Fatalf("ordered multi-context options = %#v", options)
		}
	})

	t.Run("invalid credentials never enter context selection", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("browser-login-invalid")}
		user := createContextSelectionUser(t, ctx, identityService, "browser-login-invalid", audit)
		tenant := createContextSelectionTenant(t, ctx, membershipService, "browser-login-invalid", audit)
		establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, audit)
		ticketsBefore := countContextSelectionRows(t, db, "system.context_selection_tickets")
		familiesBefore := countContextSelectionRows(t, db, "system.refresh_token_families")

		result, err := loginService.LoginLocalBrowser(ctx, LoginLocalBrowserInput{
			Username: "browser-login-invalid",
			Password: "wrong-password",
			Audit:    audit,
		})
		if !errors.Is(err, commonapi.ErrUnauthorized) || result != nil {
			t.Fatalf("invalid browser login result=%#v err=%v", result, err)
		}
		if got := countContextSelectionRows(t, db, "system.context_selection_tickets"); got != ticketsBefore {
			t.Fatalf("selection tickets after invalid login = %d, want %d", got, ticketsBefore)
		}
		if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBefore {
			t.Fatalf("families after invalid login = %d, want %d", got, familiesBefore)
		}
		assertIAMEventCount(t, db, user.PrincipalID, "iam.authentication.failed", 1)
	})

	t.Run("authenticated user without a context is forbidden", func(t *testing.T) {
		audit := AuditMetadata{RequestID: stringPointer("browser-login-no-context")}
		user := createContextSelectionUser(t, ctx, identityService, "browser-login-no-context", audit)
		result, err := loginService.LoginLocalBrowser(ctx, LoginLocalBrowserInput{
			Username: "browser-login-no-context",
			Password: "context-selection-password",
			Audit:    audit,
		})
		if !errors.Is(err, commonapi.ErrForbidden) || result != nil {
			t.Fatalf("no-context browser login result=%#v err=%v", result, err)
		}
		assertIAMEventCount(t, db, user.PrincipalID, "iam.authentication.succeeded", 1)
	})

	if _, err := optionsService.ListBrowserContextOptions(ctx, "addp_rt_not-an-access-token"); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("unsupported context options token error = %v, want unauthorized", err)
	}
}

func assertIAMEventCount(t *testing.T, db *gorm.DB, principalID int64, eventName string, want int64) {
	t.Helper()
	var count int64
	if err := db.Table("system.audit_logs").
		Where("principal_id = ? AND event_name = ?", principalID, eventName).
		Count(&count).Error; err != nil {
		t.Fatalf("count IAM event %s: %v", eventName, err)
	}
	if count != want {
		t.Fatalf("IAM event %s count = %d, want %d", eventName, count, want)
	}
}
