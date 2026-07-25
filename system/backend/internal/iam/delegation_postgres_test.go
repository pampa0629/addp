package iam

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/migration"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDelegationServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_RUNTIME_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_RUNTIME_TEST_DSN to a disposable PostgreSQL 15+ database")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatalf("reset delegation test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}

	repository := NewRepository(db)
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"manager"},
	}, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	selectionService, err := NewContextSelectionService(repository, tokenService)
	if err != nil {
		t.Fatal(err)
	}
	delegationService, err := NewDelegationService(
		repository,
		systemauthorization.ToolAuthorizationCatalog{},
		DelegationServiceConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	identityService := NewIdentityService(repository, time.Now)
	membershipService := NewTenantMembershipService(repository, time.Now)
	audit := AuditMetadata{RequestID: stringPointer("delegation-postgres")}
	user := createContextSelectionUser(t, ctx, identityService, "delegation-user", audit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "delegation", audit)
	membership := establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, audit)
	insertRoleAssignment(
		t,
		db,
		user.PrincipalID,
		"tenant.data_engineer",
		"tenant",
		&tenant.ID,
		nil,
		nil,
		time.Now().UTC().Add(-time.Minute),
		nil,
		"manual",
	)
	sessionSelection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID: user.PrincipalID,
		Authentication: SessionAuthentication{
			Methods:         []string{"password"},
			AssuranceLevel:  AssuranceLevelAAL1,
			AuthenticatedAt: time.Now().UTC().Add(-time.Minute),
		},
		Audit: audit,
	})
	if err != nil || sessionSelection.Session == nil {
		t.Fatalf("issue source browser session: result=%#v error=%v", sessionSelection, err)
	}

	issued, err := delegationService.IssueDelegatedAccessToken(ctx, IssueDelegatedAccessTokenInput{
		SourceAccessToken: sessionSelection.Session.AccessToken,
		Audience:          "develop",
		Scopes:            []string{"workflow.run"},
		AgentRunID:        "run-first-party",
		ToolCallID:        "call-first-party",
		Audit:             audit,
	})
	if err != nil {
		t.Fatalf("issue first-party delegation: %v", err)
	}
	if !strings.HasPrefix(issued.AccessToken, "addp_dat_") || issued.TokenType != "Bearer" ||
		issued.Audience != "develop" || len(issued.Scopes) != 1 || issued.Scopes[0] != "workflow.run" {
		t.Fatalf("issued delegation = %#v", issued)
	}
	if issued.ExpiresAt.After(time.Now().UTC().Add(2 * time.Minute)) {
		t.Fatalf("delegated expiry exceeds two minutes: %s", issued.ExpiresAt)
	}

	var stored DelegatedAccessToken
	if err := db.Where("agent_run_id = ? AND tool_call_id = ?", "run-first-party", "call-first-party").Take(&stored).Error; err != nil {
		t.Fatalf("load stored delegation: %v", err)
	}
	if stored.TokenHash == issued.AccessToken || stored.TokenHash != hashOpaqueToken(issued.AccessToken) {
		t.Fatalf("delegated token was not stored as SHA-256 hash: %q", stored.TokenHash)
	}
	var source AccessToken
	if err := db.Where("token_hash = ?", hashOpaqueToken(sessionSelection.Session.AccessToken)).Take(&source).Error; err != nil {
		t.Fatal(err)
	}
	var family RefreshTokenFamily
	if err := db.First(&family, source.FamilyID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SourceAccessTokenID != source.ID || stored.ExpiresAt.After(source.ExpiresAt) || stored.ExpiresAt.After(family.ExpiresAt) {
		t.Fatalf("stored delegation lifetime/source = token:%#v source:%#v family:%#v", stored, source, family)
	}
	resolved, err := NewAuthContextService(repository)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := resolved.ResolveDelegatedAccessToken(ctx, issued.AccessToken)
	if err != nil {
		t.Fatalf("resolve issued delegation: %v", err)
	}
	if projected.Client.ClientID == nil || *projected.Client.ClientID != "addp-web" ||
		projected.Delegation == nil || projected.Delegation.AgentRunID != "run-first-party" {
		t.Fatalf("projected delegation = %#v", projected)
	}

	if _, err := delegationService.IssueDelegatedAccessToken(ctx, IssueDelegatedAccessTokenInput{
		SourceAccessToken: sessionSelection.Session.AccessToken,
		Audience:          "develop",
		Scopes:            []string{"workflow.run"},
		AgentRunID:        "run-first-party",
		ToolCallID:        "call-first-party",
		Audit:             audit,
	}); !errors.Is(err, ErrDelegationConflict) || !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate delegation error=%v", err)
	}
	if _, err := delegationService.IssueDelegatedAccessToken(ctx, IssueDelegatedAccessTokenInput{
		SourceAccessToken: sessionSelection.Session.AccessToken,
		Audience:          "system",
		Scopes:            []string{"engine.list"},
		AgentRunID:        "run-permission-denied",
		ToolCallID:        "call-permission-denied",
		Audit:             audit,
	}); !errors.Is(err, ErrDelegationPermissionDenied) || !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("Role Permission denial error=%v", err)
	}

	currentTime := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	oauthToken := "addp_at_oauth_delegation"
	insertOAuthSourceAccessToken(
		t,
		db,
		user.PrincipalID,
		membership.Membership.ID,
		uuid.NewString(),
		oauthToken,
		[]string{"workflow.run"},
		currentTime,
	)
	oauthIssued, err := delegationService.IssueDelegatedAccessToken(ctx, IssueDelegatedAccessTokenInput{
		SourceAccessToken: oauthToken,
		Audience:          "develop",
		Scopes:            []string{"workflow.run"},
		AgentRunID:        "run-oauth",
		ToolCallID:        "call-oauth",
		Audit:             audit,
	})
	if err != nil {
		t.Fatalf("issue OAuth delegation: %v", err)
	}
	oauthContext, err := resolved.ResolveDelegatedAccessToken(ctx, oauthIssued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if oauthContext.Client.ClientID == nil || *oauthContext.Client.ClientID != "addp-cli" ||
		oauthContext.Delegation == nil || oauthContext.Delegation.DelegatedByClientID != "addp-cli" {
		t.Fatalf("OAuth delegated context = %#v", oauthContext)
	}

	narrowOAuthToken := "addp_at_oauth_scope_denied"
	insertOAuthSourceAccessToken(
		t,
		db,
		user.PrincipalID,
		membership.Membership.ID,
		uuid.NewString(),
		narrowOAuthToken,
		[]string{"workflow.validate"},
		currentTime,
	)
	if _, err := delegationService.IssueDelegatedAccessToken(ctx, IssueDelegatedAccessTokenInput{
		SourceAccessToken: narrowOAuthToken,
		Audience:          "develop",
		Scopes:            []string{"workflow.run"},
		AgentRunID:        "run-oauth-denied",
		ToolCallID:        "call-oauth-denied",
		Audit:             audit,
	}); !errors.Is(err, ErrDelegationPermissionDenied) || !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("OAuth scope expansion error=%v", err)
	}

	invalidHTTPStatus := 99
	rollbackAudit := audit
	rollbackAudit.HTTPStatus = &invalidHTTPStatus
	if _, err := delegationService.IssueDelegatedAccessToken(ctx, IssueDelegatedAccessTokenInput{
		SourceAccessToken: sessionSelection.Session.AccessToken,
		Audience:          "develop",
		Scopes:            []string{"workflow.run"},
		AgentRunID:        "run-audit-rollback",
		ToolCallID:        "call-audit-rollback",
		Audit:             rollbackAudit,
	}); err == nil {
		t.Fatal("invalid audit metadata unexpectedly succeeded")
	}
	var rollbackCount int64
	if err := db.Model(&DelegatedAccessToken{}).
		Where("agent_run_id = ? AND tool_call_id = ?", "run-audit-rollback", "call-audit-rollback").
		Count(&rollbackCount).Error; err != nil || rollbackCount != 0 {
		t.Fatalf("delegation was not rolled back with audit: count=%d error=%v", rollbackCount, err)
	}

	var auditCount int64
	if err := db.Table("system.audit_logs").Where("event_name = ?", "iam.delegation.issued").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("delegation audit count=%d, want 2", auditCount)
	}
	var leakedSecretCount int64
	if err := db.Table("system.audit_logs").
		Where("event_name = ? AND details::text LIKE ?", "iam.delegation.issued", "%addp_dat_%").
		Count(&leakedSecretCount).Error; err != nil || leakedSecretCount != 0 {
		t.Fatalf("delegation audit leaked token: count=%d error=%v", leakedSecretCount, err)
	}
}
