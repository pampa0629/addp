package iam

import (
	"context"
	"errors"
	"os"
	"strings"
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

func TestContextSelectionServicesAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset context selection test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	currentTime := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	now := func() time.Time { return currentTime }
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"standard", "manager"},
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
	audit := AuditMetadata{RequestID: stringPointer("context-selection-test")}
	authentication := SessionAuthentication{
		Methods:         []string{"password"},
		AssuranceLevel:  AssuranceLevelAAL1,
		AuthenticatedAt: currentTime.Add(-time.Minute),
	}

	user := createContextSelectionUser(t, ctx, identityService, "direct-user", audit)
	tenantA := createContextSelectionTenant(t, ctx, membershipService, "alpha", audit)
	membershipA := establishContextSelectionMembership(
		t, ctx, membershipService, tenantA.ID, user.PrincipalID, audit,
	)
	direct, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID:    user.PrincipalID,
		Authentication: authentication,
		Audit:          audit,
	})
	if err != nil {
		t.Fatalf("begin direct context: %v", err)
	}
	if direct.NextAction != ContextSelectionNextActionSessionIssued || direct.Session == nil || direct.Challenge != nil {
		t.Fatalf("direct context result = %#v", direct)
	}
	assertIssuedBrowserSession(t, db, direct.Session, membershipA.Membership.ID, 2)
	assertContextSelectionTableCount(t, db, "system.context_selection_tickets", 0)
	assertPlainSessionSecretsAbsent(t, db, direct.Session)

	currentTime = currentTime.Add(time.Second)
	tenantB := createContextSelectionTenant(t, ctx, membershipService, "beta", audit)
	membershipB := establishContextSelectionMembership(
		t, ctx, membershipService, tenantB.ID, user.PrincipalID, audit,
	)
	challengeResult := beginMultiContextSelection(
		t, ctx, selectionService, user.PrincipalID, authentication, audit,
	)
	if len(challengeResult.Challenge.Contexts) != 2 ||
		challengeResult.Challenge.Contexts[0].TenantCode != "alpha" ||
		challengeResult.Challenge.Contexts[1].TenantCode != "beta" {
		t.Fatalf("ordered tenant contexts = %#v", challengeResult.Challenge.Contexts)
	}
	assertSelectionTicketStoredAsHash(t, db, challengeResult.Challenge.SelectionTicket, user.PrincipalID, 2)
	familiesBeforeConsume := countContextSelectionRows(t, db, "system.refresh_token_families")
	consumed, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
		SelectionTicket: challengeResult.Challenge.SelectionTicket,
		Choice: ContextSelectionChoice{
			Type:               ContextTypeTenant,
			TenantMembershipID: &membershipB.Membership.ID,
		},
		Audit: audit,
	})
	if err != nil {
		t.Fatalf("consume context selection ticket: %v", err)
	}
	assertIssuedBrowserSession(t, db, consumed, membershipB.Membership.ID, 2)
	assertContextSelectionTicketConsumed(t, db, challengeResult.Challenge.SelectionTicket, true)
	if _, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
		SelectionTicket: challengeResult.Challenge.SelectionTicket,
		Choice: ContextSelectionChoice{
			Type:               ContextTypeTenant,
			TenantMembershipID: &membershipB.Membership.ID,
		},
		Audit: audit,
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("replayed selection ticket error = %v, want conflict", err)
	}
	if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBeforeConsume+1 {
		t.Fatalf("families after ticket replay = %d, want %d", got, familiesBeforeConsume+1)
	}

	rollbackChallenge := beginMultiContextSelection(
		t, ctx, selectionService, user.PrincipalID, authentication, audit,
	)
	familiesBeforeRollback := countContextSelectionRows(t, db, "system.refresh_token_families")
	accessTokensBeforeRollback := countContextSelectionRows(t, db, "system.access_tokens")
	_, err = selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
		SelectionTicket: rollbackChallenge.Challenge.SelectionTicket,
		Choice: ContextSelectionChoice{
			Type:               ContextTypeTenant,
			TenantMembershipID: &membershipA.Membership.ID,
		},
		Audit: AuditMetadata{RequestID: stringPointer(" ")},
	})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("consume with rejected audit error = %v, want bad request", err)
	}
	assertContextSelectionTicketConsumed(t, db, rollbackChallenge.Challenge.SelectionTicket, false)
	if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBeforeRollback {
		t.Fatalf("families after rejected audit = %d, want %d", got, familiesBeforeRollback)
	}
	if got := countContextSelectionRows(t, db, "system.access_tokens"); got != accessTokensBeforeRollback {
		t.Fatalf("access tokens after rejected audit = %d, want %d", got, accessTokensBeforeRollback)
	}

	concurrentChallenge := beginMultiContextSelection(
		t, ctx, selectionService, user.PrincipalID, authentication, audit,
	)
	familiesBeforeConcurrent := countContextSelectionRows(t, db, "system.refresh_token_families")
	type consumeResult struct {
		session *IssuedBrowserSession
		err     error
	}
	results := make(chan consumeResult, 2)
	var waitGroup sync.WaitGroup
	for index := 0; index < 2; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			requestAudit := AuditMetadata{RequestID: stringPointer("concurrent-selection-" + string(rune('a'+index)))}
			session, err := selectionService.ConsumeContextSelection(ctx, ConsumeContextSelectionInput{
				SelectionTicket: concurrentChallenge.Challenge.SelectionTicket,
				Choice: ContextSelectionChoice{
					Type:               ContextTypeTenant,
					TenantMembershipID: &membershipA.Membership.ID,
				},
				Audit: requestAudit,
			})
			results <- consumeResult{session: session, err: err}
		}(index)
	}
	waitGroup.Wait()
	close(results)
	successCount := 0
	conflictCount := 0
	for result := range results {
		if result.err == nil && result.session != nil {
			successCount++
		} else if errors.Is(result.err, commonapi.ErrConflict) {
			conflictCount++
		} else {
			t.Fatalf("unexpected concurrent consume result: session=%#v err=%v", result.session, result.err)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent consumes = success:%d conflict:%d, want 1/1", successCount, conflictCount)
	}
	if got := countContextSelectionRows(t, db, "system.refresh_token_families"); got != familiesBeforeConcurrent+1 {
		t.Fatalf("families after concurrent consume = %d, want %d", got, familiesBeforeConcurrent+1)
	}

	grantBootstrapPlatformRole(t, db, user.PrincipalID, currentTime.Add(-time.Minute))
	platformAndTenantAuthentication := authentication
	platformAndTenantAuthentication.Methods = []string{"password", "totp"}
	platformAndTenantAuthentication.AssuranceLevel = AssuranceLevelAAL2
	platformAndTenant := beginMultiContextSelection(
		t, ctx, selectionService, user.PrincipalID, platformAndTenantAuthentication, audit,
	)
	if len(platformAndTenant.Challenge.Contexts) != 3 ||
		platformAndTenant.Challenge.Contexts[0].Type != ContextTypePlatform ||
		platformAndTenant.Challenge.Contexts[1].TenantCode != "alpha" ||
		platformAndTenant.Challenge.Contexts[2].TenantCode != "beta" {
		t.Fatalf("platform and tenant context order = %#v", platformAndTenant.Challenge.Contexts)
	}

	platformOnly := createContextSelectionUser(t, ctx, identityService, "platform-user", audit)
	grantBootstrapPlatformRole(t, db, platformOnly.PrincipalID, currentTime.Add(-time.Minute))
	if _, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID:    platformOnly.PrincipalID,
		Authentication: authentication,
		Audit:          audit,
	}); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("platform aal1 error = %v, want step-up required", err)
	}
	platformAuthentication := authentication
	platformAuthentication.Methods = []string{"password", "totp"}
	platformAuthentication.AssuranceLevel = AssuranceLevelAAL2
	platformDirect, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID:    platformOnly.PrincipalID,
		Authentication: platformAuthentication,
		Audit:          audit,
	})
	if err != nil {
		t.Fatalf("begin platform context: %v", err)
	}
	if platformDirect.Session == nil || platformDirect.Session.Context.Type != ContextTypePlatform ||
		platformDirect.Session.Context.TenantID != nil {
		t.Fatalf("platform direct session = %#v", platformDirect)
	}

	noContext := createContextSelectionUser(t, ctx, identityService, "no-context-user", audit)
	if _, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID:    noContext.PrincipalID,
		Authentication: authentication,
		Audit:          audit,
	}); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("no context error = %v, want forbidden", err)
	}
}

func createContextSelectionUser(
	t *testing.T,
	ctx context.Context,
	service *IdentityService,
	username string,
	audit AuditMetadata,
) *CreatedLocalUser {
	t.Helper()
	created, err := service.CreateLocalUser(ctx, CreateLocalUserInput{
		Username:    username,
		Password:    "context-selection-password",
		DisplayName: username,
		Audit:       audit,
	})
	if err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return created
}

func createContextSelectionTenant(
	t *testing.T,
	ctx context.Context,
	service *TenantMembershipService,
	code string,
	audit AuditMetadata,
) *Tenant {
	t.Helper()
	tenant, err := service.CreateTenant(ctx, CreateTenantInput{
		Code:  code,
		Name:  strings.ToUpper(code),
		Audit: audit,
	})
	if err != nil {
		t.Fatalf("create tenant %s: %v", code, err)
	}
	return tenant
}

func establishContextSelectionMembership(
	t *testing.T,
	ctx context.Context,
	service *TenantMembershipService,
	tenantID int64,
	principalID int64,
	audit AuditMetadata,
) *TenantMembershipChangeResult {
	t.Helper()
	result, err := service.EstablishMembership(ctx, EstablishTenantMembershipInput{
		TenantID:             tenantID,
		PrincipalID:          principalID,
		SourceType:           TenantMembershipSourceManual,
		CreatedByPrincipalID: &principalID,
		Audit:                audit,
	})
	if err != nil {
		t.Fatalf("establish membership for tenant %d: %v", tenantID, err)
	}
	return result
}

func beginMultiContextSelection(
	t *testing.T,
	ctx context.Context,
	service *ContextSelectionService,
	principalID int64,
	authentication SessionAuthentication,
	audit AuditMetadata,
) *ContextSelectionResult {
	t.Helper()
	result, err := service.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID:    principalID,
		Authentication: authentication,
		Audit:          audit,
	})
	if err != nil {
		t.Fatalf("begin multi-context selection: %v", err)
	}
	if result.NextAction != ContextSelectionNextActionSelectContext || result.Challenge == nil || result.Session != nil {
		t.Fatalf("multi-context result = %#v", result)
	}
	if !strings.HasPrefix(result.Challenge.SelectionTicket, "addp_cst_") {
		t.Fatalf("selection ticket = %q", result.Challenge.SelectionTicket)
	}
	return result
}

func assertIssuedBrowserSession(
	t *testing.T,
	db *gorm.DB,
	session *IssuedBrowserSession,
	membershipID int64,
	wantResourceTickets int,
) {
	t.Helper()
	if session == nil || !strings.HasPrefix(session.AccessToken, "addp_at_") ||
		!strings.HasPrefix(session.RefreshToken, "addp_rt_") {
		t.Fatalf("issued browser session = %#v", session)
	}
	if session.Context.Type != ContextTypeTenant || session.Context.TenantMembershipID == nil ||
		*session.Context.TenantMembershipID != membershipID {
		t.Fatalf("issued tenant context = %#v, want membership %d", session.Context, membershipID)
	}
	if len(session.ResourceAccessTickets) != wantResourceTickets {
		t.Fatalf("resource tickets = %#v, want %d", session.ResourceAccessTickets, wantResourceTickets)
	}
	for owner, ticket := range session.ResourceAccessTickets {
		if !strings.HasPrefix(ticket, "addp_rat_") || (owner != "manager" && owner != "standard") {
			t.Fatalf("resource ticket %s=%q", owner, ticket)
		}
	}
	var family RefreshTokenFamily
	if err := db.First(&family, session.FamilyID).Error; err != nil {
		t.Fatalf("read issued family: %v", err)
	}
	if family.ClientID != "addp-web" || family.AuthType != "first_party" ||
		family.IssuedAuthorizationVersion <= 0 {
		t.Fatalf("issued family = %#v", family)
	}
	var access AccessToken
	if err := db.Where("family_id = ?", family.ID).Take(&access).Error; err != nil {
		t.Fatalf("read issued access token: %v", err)
	}
	if access.TokenHash != hashOpaqueToken(session.AccessToken) || access.TokenHash == session.AccessToken {
		t.Fatalf("stored access token hash = %q", access.TokenHash)
	}
	var refresh RefreshToken
	if err := db.Where("family_id = ?", family.ID).Take(&refresh).Error; err != nil {
		t.Fatalf("read issued refresh token: %v", err)
	}
	if refresh.TokenHash != hashOpaqueToken(session.RefreshToken) || refresh.IssuedAccessTokenID != access.ID {
		t.Fatalf("stored refresh token = %#v", refresh)
	}
}

func assertPlainSessionSecretsAbsent(t *testing.T, db *gorm.DB, session *IssuedBrowserSession) {
	t.Helper()
	secrets := []string{session.AccessToken, session.RefreshToken}
	for _, ticket := range session.ResourceAccessTickets {
		secrets = append(secrets, ticket)
	}
	for _, secret := range secrets {
		var auditCount int64
		if err := db.Table("system.audit_logs").Where("details::text LIKE ?", "%"+secret+"%").Count(&auditCount).Error; err != nil {
			t.Fatalf("scan audit details for secret: %v", err)
		}
		if auditCount != 0 {
			t.Fatalf("plain session secret appeared in %d audit rows", auditCount)
		}
	}
}

func assertSelectionTicketStoredAsHash(
	t *testing.T,
	db *gorm.DB,
	plainTicket string,
	principalID int64,
	wantOptions int64,
) {
	t.Helper()
	var ticket ContextSelectionTicket
	if err := db.Where("token_hash = ?", hashOpaqueToken(plainTicket)).Take(&ticket).Error; err != nil {
		t.Fatalf("read selection ticket by hash: %v", err)
	}
	if ticket.TokenHash == plainTicket || ticket.PrincipalID != principalID {
		t.Fatalf("stored selection ticket = %#v", ticket)
	}
	var optionCount int64
	if err := db.Model(&ContextSelectionOption{}).Where("ticket_id = ?", ticket.ID).Count(&optionCount).Error; err != nil {
		t.Fatalf("count selection options: %v", err)
	}
	if optionCount != wantOptions {
		t.Fatalf("selection option count = %d, want %d", optionCount, wantOptions)
	}
	var leakedAuditCount int64
	if err := db.Table("system.audit_logs").Where("details::text LIKE ?", "%"+plainTicket+"%").Count(&leakedAuditCount).Error; err != nil {
		t.Fatalf("scan ticket audit details: %v", err)
	}
	if leakedAuditCount != 0 {
		t.Fatalf("selection ticket leaked into %d audit rows", leakedAuditCount)
	}
}

func assertContextSelectionTicketConsumed(t *testing.T, db *gorm.DB, plainTicket string, want bool) {
	t.Helper()
	var ticket ContextSelectionTicket
	if err := db.Where("token_hash = ?", hashOpaqueToken(plainTicket)).Take(&ticket).Error; err != nil {
		t.Fatalf("read context selection ticket: %v", err)
	}
	if (ticket.ConsumedAt != nil) != want {
		t.Fatalf("ticket consumed = %t, want %t", ticket.ConsumedAt != nil, want)
	}
}

func grantBootstrapPlatformRole(t *testing.T, db *gorm.DB, principalID int64, validFrom time.Time) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, source_type, valid_from)
		SELECT ?, role.id, 'platform', 'bootstrap', ?
		FROM system.roles role
		WHERE role.role_key = 'platform.system_administrator'
	`, principalID, validFrom).Error; err != nil {
		t.Fatalf("grant bootstrap platform role: %v", err)
	}
}

func countContextSelectionRows(t *testing.T, db *gorm.DB, tableName string) int64 {
	t.Helper()
	var count int64
	if err := db.Table(tableName).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	return count
}

func assertContextSelectionTableCount(t *testing.T, db *gorm.DB, tableName string, want int64) {
	t.Helper()
	if got := countContextSelectionRows(t, db, tableName); got != want {
		t.Fatalf("%s count = %d, want %d", tableName, got, want)
	}
}
