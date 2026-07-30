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
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExecutionAuthorizationServiceAgainstPostgres(t *testing.T) {
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
		t.Fatalf("reset execution authorization test schema: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply IAM migrations: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE system.engines (
			id bigint PRIMARY KEY,
			tenant_id bigint,
			lifecycle_state text NOT NULL,
			is_builtin boolean NOT NULL DEFAULT false
		)
		; CREATE SCHEMA common
		; CREATE TABLE common.task_executions (
			execution_id uuid PRIMARY KEY,
			tenant_id bigint NOT NULL,
			module text NOT NULL,
			parent_execution_id uuid,
			status text NOT NULL,
			actor_principal_id bigint,
			actor_tenant_membership_id bigint,
			issued_authorization_version bigint
		)
	`).Error; err != nil {
		t.Fatalf("create execution engine fixture table: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec(`DROP SCHEMA IF EXISTS common CASCADE`).Error; err != nil {
			t.Errorf("clean execution authorization fixture schema: %v", err)
		}
	})

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
	service, err := NewExecutionAuthorizationService(repository)
	if err != nil {
		t.Fatal(err)
	}
	identityService := NewIdentityService(repository, time.Now)
	membershipService := NewTenantMembershipService(repository, time.Now)
	audit := AuditMetadata{RequestID: stringPointer("execution-authorization-postgres")}
	user := createContextSelectionUser(t, ctx, identityService, "execution-authorization-user", audit)
	tenant := createContextSelectionTenant(t, ctx, membershipService, "execution-authorization", audit)
	membership := establishContextSelectionMembership(t, ctx, membershipService, tenant.ID, user.PrincipalID, audit)
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.data_engineer", "tenant", &tenant.ID, nil, nil, time.Now().UTC().Add(-time.Minute), nil, "manual")

	selection, err := selectionService.BeginContextSelection(ctx, BeginContextSelectionInput{
		PrincipalID: user.PrincipalID,
		Authentication: SessionAuthentication{
			Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1,
			AuthenticatedAt: time.Now().UTC().Add(-time.Minute),
		},
		Audit: audit,
	})
	if err != nil || selection.Session == nil {
		t.Fatalf("issue execution source session: result=%#v error=%v", selection, err)
	}
	if err := db.Exec(`
		INSERT INTO system.engines (id, tenant_id, lifecycle_state, is_builtin)
		VALUES (12, ?, 'active', false), (13, NULL, 'active', true), (14, ?, 'active', false)
	`, tenant.ID, tenant.ID+100).Error; err != nil {
		t.Fatalf("insert engine fixtures: %v", err)
	}

	executionID := uuid.New()
	issued, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: executionID, EngineIDs: []int64{13, 12},
		Effects: []string{"read"}, ExpiresIn: 10 * time.Minute, Audit: audit,
	})
	if err != nil {
		t.Fatalf("issue execution authorization: %v", err)
	}
	if issued.ID <= 0 || issued.TenantID != tenant.ID || issued.TenantMembershipID != membership.Membership.ID ||
		len(issued.EngineIDs) != 2 || issued.EngineIDs[0] != 12 || issued.EngineIDs[1] != 13 {
		t.Fatalf("issued execution authorization = %#v", issued)
	}
	if _, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: executionID, EngineIDs: []int64{12},
		Effects: []string{"read"}, Audit: audit,
	}); !errors.Is(err, ErrExecutionAuthorizationConflict) {
		t.Fatalf("duplicate execution authorization error = %v", err)
	}
	if _, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: uuid.New(), EngineIDs: []int64{14},
		Effects: []string{"read"}, Audit: audit,
	}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("cross-tenant engine issue error = %v", err)
	}
	if _, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: uuid.New(), EngineIDs: []int64{12},
		Effects: []string{"ddl"}, Audit: audit,
	}); !errors.Is(err, ErrExecutionAuthorizationPermissionDenied) {
		t.Fatalf("DDL permission issue error = %v", err)
	}

	var developPrincipalID int64
	if err := db.Raw(`
		SELECT service_principal.id
		FROM system.service_principals service_principal
		WHERE service_principal.name = 'addp-develop'
	`).Scan(&developPrincipalID).Error; err != nil || developPrincipalID <= 0 {
		t.Fatalf("find addp-develop principal: id=%d error=%v", developPrincipalID, err)
	}
	servicePrincipalType := PrincipalTypeServicePrincipal
	tenantContext := ContextTypeTenant
	consumeAudit := AuditMetadata{
		PrincipalID: &developPrincipalID, PrincipalType: &servicePrincipalType,
		ContextType: &tenantContext, TenantID: &tenant.ID,
		RequestID: stringPointer("execution-authorization-consume"),
	}
	authorized, err := service.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: 12,
		RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	})
	if err != nil || authorized.EngineID != 12 {
		t.Fatalf("authorize execution engine access: result=%#v error=%v", authorized, err)
	}

	parentExecutionID := uuid.New()
	childExecutionID := uuid.New()
	if err := db.Exec(`
		INSERT INTO common.task_executions (
			execution_id, tenant_id, module, status, actor_principal_id,
			actor_tenant_membership_id, issued_authorization_version
		) VALUES (?, ?, 'orchestrator', 'running', ?, ?, ?)
	`, parentExecutionID, tenant.ID, issued.ActorPrincipalID, issued.TenantMembershipID,
		issued.IssuedAuthorizationVersion).Error; err != nil {
		t.Fatalf("create parent execution provenance: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO common.task_executions (
			execution_id, tenant_id, module, parent_execution_id, status,
			actor_principal_id, actor_tenant_membership_id, issued_authorization_version
		) VALUES (?, ?, 'develop', ?, 'pending', ?, ?, ?)
	`, childExecutionID, tenant.ID, parentExecutionID,
		issued.ActorPrincipalID, issued.TenantMembershipID, issued.IssuedAuthorizationVersion).Error; err != nil {
		t.Fatalf("create child execution provenance: %v", err)
	}
	issuedFromExecution, err := service.IssueFromExecution(ctx, IssueExecutionAuthorizationFromExecutionInput{
		ParentExecutionID: parentExecutionID, Audience: "develop", ExecutionID: childExecutionID,
		EngineIDs: []int64{12}, Effects: []string{"read"}, ExpiresIn: 10 * time.Minute,
		ServicePrincipalID: developPrincipalID, ServiceClientID: "addp-develop",
		TenantID: tenant.ID, Audit: consumeAudit,
	})
	if err != nil || issuedFromExecution.ActorPrincipalID != issued.ActorPrincipalID ||
		issuedFromExecution.TenantMembershipID != issued.TenantMembershipID ||
		issuedFromExecution.IssuedAuthorizationVersion != issued.IssuedAuthorizationVersion {
		t.Fatalf("issue authorization from parent execution: result=%#v error=%v", issuedFromExecution, err)
	}
	for name, input := range map[string]AuthorizeExecutionEngineAccessInput{
		"wrong client": {
			AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: 12,
			RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
			ServiceClientID: "addp-meta", TenantID: tenant.ID, Audit: consumeAudit,
		},
		"effect expansion": {
			AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: 12,
			RequiredEffects: []string{"write"}, ServicePrincipalID: developPrincipalID,
			ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.AuthorizeEngineAccess(ctx, input); !errors.Is(err, ErrExecutionAuthorizationPermissionDenied) {
				t.Fatalf("error = %v, want permission denied", err)
			}
		})
	}

	concurrentExecutionID := uuid.New()
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, issueErr := service.Issue(ctx, IssueExecutionAuthorizationInput{
				SourceAccessToken: selection.Session.AccessToken,
				Audience:          "develop", ExecutionID: concurrentExecutionID, EngineIDs: []int64{12},
				Effects: []string{"read"}, Audit: audit,
			})
			results <- issueErr
		}()
	}
	wait.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for issueErr := range results {
		switch {
		case issueErr == nil:
			succeeded++
		case errors.Is(issueErr, ErrExecutionAuthorizationConflict):
			conflicted++
		default:
			t.Fatalf("concurrent issue error = %v", issueErr)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent issue results succeeded=%d conflicted=%d", succeeded, conflicted)
	}

	var source AccessToken
	if err := db.Where("token_hash = ?", hashOpaqueToken(selection.Session.AccessToken)).Take(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&AccessToken{}).Where("id = ?", source.ID).Update("revoked_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("revoke source access token: %v", err)
	}
	var authorizationAfterSourceRevocation ExecutionAuthorization
	if err := db.First(&authorizationAfterSourceRevocation, issued.ID).Error; err != nil ||
		authorizationAfterSourceRevocation.RevokedAt != nil || authorizationAfterSourceRevocation.RevokedReason != nil {
		t.Fatalf("execution authorization changed with source token revocation: authorization=%#v error=%v", authorizationAfterSourceRevocation, err)
	}
	if _, err := service.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: 12,
		RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	}); err != nil {
		t.Fatalf("source-token-independent execution authorization consume error = %v", err)
	}

	var issuedAuditCount, consumedAuditCount, leakedSecretCount int64
	if err := db.Table("system.audit_logs").Where("event_name = ?", "iam.execution_authorization.issued").Count(&issuedAuditCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("system.audit_logs").Where("event_name = ?", "iam.execution_authorization.consumed").Count(&consumedAuditCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("system.audit_logs").
		Where("event_name LIKE ? AND details::text LIKE ?", "iam.execution_authorization.%", "%addp_at_%").
		Count(&leakedSecretCount).Error; err != nil {
		t.Fatal(err)
	}
	if issuedAuditCount != 2 || consumedAuditCount != 2 || leakedSecretCount != 0 {
		t.Fatalf("audit issued=%d consumed=%d leaked=%d", issuedAuditCount, consumedAuditCount, leakedSecretCount)
	}
}
