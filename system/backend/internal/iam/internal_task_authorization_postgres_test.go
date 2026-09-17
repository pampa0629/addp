package iam

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/schema"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestInternalTaskAuthorizationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("System IAM PostgreSQL gate only")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := schema.InitializeCommon(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	repo := NewRepository(db)
	service, err := NewExecutionAuthorizationService(repo)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := NewTokenFamilyService(repo, BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	audit := AuditMetadata{RequestID: stringPointer("internal-task-test")}
	user := createContextSelectionUser(t, ctx, NewIdentityService(repo, time.Now), "ontology-publisher", audit)
	members := NewTenantMembershipService(repo, time.Now)
	tenant := createContextSelectionTenant(t, ctx, members, "ontology-auth", audit)
	membership := establishContextSelectionMembership(t, ctx, members, tenant.ID, user.PrincipalID, audit)
	role, err := NewTenantRoleService(repo, time.Now).CreateRole(ctx, CreateTenantRoleInput{
		TenantID: tenant.ID, RoleKey: "tenant.ontology_publisher", Name: "Ontology Publisher", ScopeTypes: []string{"tenant"},
		PermissionKeys: []string{"ontology.revision.publish", "system.execution_authorization.create"}, ActorPrincipalID: user.PrincipalID, Audit: audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	insertRoleAssignment(t, db, user.PrincipalID, role.RoleKey, "tenant", &tenant.ID, nil, nil, time.Now().Add(-time.Minute), nil, "manual")
	session, err := selection.BeginContextSelection(ctx, BeginContextSelectionInput{PrincipalID: user.PrincipalID,
		Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: time.Now()}, Audit: audit})
	if err != nil || session.Session == nil {
		t.Fatalf("session: %v", err)
	}
	var principal Principal
	if err := db.First(&principal, user.PrincipalID).Error; err != nil {
		t.Fatal(err)
	}
	var runtimeID int64
	if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-ontology'").Scan(&runtimeID).Error; err != nil {
		t.Fatal(err)
	}
	establishContextSelectionMembership(t, ctx, members, tenant.ID, runtimeID, audit)
	insertRoleAssignment(t, db, runtimeID, "tenant.ontology_runtime", "tenant", &tenant.ID, nil, nil, time.Now().Add(-time.Minute), nil, "bootstrap")
	hash, err := bcrypt.GenerateFromPassword([]byte("internal-task-test-secret-32-bytes"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?, status='active' WHERE client_id='addp-ontology'", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	serviceType, tenantType := PrincipalTypeServicePrincipal, ContextTypeTenant
	runtimeAudit := AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &serviceType, ContextType: &tenantType, TenantID: &tenant.ID}
	scope := execution.InternalTaskScope{TaskType: "semantic_projection", ResourceID: "beijing_outdoor", Revision: "1", Digest: strings.Repeat("a", 64), Generation: uuid.NewString()}
	newRequest := func() IssueExecutionAuthorizationInput {
		id := uuid.New()
		job := &execution.TaskExecution{ExecutionID: id.String(), TenantID: int(tenant.ID), Module: "ontology", Source: "ontology", TaskType: scope.TaskType,
			Status: execution.ExecutionStatusPending, TriggerType: execution.TriggerTypeManual, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 3,
			ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &membership.Membership.ID, IssuedAuthorizationVersion: &principal.AuthorizationVersion,
			ExecutionConfig: commonmodels.JSONMap{"ontology_id": scope.ResourceID, "revision": scope.Revision, "digest": scope.Digest, "generation": scope.Generation},
			Metadata:        commonmodels.JSONMap{}, ErrorDetails: commonmodels.JSONMap{}}
		if err := execution.NewTaskExecutionRepository(db).Create(ctx, job); err != nil {
			t.Fatal(err)
		}
		copy := scope
		return IssueExecutionAuthorizationInput{SourceAccessToken: session.Session.AccessToken, Audience: "ontology", ExecutionID: id, InternalTask: &copy, Audit: audit}
	}
	request := newRequest()
	if _, err := resolveDelegationSourceAccessTokenSnapshot(ctx, repo, session.Session.AccessToken); err != nil {
		t.Fatalf("source credential validation: %#v", err)
	}
	issued, err := service.Issue(ctx, request)
	if err != nil || issued.InternalTask == nil || *issued.InternalTask != scope || len(issued.Accesses) != 0 {
		t.Fatalf("issue: %+v %#v", issued, err)
	}
	var saved ExecutionAuthorization
	if err := db.First(&saved, issued.ID).Error; err != nil || saved.SealedAt == nil || saved.InternalTask == nil {
		t.Fatalf("seal: %v", err)
	}
	for _, query := range []string{
		"UPDATE system.execution_authorizations SET internal_task=jsonb_set(internal_task,'{revision}','\"2\"') WHERE id=?",
		"UPDATE system.execution_authorizations SET internal_task=NULL WHERE id=?",
		"UPDATE system.execution_authorizations SET audience='develop' WHERE id=?",
		"INSERT INTO system.execution_authorization_engine_accesses(authorization_id,engine_id,effects) VALUES (?,1,ARRAY['read'])",
	} {
		if err := db.Exec(query, issued.ID).Error; err == nil {
			t.Fatalf("mutation accepted: %s", query)
		}
	}
	t.Run("scope_and_provenance_rejected", func(t *testing.T) {
		for _, mutate := range []func(*IssueExecutionAuthorizationInput){
			func(r *IssueExecutionAuthorizationInput) { r.InternalTask = nil },
			func(r *IssueExecutionAuthorizationInput) {
				r.Accesses = []ExecutionEngineAccessScope{{EngineID: 1, Effects: []string{"read"}}}
			},
			func(r *IssueExecutionAuthorizationInput) { r.InternalTask.Revision = "2" },
			func(r *IssueExecutionAuthorizationInput) { r.ExecutionID = uuid.New() },
			func(r *IssueExecutionAuthorizationInput) { r.Audience = "develop" },
		} {
			r := newRequest()
			mutate(&r)
			if _, err := service.Issue(ctx, r); err == nil {
				t.Fatal("invalid scope accepted")
			}
		}
	})
	t.Run("concurrent_issue_once", func(t *testing.T) {
		r := newRequest()
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := service.Issue(ctx, r); results <- err }()
		}
		wg.Wait()
		close(results)
		ok, conflict := 0, 0
		for err := range results {
			if err == nil {
				ok++
			} else if errors.Is(err, ErrExecutionAuthorizationConflict) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("outcomes %d/%d", ok, conflict)
		}
	})
	t.Run("audit_failure_rolls_back_grant", func(t *testing.T) {
		r := newRequest()
		if err := db.Exec(`CREATE FUNCTION system.fail_internal_grant_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_name='iam.execution_authorization.issued' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END; $$;
		CREATE TRIGGER fail_internal_grant_audit BEFORE INSERT ON system.audit_logs FOR EACH ROW EXECUTE FUNCTION system.fail_internal_grant_audit()`).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Exec("DROP TRIGGER fail_internal_grant_audit ON system.audit_logs; DROP FUNCTION system.fail_internal_grant_audit()").Error; err != nil {
				t.Error(err)
			}
		}()
		if _, err := service.Issue(ctx, r); err == nil {
			t.Fatal("failed audit allowed grant")
		}
		var count int64
		if err := db.Model(&ExecutionAuthorization{}).Where("execution_id=?", r.ExecutionID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("grant survived rollback %d %v", count, err)
		}
	})
	leaseToken := uuid.New()
	input := AuthorizeInternalTaskInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, Attempt: 1, LeaseToken: leaseToken, InternalTask: scope,
		ServicePrincipalID: runtimeID, ServiceClientID: "addp-ontology", TenantID: tenant.ID, Audit: runtimeAudit}
	if _, err := service.AuthorizeInternalTask(ctx, input); err == nil {
		t.Fatal("pending execution consumed")
	}
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", issued.ExecutionID).Updates(map[string]any{
		"execution_authorization_id": issued.ID, "authorization_expires_at": issued.ExpiresAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, lease, err := execution.ClaimNext(ctx, tx, execution.ClaimOptions{Module: "ontology", TaskType: scope.TaskType, WorkerID: "ontology-test", LeaseDuration: time.Minute, RequireAuthorization: true})
		if err != nil {
			return err
		}
		if lease == nil || lease.ExecutionID != issued.ExecutionID.String() {
			return errors.New("unexpected claim")
		}
		input.LeaseToken = uuid.MustParse(lease.Token)
		input.Attempt = lease.Attempt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	access, err := service.AuthorizeInternalTask(ctx, input)
	if err != nil || access.Attempt != 1 || !access.ExpiresAt.Before(issued.ExpiresAt) {
		t.Fatalf("consume: %+v %v", access, err)
	}
	t.Run("consumer_boundary_rejected", func(t *testing.T) {
		for _, mutate := range []func(*AuthorizeInternalTaskInput){
			func(i *AuthorizeInternalTaskInput) { i.ServiceClientID = "addp-develop" }, func(i *AuthorizeInternalTaskInput) { i.ServicePrincipalID++ },
			func(i *AuthorizeInternalTaskInput) { i.TenantID++ }, func(i *AuthorizeInternalTaskInput) { i.ExecutionID = uuid.New() },
			func(i *AuthorizeInternalTaskInput) { i.Attempt++ }, func(i *AuthorizeInternalTaskInput) { i.LeaseToken = uuid.New() },
			func(i *AuthorizeInternalTaskInput) { i.InternalTask.Generation = uuid.NewString() },
		} {
			i := input
			mutate(&i)
			if _, err := service.AuthorizeInternalTask(ctx, i); err == nil {
				t.Fatal("changed boundary accepted")
			}
		}
		if _, err := service.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID,
			EngineID: 1, RequiredEffects: []string{"read"}, ServicePrincipalID: runtimeID, ServiceClientID: "addp-ontology", TenantID: tenant.ID, Audit: runtimeAudit}); err == nil {
			t.Fatal("internal grant accessed engine")
		}
	})
	t.Run("current_lease_and_auth_rechecked", func(t *testing.T) {
		rollback := errors.New("rollback probe")
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("UPDATE system.execution_authorizations SET revoked_at=clock_timestamp(), revoked_reason='test' WHERE id=?", issued.ID).Error; err != nil {
				return err
			}
			probe, _ := NewExecutionAuthorizationService(NewRepository(tx))
			if _, err := probe.AuthorizeInternalTask(ctx, input); err == nil {
				t.Error("revoked authorization accepted")
			}
			return rollback
		}); !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", issued.ExecutionID).Update("lease_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := service.AuthorizeInternalTask(ctx, input); err == nil {
			t.Fatal("expired lease accepted")
		}
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", issued.ExecutionID).Update("lease_expires_at", time.Now().Add(time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("UPDATE system.principals SET authorization_version=authorization_version+1 WHERE id=?", user.PrincipalID).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := service.AuthorizeInternalTask(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("stale version: %v", err)
		}
	})
	var auditCount int64
	if err := db.Table("system.audit_logs").Where("entity_type='execution_authorization' AND entity_id=?", strconv.FormatInt(issued.ID, 10)).Count(&auditCount).Error; err != nil || auditCount < 2 {
		t.Fatalf("audit count %d %v", auditCount, err)
	}
	encoded, _ := json.Marshal(access)
	if strings.Contains(string(encoded), "lease_token") || strings.Contains(string(encoded), "password") {
		t.Fatal("credential leaked")
	}
}
