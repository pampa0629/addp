package iam

import (
	"context"
	"errors"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"os"
	"strings"
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

	exerciseNotebookSessionAuthorization(t, ctx, db)
}

func TestNotebookSessionAuthorizationServiceAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_NOTEBOOK_AUTHORIZATION") != "1" {
		t.Skip("requires isolated System MySQL installation")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Schema: "system"}, "system", &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	exerciseNotebookSessionAuthorization(t, t.Context(), db)
}

func exerciseNotebookSessionAuthorization(t *testing.T, ctx context.Context, db *gorm.DB) {
	t.Helper()
	repository := NewRepository(db)
	tokenService, err := NewTokenFamilyService(repository, BrowserSessionConfig{
		ResourceTicketOwners: []string{"manager"},
	}, nil, nil)
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
	identityService := NewIdentityService(repository, nil)
	membershipService := NewTenantMembershipService(repository, nil)
	issueAudit := AuditMetadata{RequestID: stringPointer("notebook-catalog-authorization-postgres")}
	user := createContextSelectionUser(t, ctx, identityService, "notebook-catalog-user", issueAudit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "notebook-catalog", issueAudit)
	establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, issueAudit)
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.infrastructure_administrator", "tenant", &tenant.ID, nil, nil,
		time.Now().UTC().Add(-time.Minute), nil, "manual")
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_engineer", "tenant", &tenant.ID, nil, nil,
		time.Now().UTC().Add(-time.Minute), nil, "manual")
	notebookEngineID := insertEngineFixture(t, db, tenant.ID, "Notebook Engine", "postgresql", map[string]interface{}{
		"host": "notebook-engine", "port": 5432, "database": "notebook",
	}, false)

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
	serviceActor := func(authorizationID, sessionID uuid.UUID) AuthorizeNotebookEngineCatalogInput {
		return AuthorizeNotebookEngineCatalogInput{
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
		logCredentialValidationBoundaries(t, err)
		t.Fatalf("Issue() result=%#v error=%v", issued, err)
	}
	if duplicate, err := service.Issue(ctx, IssueNotebookSessionAuthorizationInput{SourceAccessToken: accessToken, SessionID: sessionID, TaskID: 42, ExpiresIn: 10 * time.Minute, Audit: issueAudit}); duplicate != nil || !errors.Is(err, ErrNotebookSessionAuthorizationConflict) {
		t.Fatalf("duplicate Notebook session: %v", err)
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
		AuthorizationID: issued.ID, SessionID: sessionID, ExecutionID: executionID, EngineID: notebookEngineID,
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
		AuthorizationID: derived.AuthorizationID, ExecutionID: executionID, EngineID: notebookEngineID,
		RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	}); err != nil {
		t.Fatalf("consume Notebook execution authorization: %v", err)
	}

	for _, boundary := range []struct {
		engine int64
		effect string
	}{{notebookEngineID, "write"}, {notebookEngineID + 1, "read"}} {
		got, err := executionService.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{AuthorizationID: derived.AuthorizationID,
			ExecutionID: executionID, EngineID: boundary.engine, RequiredEffects: []string{boundary.effect}, ServicePrincipalID: developPrincipalID,
			ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit})
		if got != nil || !errors.Is(err, ErrExecutionAuthorizationPermissionDenied) {
			t.Fatalf("out-of-scope Notebook engine access: %v", err)
		}
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
		AuthorizationID: derived.AuthorizationID, ExecutionID: executionID, EngineID: notebookEngineID,
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
		var validationError *CredentialValidationError
		if errors.As(err, &validationError) {
			t.Fatalf("issue second authorization: credential reason=%s error=%v", validationError.Reason, err)
		}
		t.Fatalf("issue second authorization: %v", err)
	}
	secondExecutionID := uuid.New()
	secondDerived, err := service.DeriveExecutionEngineAccess(ctx, DeriveNotebookExecutionEngineAccessInput{
		AuthorizationID: second.ID, SessionID: secondSessionID, ExecutionID: secondExecutionID, EngineID: notebookEngineID,
		ExpiresIn: 5 * time.Minute, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	})
	if err != nil {
		t.Fatalf("derive second Notebook execution authorization: %v", err)
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
	var unaffected NotebookSessionAuthorization
	if err := db.Where("id=?", second.ID).Take(&unaffected).Error; err != nil || unaffected.RevokedAt != nil {
		t.Fatal("foreign tenant changed original revocation", err)
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

	if result, err := executionService.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: secondDerived.AuthorizationID, ExecutionID: secondExecutionID, EngineID: notebookEngineID,
		RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID, ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	}); result != nil || !errors.Is(err, ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("explicitly revoked Notebook execution: %v", err)
	}
	thirdSessionID := uuid.New()
	third, err := service.Issue(ctx, IssueNotebookSessionAuthorizationInput{SourceAccessToken: newSession(), SessionID: thirdSessionID, TaskID: 44, ExpiresIn: 5 * time.Minute, Audit: issueAudit})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Principal{}).Where("id = ?", user.PrincipalID).
		Update("authorization_version", gorm.Expr("authorization_version + 1")).Error; err != nil {
		t.Fatalf("advance User authorization version: %v", err)
	}
	if _, err := service.Authorize(ctx, serviceActor(third.ID, thirdSessionID)); !errors.Is(err, ErrNotebookSessionAuthorizationForbidden) {
		t.Fatalf("authorization version change error = %v", err)
	}

	var issuedAuditCount, consumedAuditCount, revokedAuditCount int64
	db.Table("system.audit_logs").Where("event_name = ?", "iam.notebook_session_authorization.issued").Count(&issuedAuditCount)
	db.Table("system.audit_logs").Where("event_name = ?", "iam.notebook_session_authorization.consumed").Count(&consumedAuditCount)
	db.Table("system.audit_logs").Where("event_name = ?", "iam.notebook_session_authorization.revoked").Count(&revokedAuditCount)
	var details []string
	if err := db.Table("system.audit_logs").Where("event_name LIKE ?", "iam.notebook_session_authorization.%").Pluck("details", &details).Error; err != nil {
		t.Fatal(err)
	}
	for _, record := range details {
		if strings.Contains(record, "addp_at_") {
			t.Fatal("Notebook audit contained an access token")
		}
	}
	if issuedAuditCount != 3 || consumedAuditCount != 2 || revokedAuditCount != 3 {
		t.Fatalf("audit issued=%d consumed=%d revoked=%d", issuedAuditCount, consumedAuditCount, revokedAuditCount)
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
