package iam

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
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
		CREATE SCHEMA common
		; CREATE TABLE common.task_executions (
			execution_id uuid PRIMARY KEY,
			tenant_id bigint NOT NULL,
			module text NOT NULL,
			parent_execution_id uuid,
			status text NOT NULL,
			attempt integer NOT NULL DEFAULT 0,
			lease_token uuid,
			lease_expires_at timestamptz,
			actor_principal_id bigint,
			actor_tenant_membership_id bigint,
			issued_authorization_version bigint
		)
	`).Error; err != nil {
		t.Fatalf("create execution fixture schema: %v", err)
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
	tenantEngineID := insertEngineFixture(t, db, tenant.ID, "Tenant Engine", "postgresql", map[string]interface{}{
		"host": "tenant-engine", "port": 5432, "database": "tenant",
	}, false)
	builtinEngineID := insertEngineFixture(t, db, nil, "Builtin Engine", "duckdb", map[string]interface{}{
		"protocol": "http", "host": "builtin-engine", "port": 18100,
	}, true)
	foreignEngineID := insertEngineFixture(t, db, tenant.ID+100, "Foreign Engine", "postgresql", map[string]interface{}{
		"host": "foreign-engine", "port": 5432, "database": "foreign",
	}, false)

	executionID := uuid.New()
	issued, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: executionID,
		Accesses: executionAccessScopes([]int64{builtinEngineID, tenantEngineID}, "read"), ExpiresIn: 10 * time.Minute, Audit: audit,
	})
	if err != nil {
		t.Fatalf("issue execution authorization: %v", err)
	}
	if issued.ID <= 0 || issued.TenantID != tenant.ID || issued.TenantMembershipID != membership.Membership.ID ||
		len(issued.Accesses) != 2 || issued.Accesses[0].EngineID != tenantEngineID || issued.Accesses[1].EngineID != builtinEngineID {
		t.Fatalf("issued execution authorization = %#v", issued)
	}
	if _, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: executionID,
		Accesses: executionAccessScopes([]int64{tenantEngineID}, "read"), Audit: audit,
	}); !errors.Is(err, ErrExecutionAuthorizationConflict) {
		t.Fatalf("duplicate execution authorization error = %v", err)
	}
	if _, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: uuid.New(),
		Accesses: executionAccessScopes([]int64{foreignEngineID}, "read"), Audit: audit,
	}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("cross-tenant engine issue error = %v", err)
	}
	if _, err := service.Issue(ctx, IssueExecutionAuthorizationInput{
		SourceAccessToken: selection.Session.AccessToken,
		Audience:          "develop", ExecutionID: uuid.New(),
		Accesses: executionAccessScopes([]int64{tenantEngineID}, "ddl"), Audit: audit,
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
		AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: tenantEngineID,
		RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
		ServiceClientID: "addp-develop", TenantID: tenant.ID, Audit: consumeAudit,
	})
	if err != nil || authorized.EngineID != tenantEngineID {
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
		Accesses: executionAccessScopes([]int64{tenantEngineID}, "read"), ExpiresIn: 10 * time.Minute,
		ServicePrincipalID: developPrincipalID, ServiceClientID: "addp-develop",
		TenantID: tenant.ID, Audit: consumeAudit,
	})
	if err != nil || issuedFromExecution.ActorPrincipalID != issued.ActorPrincipalID ||
		issuedFromExecution.TenantMembershipID != issued.TenantMembershipID ||
		issuedFromExecution.IssuedAuthorizationVersion != issued.IssuedAuthorizationVersion {
		t.Fatalf("issue authorization from parent execution: result=%#v error=%v", issuedFromExecution, err)
	}

	var transferPrincipalID int64
	if err := db.Raw(`
		SELECT service_principal.id
		FROM system.service_principals service_principal
		WHERE service_principal.name = 'addp-transfer'
	`).Scan(&transferPrincipalID).Error; err != nil || transferPrincipalID <= 0 {
		t.Fatalf("find addp-transfer principal: id=%d error=%v", transferPrincipalID, err)
	}
	transferExecutionID := uuid.New()
	transferLeaseToken := uuid.New()
	if err := db.Exec(`
		INSERT INTO common.task_executions (
			execution_id, tenant_id, module, parent_execution_id, status, attempt, lease_token, lease_expires_at,
			actor_principal_id, actor_tenant_membership_id, issued_authorization_version
		) VALUES (?, ?, 'transfer', ?, 'running', 2, ?, NOW() + INTERVAL '5 minutes', ?, ?, ?)
	`, transferExecutionID, tenant.ID, parentExecutionID, transferLeaseToken,
		issued.ActorPrincipalID, issued.TenantMembershipID, issued.IssuedAuthorizationVersion).Error; err != nil {
		t.Fatalf("create leased Transfer execution provenance: %v", err)
	}
	transferAudit := consumeAudit
	transferAudit.PrincipalID = &transferPrincipalID
	issuedFromLease, err := service.IssueFromExecution(ctx, IssueExecutionAuthorizationFromExecutionInput{
		ParentExecutionID: parentExecutionID, Audience: commonExecution.AudienceTransfer,
		ExecutionID: transferExecutionID, Attempt: 2, LeaseToken: transferLeaseToken,
		Accesses: []ExecutionEngineAccessScope{
			{EngineID: tenantEngineID, Effects: []string{"read"}},
			{EngineID: builtinEngineID, Effects: []string{"write"}},
		}, ExpiresIn: 10 * time.Minute,
		ServicePrincipalID: transferPrincipalID, ServiceClientID: "addp-transfer",
		TenantID: tenant.ID, Audit: transferAudit,
	})
	if err != nil || issuedFromLease == nil || issuedFromLease.Audience != commonExecution.AudienceTransfer {
		t.Fatalf("issue authorization from current Transfer lease: result=%#v error=%v", issuedFromLease, err)
	}
	if _, err := service.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: issuedFromLease.ID, ExecutionID: transferExecutionID, EngineID: builtinEngineID,
		RequiredEffects: []string{"write"}, ServicePrincipalID: transferPrincipalID,
		ServiceClientID: "addp-transfer", TenantID: tenant.ID, Audit: transferAudit,
	}); err != nil {
		t.Fatalf("consume current Transfer attempt authorization: %v", err)
	}
	for name, input := range map[string]AuthorizeExecutionEngineAccessInput{
		"source write expansion": {
			AuthorizationID: issuedFromLease.ID, ExecutionID: transferExecutionID, EngineID: tenantEngineID,
			RequiredEffects: []string{"write"}, ServicePrincipalID: transferPrincipalID,
			ServiceClientID: "addp-transfer", TenantID: tenant.ID, Audit: transferAudit,
		},
		"target read expansion": {
			AuthorizationID: issuedFromLease.ID, ExecutionID: transferExecutionID, EngineID: builtinEngineID,
			RequiredEffects: []string{"read"}, ServicePrincipalID: transferPrincipalID,
			ServiceClientID: "addp-transfer", TenantID: tenant.ID, Audit: transferAudit,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, accessErr := service.AuthorizeEngineAccess(ctx, input); !errors.Is(accessErr, ErrExecutionAuthorizationPermissionDenied) {
				t.Fatalf("error=%v, want permission denied", accessErr)
			}
		})
	}
	for name, attemptToken := range map[string]struct {
		attempt int
		token   uuid.UUID
	}{
		"wrong attempt": {attempt: 1, token: transferLeaseToken},
		"wrong token":   {attempt: 2, token: uuid.New()},
	} {
		t.Run(name, func(t *testing.T) {
			if _, issueErr := service.IssueFromExecution(ctx, IssueExecutionAuthorizationFromExecutionInput{
				ParentExecutionID: parentExecutionID, Audience: commonExecution.AudienceTransfer,
				ExecutionID: transferExecutionID, Attempt: attemptToken.attempt, LeaseToken: attemptToken.token,
				Accesses: executionAccessScopes([]int64{tenantEngineID}, "read"), ExpiresIn: time.Minute,
				ServicePrincipalID: transferPrincipalID, ServiceClientID: "addp-transfer",
				TenantID: tenant.ID, Audit: transferAudit,
			}); !errors.Is(issueErr, ErrExecutionAuthorizationUnavailable) {
				t.Fatalf("error=%v, want unavailable", issueErr)
			}
		})
	}
	transferLeaseToken3 := uuid.New()
	if err := db.Exec(`
		UPDATE common.task_executions
		SET attempt = 3, lease_token = ?, lease_expires_at = NOW() + INTERVAL '5 minutes'
		WHERE execution_id = ?
	`, transferLeaseToken3, transferExecutionID).Error; err != nil {
		t.Fatalf("advance Transfer execution attempt: %v", err)
	}
	issuedFromLease3, err := service.IssueFromExecution(ctx, IssueExecutionAuthorizationFromExecutionInput{
		ParentExecutionID: parentExecutionID, Audience: commonExecution.AudienceTransfer,
		ExecutionID: transferExecutionID, Attempt: 3, LeaseToken: transferLeaseToken3,
		Accesses: []ExecutionEngineAccessScope{
			{EngineID: tenantEngineID, Effects: []string{"read"}},
			{EngineID: builtinEngineID, Effects: []string{"write"}},
		}, ExpiresIn: 10 * time.Minute,
		ServicePrincipalID: transferPrincipalID, ServiceClientID: "addp-transfer",
		TenantID: tenant.ID, Audit: transferAudit,
	})
	if err != nil || issuedFromLease3 == nil || issuedFromLease3.ID == issuedFromLease.ID {
		t.Fatalf("issue new authorization for next Transfer attempt: result=%#v error=%v", issuedFromLease3, err)
	}
	if _, err := service.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: issuedFromLease.ID, ExecutionID: transferExecutionID, EngineID: tenantEngineID,
		RequiredEffects: []string{"read"}, ServicePrincipalID: transferPrincipalID,
		ServiceClientID: "addp-transfer", TenantID: tenant.ID, Audit: transferAudit,
	}); !errors.Is(err, ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("stale Transfer attempt authorization error=%v, want unavailable", err)
	}
	if _, err := service.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{
		AuthorizationID: issuedFromLease3.ID, ExecutionID: transferExecutionID, EngineID: tenantEngineID,
		RequiredEffects: []string{"read"}, ServicePrincipalID: transferPrincipalID,
		ServiceClientID: "addp-transfer", TenantID: tenant.ID, Audit: transferAudit,
	}); err != nil {
		t.Fatalf("consume next Transfer attempt authorization: %v", err)
	}
	if err := db.Exec(`
		UPDATE common.task_executions SET lease_expires_at = NOW() - INTERVAL '1 second'
		WHERE execution_id = ?
	`, transferExecutionID).Error; err != nil {
		t.Fatalf("expire Transfer execution lease: %v", err)
	}
	if _, issueErr := service.IssueFromExecution(ctx, IssueExecutionAuthorizationFromExecutionInput{
		ParentExecutionID: parentExecutionID, Audience: commonExecution.AudienceTransfer,
		ExecutionID: transferExecutionID, Attempt: 3, LeaseToken: transferLeaseToken3,
		Accesses: executionAccessScopes([]int64{tenantEngineID}, "read"), ExpiresIn: time.Minute,
		ServicePrincipalID: transferPrincipalID, ServiceClientID: "addp-transfer",
		TenantID: tenant.ID, Audit: transferAudit,
	}); !errors.Is(issueErr, ErrExecutionAuthorizationUnavailable) {
		t.Fatalf("expired lease error=%v, want unavailable", issueErr)
	}
	for name, input := range map[string]AuthorizeExecutionEngineAccessInput{
		"wrong client": {
			AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: tenantEngineID,
			RequiredEffects: []string{"read"}, ServicePrincipalID: developPrincipalID,
			ServiceClientID: "addp-meta", TenantID: tenant.ID, Audit: consumeAudit,
		},
		"effect expansion": {
			AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: tenantEngineID,
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
				Audience:          "develop", ExecutionID: concurrentExecutionID,
				Accesses: executionAccessScopes([]int64{tenantEngineID}, "read"), Audit: audit,
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
		AuthorizationID: issued.ID, ExecutionID: executionID, EngineID: tenantEngineID,
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
	if issuedAuditCount != 2 || consumedAuditCount != 4 || leakedSecretCount != 0 {
		t.Fatalf("audit issued=%d consumed=%d leaked=%d", issuedAuditCount, consumedAuditCount, leakedSecretCount)
	}
}

func executionAccessScopes(engineIDs []int64, effects ...string) []ExecutionEngineAccessScope {
	accesses := make([]ExecutionEngineAccessScope, 0, len(engineIDs))
	for _, engineID := range engineIDs {
		accesses = append(accesses, ExecutionEngineAccessScope{EngineID: engineID, Effects: append([]string(nil), effects...)})
	}
	return accesses
}
