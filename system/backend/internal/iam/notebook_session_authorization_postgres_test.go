package iam

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNotebookSessionAuthorizationServiceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`).Error; err != nil {
		t.Fatalf("reset Notebook Session authorization test schema: %v", err)
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
	service, err := NewNotebookSessionAuthorizationService(repository)
	if err != nil {
		t.Fatal(err)
	}
	executionService, err := NewExecutionAuthorizationService(repository)
	if err != nil {
		t.Fatal(err)
	}
	identityService := NewIdentityService(repository, time.Now)
	membershipService := NewTenantMembershipService(repository, time.Now)
	issueAudit := AuditMetadata{RequestID: stringPointer("notebook-catalog-authorization-postgres")}
	user := createContextSelectionUser(t, ctx, identityService, "notebook-catalog-user", issueAudit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "notebook-catalog", issueAudit)
	establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, issueAudit)
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.infrastructure_administrator", "tenant", &tenant.ID, nil, nil,
		time.Now().UTC().Add(-time.Minute), nil, "manual")
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_engineer", "tenant", &tenant.ID, nil, nil,
		time.Now().UTC().Add(-time.Minute), nil, "manual")
	if err := db.Exec(`
		INSERT INTO system.engines (id, tenant_id, name, engine_type, connection_info, lifecycle_state, is_builtin)
		VALUES (12, ?, 'Notebook Engine', 'postgresql', '{}'::json, 'active', false)
	`, tenant.ID).Error; err != nil {
		t.Fatalf("insert Notebook execution engine fixture: %v", err)
	}

	newSession := func() string {
		selection, selectionErr := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
			PrincipalID: user.PrincipalID,
			Authentication: SessionAuthentication{
				Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1,
				AuthenticatedAt: time.Now().UTC().Add(-time.Minute),
			},
			Audit: issueAudit,
		})
		if selectionErr != nil || selection.Session == nil {
			t.Fatalf("issue Notebook Catalog source session: result=%#v error=%v", selection, selectionErr)
		}
		return selection.Session.AccessToken
	}

	developPrincipalID := notebookSessionDevelopPrincipalID(t, db)
	servicePrincipalType := PrincipalTypeServicePrincipal
	tenantContext := ContextTypeTenant
	consumeAudit := AuditMetadata{
		PrincipalID: &developPrincipalID, PrincipalType: &servicePrincipalType,
		ContextType: &tenantContext, TenantID: &tenant.ID,
		RequestID: stringPointer("notebook-catalog-authorization-consume"),
	}
	serviceActor := func(authorizationID, sessionID uuid.UUID) AuthorizeNotebookCatalogInput {
		return AuthorizeNotebookCatalogInput{
			AuthorizationID: authorizationID, SessionID: sessionID,
			ServicePrincipalID: developPrincipalID, ServiceClientID: "addp-develop",
			TenantID: tenant.ID, Audit: consumeAudit,
		}
	}

	accessToken := newSession()
	sessionID := uuid.New()
	issued, err := service.Issue(ctx, IssueNotebookSessionAuthorizationInput{
		SourceAccessToken: accessToken, SessionID: sessionID, TaskID: 42,
		ExpiresIn: 10 * time.Minute, Audit: issueAudit,
	})
	if err != nil || issued.ID == uuid.Nil || issued.SessionID != sessionID || issued.TaskID != 42 {
		t.Fatalf("Issue() result=%#v error=%v", issued, err)
	}
	authorized, err := service.Authorize(ctx, serviceActor(issued.ID, sessionID))
	if err != nil || authorized.TenantID != tenant.ID || authorized.TaskID != 42 {
		t.Fatalf("Authorize() result=%#v error=%v", authorized, err)
	}
	if _, err := service.Authorize(ctx, serviceActor(issued.ID, uuid.New())); !errors.Is(err, ErrNotebookSessionAuthorizationForbidden) {
		t.Fatalf("Session mismatch error = %v", err)
	}
	executionID := uuid.New()
	derived, err := service.DeriveExecutionEngineAccess(ctx, DeriveNotebookExecutionEngineAccessInput{
		AuthorizationID: issued.ID, SessionID: sessionID, ExecutionID: executionID, EngineID: 12,
		ExpiresIn: 5 * time.Minute, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	})
	if err != nil || derived.AuthorizationID <= 0 || derived.ExecutionID != executionID {
		t.Fatalf("derive Notebook execution authorization: result=%#v error=%v", derived, err)
	}
	var executionAuthorization ExecutionAuthorization
	if err := db.First(&executionAuthorization, derived.AuthorizationID).Error; err != nil ||
		executionAuthorization.SourceNotebookSessionAuthorizationID == nil ||
		*executionAuthorization.SourceNotebookSessionAuthorizationID != issued.ID {
		t.Fatalf("Notebook execution authorization provenance=%#v error=%v", executionAuthorization, err)
	}
	if _, err := executionService.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: derived.AuthorizationID, ExecutionID: executionID, EngineID: 12,
		RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	}); err != nil {
		t.Fatalf("consume Notebook execution authorization: %v", err)
	}

	var source AccessToken
	if err := db.Where("token_hash = ?", hashOpaqueToken(accessToken)).Take(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&AccessToken{}).Where("id = ?", source.ID).
		Update("revoked_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("revoke source Access Token: %v", err)
	}
	if _, err := service.Authorize(ctx, serviceActor(issued.ID, sessionID)); err != nil {
		t.Fatalf("ordinary Access Token revocation invalidated authorization: %v", err)
	}

	familyRevokedAt := time.Now().UTC()
	if err := repository.RevokeTokenFamily(ctx, source.FamilyID, familyRevokedAt, "test_family_revocation"); err != nil {
		t.Fatalf("revoke source Token Family: %v", err)
	}
	var revoked NotebookSessionAuthorization
	if err := db.Where("id = ?", issued.ID).Take(&revoked).Error; err != nil ||
		revoked.RevokedAt == nil || revoked.RevokedReason == nil || *revoked.RevokedReason != "token_family_revoked" {
		t.Fatalf("Family revocation derivative=%#v error=%v", revoked, err)
	}
	if _, err := service.Authorize(ctx, serviceActor(issued.ID, sessionID)); !errors.Is(err, ErrNotebookSessionAuthorizationForbidden) {
		t.Fatalf("revoked Family consume error = %v", err)
	}
	if err := db.First(&executionAuthorization, derived.AuthorizationID).Error; err != nil ||
		executionAuthorization.RevokedAt == nil || executionAuthorization.RevokedReason == nil ||
		*executionAuthorization.RevokedReason != "notebook_session_revoked" {
		t.Fatalf("Family revocation execution derivative=%#v error=%v", executionAuthorization, err)
	}
	if _, err := executionService.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: derived.AuthorizationID, ExecutionID: executionID, EngineID: 12,
		RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	}); !errors.Is(err, ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("Family-revoked Notebook execution lease error = %v", err)
	}

	secondSessionID := uuid.New()
	second, err := service.Issue(ctx, IssueNotebookSessionAuthorizationInput{
		SourceAccessToken: newSession(), SessionID: secondSessionID, TaskID: 43,
		ExpiresIn: 10 * time.Minute, Audit: issueAudit,
	})
	if err != nil {
		t.Fatalf("issue second authorization: %v", err)
	}
	secondExecutionID := uuid.New()
	secondDerived, err := service.DeriveExecutionEngineAccess(ctx, DeriveNotebookExecutionEngineAccessInput{
		AuthorizationID: second.ID, SessionID: secondSessionID, ExecutionID: secondExecutionID, EngineID: 12,
		ExpiresIn: 5 * time.Minute, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	})
	if err != nil {
		t.Fatalf("derive second Notebook execution authorization: %v", err)
	}
	if err := db.Model(&Principal{}).Where("id = ?", user.PrincipalID).
		Update("authorization_version", gorm.Expr("authorization_version + 1")).Error; err != nil {
		t.Fatalf("advance User authorization version: %v", err)
	}
	if _, err := service.Authorize(ctx, serviceActor(second.ID, secondSessionID)); !errors.Is(err, ErrNotebookSessionAuthorizationForbidden) {
		t.Fatalf("authorization version change error = %v", err)
	}

	wrongTenantAudit := consumeAudit
	wrongTenant := createContextSelectionTenant(t, ctx, membershipService, "notebook-catalog-other", issueAudit)
	wrongTenantID := wrongTenant.ID
	wrongTenantAudit.TenantID = &wrongTenantID
	revokeInput := RevokeNotebookSessionAuthorizationInput{
		AuthorizationID: second.ID, SessionID: secondSessionID,
		ServicePrincipalID: developPrincipalID, ServiceClientID: "addp-develop",
		TenantID: wrongTenantID, Audit: wrongTenantAudit,
	}
	if err := service.Revoke(ctx, revokeInput); err != nil {
		t.Fatalf("cross-Tenant idempotent Revoke() error=%v", err)
	}
	revokeInput.TenantID = tenant.ID
	revokeInput.Audit = consumeAudit
	if err := service.Revoke(ctx, revokeInput); err != nil {
		t.Fatalf("Revoke() error=%v", err)
	}
	var explicitlyRevokedExecution ExecutionAuthorization
	if err := db.First(&explicitlyRevokedExecution, secondDerived.AuthorizationID).Error; err != nil ||
		explicitlyRevokedExecution.RevokedAt == nil || explicitlyRevokedExecution.RevokedReason == nil ||
		*explicitlyRevokedExecution.RevokedReason != "notebook_session_revoked" {
		t.Fatalf("explicit Session revocation execution derivative=%#v error=%v", explicitlyRevokedExecution, err)
	}
	if err := service.Revoke(ctx, revokeInput); err != nil {
		t.Fatalf("repeated Revoke() error=%v", err)
	}

	var issuedAuditCount, consumedAuditCount, revokedAuditCount, leakedTokenCount int64
	db.Table("system.audit_logs").Where("event_name = ?", "iam.notebook_session_authorization.issued").Count(&issuedAuditCount)
	db.Table("system.audit_logs").Where("event_name = ?", "iam.notebook_session_authorization.consumed").Count(&consumedAuditCount)
	db.Table("system.audit_logs").Where("event_name = ?", "iam.notebook_session_authorization.revoked").Count(&revokedAuditCount)
	db.Table("system.audit_logs").Where("event_name LIKE ? AND details::text LIKE ?",
		"iam.notebook_session_authorization.%", "%addp_at_%").Count(&leakedTokenCount)
	if issuedAuditCount != 2 || consumedAuditCount != 2 || revokedAuditCount != 3 || leakedTokenCount != 0 {
		t.Fatalf("audit issued=%d consumed=%d revoked=%d leaked=%d",
			issuedAuditCount, consumedAuditCount, revokedAuditCount, leakedTokenCount)
	}
}

func notebookSessionDevelopPrincipalID(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var principalID int64
	if err := db.Raw(`
		SELECT id FROM system.service_principals WHERE name = 'addp-develop'
	`).Scan(&principalID).Error; err != nil || principalID <= 0 {
		t.Fatalf("find addp-develop principal: id=%d error=%v", principalID, err)
	}
	return principalID
}
