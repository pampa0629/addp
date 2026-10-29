package iam

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
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

// Runs only in the standard disposable addp_iam_test gate; never Hosted proof.
func TestCleanupAuthorizationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("System IAM PostgreSQL gate only")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
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
	service, _ := NewExecutionAuthorizationService(repo)
	audit := AuditMetadata{RequestID: stringPointer("cleanup-auth-test")}
	user := createContextSelectionUser(t, ctx, NewIdentityService(repo, nil), "cleanup-user", audit)
	members := NewTenantMembershipService(repo, testFixtureDatabaseClock(t, db))
	tenant := createContextSelectionTenant(t, ctx, members, "cleanup-auth", audit)
	member := establishContextSelectionMembership(t, ctx, members, tenant.ID, user.PrincipalID, audit)
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.infrastructure_administrator", "tenant", &tenant.ID, nil, nil, time.Now().Add(-time.Minute), nil, "bootstrap")
	var principal Principal
	if err := db.First(&principal, user.PrincipalID).Error; err != nil {
		t.Fatal(err)
	}
	var runtimeID int64
	if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-meta'").Scan(&runtimeID).Error; err != nil || runtimeID == 0 {
		t.Fatal("runtime", err)
	}
	establishContextSelectionMembership(t, ctx, members, tenant.ID, runtimeID, audit)
	insertRoleAssignment(t, db, runtimeID, "tenant.meta_runtime", "tenant", &tenant.ID, nil, nil, time.Now().Add(-time.Minute), nil, "bootstrap")
	hash, _ := bcrypt.GenerateFromPassword([]byte("cleanup-test-secret-32-bytes-long"), bcrypt.MinCost)
	if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?,status='active' WHERE client_id='addp-meta'", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	serviceType, tenantType := PrincipalTypeServicePrincipal, ContextTypeTenant
	runtimeAudit := AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &serviceType, ContextType: &tenantType, TenantID: &tenant.ID}
	fixture := func(t *testing.T) (execution.TaskExecution, execution.TaskExecution) {
		t.Helper()
		now, err := execution.DatabaseNow(db)
		if err != nil {
			t.Fatal(err)
		}
		by := int(user.PrincipalID)
		parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant.ID), Module: "system", Source: "system", TaskType: "cleanup", Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: "manual", TriggeredBy: &by, ActorPrincipalID: &user.PrincipalID, ActorTenantMembershipID: &member.Membership.ID, IssuedAuthorizationVersion: &principal.AuthorizationVersion, MaxAttempts: 3, ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
		request := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: uint(tenant.ID), RequestedBy: uint(user.PrincipalID), Action: "scan", TriggerType: "manual", ExpectedModules: []string{"meta"}, RequestedAt: now, Deadline: now.Add(time.Minute)}
		if err := request.Seal(); err != nil {
			t.Fatal(err)
		}
		children, err := execution.CreateCleanupExecutions(ctx, db, &parent, request)
		if err != nil {
			t.Fatal(err)
		}
		return parent, children[0]
	}

	t.Run("catalog_grants_only_reviewed_runtimes", func(t *testing.T) {
		var roles []string
		if err := db.Raw("SELECT r.role_key FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.cleanup_authorization.execute' ORDER BY r.role_key").Scan(&roles).Error; err != nil {
			t.Fatal(err)
		}
		expected := []string{"tenant.asset_runtime", "tenant.develop_runtime", "tenant.graph_runtime", "tenant.manager_runtime", "tenant.meta_runtime", "tenant.model_runtime", "tenant.quality_runtime", "tenant.service_runtime", "tenant.standard_runtime", "tenant.transfer_runtime"}
		if strings.Join(roles, ",") != strings.Join(expected, ",") {
			t.Fatal("unexpected cleanup runtime roles", roles)
		}
		var count int64
		if err := db.Raw("SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='graph.graph.reconcile'").Scan(&count).Error; err != nil || count != 0 {
			t.Fatal("Graph reconciliation granted implicitly", count, err)
		}
	})
	parent, child := fixture(t)
	grant, err := service.IssueCleanup(ctx, uuid.MustParse(child.ExecutionID), audit)
	if err != nil || grant == nil || grant.CleanupScope == nil || grant.SealedAt == nil {
		t.Fatalf("issue: %v", err)
	}
	input := AuthorizeCleanupInput{AuthorizationID: grant.ID, ExecutionID: grant.ExecutionID, Stage: "claim", Scope: *grant.CleanupScope, ServicePrincipalID: runtimeID, ServiceClientID: "addp-meta", TenantID: tenant.ID, Audit: runtimeAudit}
	if _, err := service.AuthorizeCleanup(ctx, input); err != nil {
		t.Fatalf("claim authorization: %v", err)
	}
	t.Run("exact_purpose_and_immutable_binding", func(t *testing.T) {
		if _, err := service.IssueCleanup(ctx, grant.ExecutionID, audit); err == nil {
			t.Fatal("duplicate issue accepted")
		}
		for _, sql := range []string{
			"UPDATE system.execution_authorizations SET cleanup_scope=jsonb_set(cleanup_scope,'{module}','\"graph\"') WHERE id=?",
			"UPDATE system.execution_authorizations SET cleanup_scope=NULL WHERE id=?",
			"UPDATE system.execution_authorizations SET audience='develop' WHERE id=?",
			"INSERT INTO system.execution_authorization_engine_accesses(authorization_id,engine_id,effects) VALUES(?,1,ARRAY['read'])",
		} {
			if err := db.Exec(sql, grant.ID).Error; err == nil {
				t.Fatal("accepted immutable boundary mutation", sql)
			}
		}
		if _, err := service.AuthorizeEngineAccess(ctx, AuthorizeExecutionEngineAccessInput{AuthorizationID: grant.ID, ExecutionID: grant.ExecutionID, EngineID: 1, RequiredEffects: []string{"read"}, ServicePrincipalID: runtimeID, ServiceClientID: "addp-meta", TenantID: tenant.ID, Audit: runtimeAudit}); err == nil {
			t.Fatal("cleanup grant accessed engine")
		}
		for _, mutate := range []func(*AuthorizeCleanupInput){
			func(i *AuthorizeCleanupInput) { i.ServiceClientID = "addp-graph" }, func(i *AuthorizeCleanupInput) { i.ServicePrincipalID++ }, func(i *AuthorizeCleanupInput) { i.TenantID++ }, func(i *AuthorizeCleanupInput) { i.ExecutionID = uuid.New() },
			func(i *AuthorizeCleanupInput) { i.Scope.RequestID = uuid.NewString() }, func(i *AuthorizeCleanupInput) { i.Scope.ParentExecutionID = uuid.NewString() }, func(i *AuthorizeCleanupInput) { i.Scope.RequestDigest = strings.Repeat("f", 64) }, func(i *AuthorizeCleanupInput) { i.Scope.Module = "graph" },
			func(i *AuthorizeCleanupInput) { i.Stage = "effect"; i.Attempt = 1; i.LeaseToken = uuid.New() },
		} {
			probe := input
			mutate(&probe)
			if _, err := service.AuthorizeCleanup(ctx, probe); err == nil {
				t.Fatal("changed boundary accepted")
			}
		}
	})
	t.Run("audit_failure_rolls_back_grant_and_attachment", func(t *testing.T) {
		_, c := fixture(t)
		if err := db.Exec(`CREATE FUNCTION system.fail_cleanup_grant_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_name='iam.execution_authorization.issued' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END; $$;
  CREATE TRIGGER fail_cleanup_grant_audit BEFORE INSERT ON system.audit_logs FOR EACH ROW EXECUTE FUNCTION system.fail_cleanup_grant_audit()`).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Exec("DROP TRIGGER fail_cleanup_grant_audit ON system.audit_logs; DROP FUNCTION system.fail_cleanup_grant_audit()").Error; err != nil {
				t.Error(err)
			}
		}()
		if _, err := service.IssueCleanup(ctx, uuid.MustParse(c.ExecutionID), audit); err == nil {
			t.Fatal("audit failure accepted")
		}
		var count int64
		db.Model(&ExecutionAuthorization{}).Where("execution_id=?", c.ExecutionID).Count(&count)
		var stored execution.TaskExecution
		db.Where("execution_id=?", c.ExecutionID).First(&stored)
		if count != 0 || stored.ExecutionAuthorizationID != nil {
			t.Fatal("partial authority survived failed audit")
		}
	})
	identity := func(e execution.TaskExecution) execution.PendingIdentity {
		return execution.PendingIdentity{ExecutionID: e.ExecutionID, TenantID: e.TenantID, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
	}
	options := func(e execution.TaskExecution) execution.ClaimOptions {
		return execution.ClaimOptions{Module: e.Module, TaskType: e.TaskType, Source: e.Source, WorkerID: "cleanup-auth-test", LeaseDuration: time.Minute}
	}
	_, pl, err := execution.ClaimPending(ctx, db, identity(parent), options(parent), nil)
	if err != nil || pl == nil {
		t.Fatal("parent claim", err)
	}
	co := options(child)
	co.RequireAuthorization = true
	_, cl, err := execution.ClaimCleanupChild(ctx, db, identity(child), co, nil)
	if err != nil || cl == nil {
		t.Fatal("child claim", err)
	}
	input.Stage, input.Attempt, input.LeaseToken = "effect", cl.Attempt, uuid.MustParse(cl.Token)
	if err := execution.CompleteWithLease(ctx, db, *pl, execution.ExecutionStatusTimeout, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.AuthorizeCleanup(ctx, input)
	if err != nil || result == nil || !result.ExpiresAt.Before(grant.ExpiresAt) {
		t.Fatalf("valid admitted child after parent closure: %v", err)
	}
	t.Run("live_revocation_and_expiry_before_effect", func(t *testing.T) {
		rollback := errors.New("rollback authorization probe")
		for _, sql := range []string{
			"UPDATE system.principals SET authorization_version=authorization_version+1 WHERE id=" + strconv.FormatInt(user.PrincipalID, 10),
			"UPDATE system.principals SET status='suspended' WHERE id=" + strconv.FormatInt(user.PrincipalID, 10),
			"UPDATE system.tenant_memberships SET expires_at=clock_timestamp() WHERE id=" + strconv.FormatInt(member.Membership.ID, 10),
			"UPDATE system.tenants SET status='closed' WHERE id=" + strconv.FormatInt(tenant.ID, 10),
			"UPDATE system.role_assignments SET status='revoked',revoked_reason='cleanup authorization probe',revoked_at=clock_timestamp(),revoked_by_principal_id=" + strconv.FormatInt(user.PrincipalID, 10) + " WHERE tenant_id=" + strconv.FormatInt(tenant.ID, 10) + " AND principal_id=" + strconv.FormatInt(user.PrincipalID, 10),
			"UPDATE system.role_assignments SET status='revoked',revoked_reason='cleanup authorization probe',revoked_at=clock_timestamp(),revoked_by_principal_id=" + strconv.FormatInt(user.PrincipalID, 10) + " WHERE tenant_id=" + strconv.FormatInt(tenant.ID, 10) + " AND principal_id=" + strconv.FormatInt(runtimeID, 10),
			"UPDATE system.execution_authorizations SET revoked_at=clock_timestamp(),revoked_reason='test' WHERE id=" + strconv.FormatInt(grant.ID, 10),
			"UPDATE common.task_executions SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE execution_id='" + child.ExecutionID + "'",
		} {
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec(sql).Error; err != nil {
					return err
				}
				probe, _ := NewExecutionAuthorizationService(NewRepository(tx))
				if _, err := probe.AuthorizeCleanup(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
					t.Errorf("effect permitted after authority changed: %s (%v)", sql, err)
				}
				return rollback
			}); !errors.Is(err, rollback) {
				t.Fatal(err)
			}
		}
		for _, mutate := range []func(*AuthorizeCleanupInput){func(i *AuthorizeCleanupInput) { i.Attempt++ }, func(i *AuthorizeCleanupInput) { i.LeaseToken = uuid.New() }, func(i *AuthorizeCleanupInput) { i.Stage = "claim"; i.Attempt = 0; i.LeaseToken = uuid.Nil }} {
			probe := input
			mutate(&probe)
			if _, err := service.AuthorizeCleanup(ctx, probe); err == nil {
				t.Fatal("stale or wrong-stage consumption accepted")
			}
		}
	})

	t.Run("lock_wait_crosses_lease_expiry", func(t *testing.T) {
		locked := db.Begin()
		if locked.Error != nil {
			t.Fatal(locked.Error)
		}
		defer locked.Rollback()
		var expiry time.Time
		if err := locked.Raw("UPDATE common.task_executions SET lease_expires_at=clock_timestamp()+interval '300 milliseconds' WHERE execution_id=? RETURNING lease_expires_at", child.ExecutionID).Scan(&expiry).Error; err != nil {
			t.Fatal(err)
		}
		pid := make(chan int, 1)
		done := make(chan error, 1)
		go func() {
			done <- db.Connection(func(conn *gorm.DB) error {
				var id int
				if err := conn.Raw("SELECT pg_backend_pid()").Scan(&id).Error; err != nil {
					return err
				}
				pid <- id
				probe, _ := NewExecutionAuthorizationService(NewRepository(conn))
				_, err := probe.AuthorizeCleanup(ctx, input)
				return err
			})
		}()
		var id int
		select {
		case id = <-pid:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		deadline := time.Now().Add(3 * time.Second)
		waiting := false
		for time.Now().Before(deadline) {
			if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=? AND wait_event_type='Lock')", id).Scan(&waiting).Error; err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !waiting {
			t.Fatal("authorization never waited for the child lock")
		}
		for {
			now, err := execution.DatabaseNow(db)
			if err != nil {
				t.Fatal(err)
			}
			if !now.Before(expiry) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := locked.Commit().Error; err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("lease expired during wait: %v", err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	})
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), cl.Token) || strings.Contains(string(encoded), "password") {
		t.Fatal("private lease disclosed")
	}
	var audits []AuditLog
	if err := db.Where("entity_type='execution_authorization' AND entity_id=?", strconv.FormatInt(grant.ID, 10)).Find(&audits).Error; err != nil || len(audits) < 2 {
		t.Fatal("missing audits", err)
	}
	encoded, _ = json.Marshal(audits)
	if strings.Contains(string(encoded), cl.Token) {
		t.Fatal("private lease archived in audit")
	}
}
