package iam

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Composed into the registered Cleanup authorization T2, with its existing DB
// lifecycle. No test-only database, role grant, or connection persists outside it.
func testCleanupEngineConnectionAgainstPostgres(t *testing.T, ctx context.Context, db *gorm.DB, role func(string, ...string) string) {
	repo := NewRepository(db)
	service, _ := NewExecutionAuthorizationService(repo)
	audit := AuditMetadata{RequestID: stringPointer("cleanup-connection-test")}
	user := createContextSelectionUser(t, ctx, NewIdentityService(repo, nil), "cleanup-connection-user", audit)
	members := NewTenantMembershipService(repo, testFixtureDatabaseClock(t, db))
	tenant := createContextSelectionTenant(t, ctx, members, "cleanup-connection", audit)
	member := establishContextSelectionMembership(t, ctx, members, tenant.ID, user.PrincipalID, audit)
	now, err := execution.DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	readRole := role("tenant.cleanup_connection_result_test", "manager.cleanup_result.read", "transfer.cleanup_result.read")
	insertRoleAssignment(t, db, user.PrincipalID, "tenant.engine_lifecycle_operator", "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), nil, "manual")
	insertRoleAssignment(t, db, user.PrincipalID, readRole, "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), nil, "manual")
	var actor Principal
	if err := db.First(&actor, user.PrincipalID).Error; err != nil {
		t.Fatal(err)
	}
	permissions, err := repo.ListEffectiveRoleAssignmentPermissions(ctx, user.PrincipalID, PrincipalTypeUser, ContextTypeTenant, &tenant.ID, &member.Membership.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, row := range permissions {
		keys = append(keys, row.PermissionKey)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "manager.cleanup_result.read,system.cleanup.read,system.engine.create,system.engine.delete,system.engine.read,transfer.cleanup_result.read" {
		t.Fatal("connection purpose widened the six User permissions", keys)
	}
	var runtimeID int64
	if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-transfer'").Scan(&runtimeID).Error; err != nil || runtimeID == 0 {
		t.Fatal("Transfer runtime missing", err)
	}
	establishContextSelectionMembership(t, ctx, members, tenant.ID, runtimeID, audit)
	insertRoleAssignment(t, db, runtimeID, "tenant.transfer_runtime", "tenant", &tenant.ID, nil, nil, now.Add(-time.Minute), nil, "bootstrap")
	hash, _ := bcrypt.GenerateFromPassword([]byte("cleanup-connection-test-secret-long"), bcrypt.MinCost)
	if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?,status='active' WHERE client_id='addp-transfer'", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	serviceType, tenantType := PrincipalTypeServicePrincipal, ContextTypeTenant
	runtimeAudit := AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &serviceType, ContextType: &tenantType, TenantID: &tenant.ID}
	type fixture struct {
		parent, child execution.TaskExecution
		parentLease   execution.Lease
		input         AuthorizeCleanupInput
	}
	prepare := func() fixture {
		t.Helper()
		now, err := execution.DatabaseNow(db)
		if err != nil {
			t.Fatal(err)
		}
		by := int(actor.ID)
		parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant.ID), Module: "system", Source: "system", TaskType: "cleanup", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", TriggeredBy: &by, ActorPrincipalID: &actor.ID, ActorTenantMembershipID: &member.Membership.ID, IssuedAuthorizationVersion: &actor.AuthorizationVersion, MaxAttempts: 3, ExecutionConfig: commonmodels.JSONMap{execution.DisplayLanguageKey: "en"}}
		request := events.CleanupRequest{RequestID: uuid.NewString(), ParentExecutionID: parent.ExecutionID, TenantID: uint(tenant.ID), RequestedBy: uint(actor.ID), Action: "execute", CleanupMode: events.CleanupModePhysical, TriggerType: "manual", CauseEvent: events.CleanupCauseEngineDeleting, ExpectedModules: []string{"transfer"}, BasedOnScan: uuid.NewString(), Context: events.CleanupContext{EngineID: 9, ExternalArtifactPolicy: "delete"}, Confirmation: events.CleanupConfirmation{Confirmed: true, TextProvided: true, RiskLevel: "low", ImpactDigest: strings.Repeat("a", 64)}, RequestedAt: now, Deadline: now.Add(time.Minute)}
		if err := request.Seal(); err != nil {
			t.Fatal(err)
		}
		children, err := execution.CreateCleanupExecutions(ctx, db, &parent, request)
		if err != nil {
			t.Fatal(err)
		}
		child := children[0]
		grant, err := service.IssueCleanup(ctx, uuid.MustParse(child.ExecutionID), audit)
		if err != nil {
			t.Fatal(err)
		}
		identity := func(e execution.TaskExecution) execution.PendingIdentity {
			return execution.PendingIdentity{ExecutionID: e.ExecutionID, TenantID: e.TenantID, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
		}
		_, parentLease, err := execution.ClaimPending(ctx, db, identity(parent), execution.ClaimOptions{Module: "system", TaskType: "cleanup", Source: "system", WorkerID: "connection-parent", LeaseDuration: time.Minute}, nil)
		if err != nil || parentLease == nil {
			t.Fatal("parent claim", err)
		}
		_, childLease, err := execution.ClaimCleanupChild(ctx, db, identity(child), execution.ClaimOptions{Module: "transfer", TaskType: "cleanup_executor", Source: "system", WorkerID: "connection-owner", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
		if err != nil || childLease == nil {
			t.Fatal("child claim", err)
		}
		return fixture{parent: parent, child: child, parentLease: *parentLease, input: AuthorizeCleanupInput{AuthorizationID: grant.ID, ExecutionID: grant.ExecutionID, Stage: "effect", Attempt: childLease.Attempt, LeaseToken: uuid.MustParse(childLease.Token), Scope: *grant.CleanupScope, ServicePrincipalID: runtimeID, ServiceClientID: "addp-transfer", TenantID: tenant.ID, Audit: runtimeAudit}}
	}
	f := prepare()
	loads := 0
	read := func(_ context.Context, tx *gorm.DB, engine, ownerTenant uint) error {
		loads++
		if tx == nil || engine != 9 || ownerTenant != uint(tenant.ID) {
			t.Fatal("engine callback lost exact authorized boundary")
		}
		return nil
	}
	result, err := service.AuthorizeCleanupEngineConnection(ctx, f.input, 9, read)
	if err != nil || result == nil || loads != 1 {
		t.Fatal("valid six-permission connection decision failed", err)
	}
	var current execution.TaskExecution
	if err := db.Where("execution_id=?", f.child.ExecutionID).First(&current).Error; err != nil || result.Access.ExpiresAt.After(*current.LeaseExpiresAt) {
		t.Fatal("connection exceeded live C", err)
	}
	for _, mutate := range []func(*AuthorizeCleanupInput){
		func(i *AuthorizeCleanupInput) { i.Stage = "claim" },
		func(i *AuthorizeCleanupInput) { i.Scope.Module = "meta" },
		func(i *AuthorizeCleanupInput) { i.ServiceClientID = "addp-meta" },
		func(i *AuthorizeCleanupInput) { i.Attempt++ },
		func(i *AuthorizeCleanupInput) { i.LeaseToken = uuid.New() },
		func(i *AuthorizeCleanupInput) { i.Scope.RequestDigest = strings.Repeat("b", 64) },
		func(i *AuthorizeCleanupInput) { i.ExecutionID = uuid.New() },
	} {
		input := f.input
		mutate(&input)
		before := loads
		if _, err := service.AuthorizeCleanupEngineConnection(ctx, input, 9, read); err == nil || loads != before {
			t.Fatal("foreign purpose reached credential callback")
		}
	}
	if _, err := service.AuthorizeCleanupEngineConnection(ctx, f.input, 10, read); err == nil || loads != 1 {
		t.Fatal("foreign Engine reached credential callback")
	}
	rollback := errors.New("rollback connection fixture")
	for _, sql := range []string{
		"UPDATE system.principals SET authorization_version=authorization_version+1 WHERE id=" + strconv.FormatInt(actor.ID, 10),
		"UPDATE system.tenants SET status='closed' WHERE id=" + strconv.FormatInt(tenant.ID, 10),
		"UPDATE system.execution_authorizations SET revoked_at=clock_timestamp(),revoked_reason='fixture' WHERE id=" + strconv.FormatInt(f.input.AuthorizationID, 10),
		"UPDATE common.task_executions SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE execution_id='" + f.parent.ExecutionID + "'",
		"UPDATE common.task_executions SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE execution_id='" + f.child.ExecutionID + "'",
	} {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(sql).Error; err != nil {
				return err
			}
			probe, _ := NewExecutionAuthorizationService(NewRepository(tx))
			before := loads
			if _, err := probe.AuthorizeCleanupEngineConnection(ctx, f.input, 9, read); err == nil || loads != before {
				t.Fatal("revoked or expired scope reached connection")
			}
			return rollback
		}); !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := execution.CompleteWithLease(ctx, tx, f.parentLease, "timeout", nil); err != nil {
			return err
		}
		probe, _ := NewExecutionAuthorizationService(NewRepository(tx))
		before := loads
		if _, err := probe.AuthorizeCleanupEngineConnection(ctx, f.input, 9, read); err == nil || loads != before {
			t.Fatal("closed P issued connection")
		}
		if _, err := probe.AuthorizeCleanup(ctx, f.input); err != nil {
			t.Fatal("new connection purpose changed ordinary effect convergence", err)
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	// Read/decrypt may itself wait. No connection may escape after P expires.
	now, _ = execution.DatabaseNow(db)
	until := now.Add(500 * time.Millisecond)
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", f.parent.ExecutionID).Update("lease_expires_at", until).Error; err != nil {
		t.Fatal(err)
	}
	lateCalled := false
	lateRead := func(context.Context, *gorm.DB, uint, uint) error {
		lateCalled = true
		for {
			wall, err := execution.DatabaseNow(db)
			if err != nil {
				return err
			}
			if !wall.Before(until) {
				return nil
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if _, err := service.AuthorizeCleanupEngineConnection(ctx, f.input, 9, lateRead); err == nil || !lateCalled {
		t.Fatal("connection returned after P expired during read")
	}
	var logs []AuditLog
	if err := db.Where("entity_type='execution_authorization' AND entity_id=?", strconv.FormatInt(f.input.AuthorizationID, 10)).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(logs)
	if !strings.Contains(string(encoded), execution.CleanupEngineConnectionPurpose) || strings.Contains(string(encoded), f.input.LeaseToken.String()) || strings.Contains(string(encoded), "password") {
		t.Fatal("connection audit missing purpose or retained private material")
	}
	if !errors.Is(ErrExecutionAuthorizationPermissionDenied, commonapi.ErrForbidden) {
		t.Fatal("denial lost standard classification")
	}
}
