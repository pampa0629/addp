package iam

import (
	"context"
	"errors"
	"fmt"
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

// Uses the ordinary service and real, issued first-party credentials. No local
// opt-in: this destructive test is discovered by the standard Hosted PG gate.
func TestEmergencySessionRevocationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(12)
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	identity := NewIdentityService(repo, nil)
	members := NewTenantMembershipService(repo, nil)
	tokens, err := NewTokenFamilyService(repo, BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewEmergencySessionService(repo)
	audit := AuditMetadata{RequestID: stringPointer("emergency-pg")}
	adminIDs := map[string]int64{}
	for _, key := range bootstrapRoleOrder {
		u := createGovernedManagementUser(t, ctx, identity, "emergency-"+key, key, audit, db)
		createManagementMFACredential(t, ctx, repo, u.PrincipalID)
		adminIDs[key] = u.PrincipalID
	}
	issue := func(pid int64, tenant *Tenant, membership *TenantMembership, step bool) *IssuedBrowserSession {
		t.Helper()
		p, err := repo.GetPrincipal(ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		auth := SessionAuthentication{Methods: []string{"password", "totp"}, AssuranceLevel: AssuranceLevelAAL2, AuthenticatedAt: time.Now().UTC().Add(-time.Minute)}
		if step {
			until := time.Now().UTC().Add(4 * time.Minute)
			auth.StepUpExpiresAt = &until
		}
		scope := ResolvedSessionContext{Type: ContextTypePlatform}
		if tenant != nil {
			scope = ResolvedSessionContext{Type: ContextTypeTenant, TenantID: &tenant.ID, TenantMembershipID: &membership.ID}
		}
		var result *IssuedBrowserSession
		if err := repo.Transaction(ctx, func(tx *Repository) error {
			var err error
			result, err = tokens.issueBrowserSessionTx(ctx, tx, browserSessionIssueInput{Principal: p, Context: scope, Authentication: auth, Mode: BrowserSessionIssueModeDirect, Audit: audit})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return result
	}
	adminTokens := map[string]*IssuedBrowserSession{}
	for key, id := range adminIDs {
		adminTokens[key] = issue(id, nil, nil, true)
	}
	sysToken := adminTokens["platform.system_administrator"].AccessToken
	secToken := adminTokens["platform.security_administrator"].AccessToken
	audToken := adminTokens["platform.audit_administrator"].AccessToken
	type fixture struct {
		tenant                            *Tenant
		executor, target                  int64
		executorM, targetM                *TenantMembership
		exec, targetSession, otherSession *IssuedBrowserSession
	}
	newFixture := func(name string) fixture {
		t.Helper()
		tenant := createUninitializedTenantFixture(t, ctx, repo, "emergency-"+name, "Emergency "+name, "")
		executor := createContextSelectionUser(t, ctx, identity, "emergency-exec-"+name, audit)
		target := createContextSelectionUser(t, ctx, identity, "emergency-target-"+name, audit)
		em := establishContextSelectionMembership(t, ctx, members, tenant.ID, executor.PrincipalID, audit).Membership
		tm := establishContextSelectionMembership(t, ctx, members, tenant.ID, target.PrincipalID, audit).Membership
		roles := NewTenantRoleService(repo, nil)
		role, err := roles.CreateRole(ctx, CreateTenantRoleInput{TenantID: tenant.ID, RoleKey: "custom.emergency_executor", Name: "Emergency entrance only", PermissionKeys: []string{emergencyPermissionPrefix + "create", emergencyPermissionPrefix + "read", emergencyPermissionPrefix + "cancel", emergencyPermissionPrefix + "execute"}, ActorPrincipalID: executor.PrincipalID, Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		until := time.Now().UTC().Add(time.Minute)
		if _, err := roles.CreateAssignments(ctx, CreateTenantRoleAssignmentsInput{TenantID: tenant.ID, MembershipID: em.ID, RoleIDs: []int64{role.ID}, Reason: "explicit emergency entrance", ActorPrincipalID: executor.PrincipalID, AssuranceLevel: AssuranceLevelAAL2, StepUpExpiresAt: &until, Audit: audit}); err != nil {
			t.Fatal(err)
		}
		return fixture{tenant: tenant, executor: executor.PrincipalID, target: target.PrincipalID, executorM: &em, targetM: &tm, exec: issue(executor.PrincipalID, tenant, &em, true), targetSession: issue(target.PrincipalID, tenant, &tm, false), otherSession: issue(target.PrincipalID, tenant, &tm, false)}
	}
	create := func(f fixture) *EmergencySessionRequest {
		t.Helper()
		r, err := service.Create(ctx, CreateEmergencySessionInput{ID: uuid.New(), AccessToken: f.exec.AccessToken, TargetPrincipalID: f.target, TargetFamilyID: f.targetSession.FamilyID, Reason: "suspicious browser session", IncidentReference: "INC-PG-EMERGENCY", Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	mutate := func(r *EmergencySessionRequest, token, action string, execution uuid.UUID) (*EmergencySessionRequest, error) {
		return service.Mutate(ctx, MutateEmergencySessionInput{ID: r.ID, Version: r.Version, AccessToken: token, Reason: "independent review", ExecutionID: execution, Audit: audit}, action)
	}
	approve := func(r *EmergencySessionRequest) *EmergencySessionRequest {
		t.Helper()
		r, err := mutate(r, sysToken, "approve", uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		r, err = mutate(r, secToken, "approve", uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	isActive := func(session *IssuedBrowserSession) bool {
		f, err := repo.GetRefreshTokenFamily(ctx, session.FamilyID)
		if err != nil {
			t.Fatal(err)
		}
		return f.RevokedAt == nil
	}
	auditCount := func(id uuid.UUID) int64 {
		var count int64
		if err := db.Model(&AuditLog{}).Where("entity_id=? AND event_name LIKE 'iam.emergency_session_revocation.%'", id.String()).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}

	t.Run("two_independent_approvals_and_exact_single_family", func(t *testing.T) {
		f := newFixture("exact")
		r := create(f)
		if r.TenantName == "" || r.ExecutorName == "" || r.TargetName == "" || r.ApprovalExpiresAt.Sub(r.RequestedAt) != 10*time.Minute {
			t.Fatal("missing readable binding or incorrect approval deadline")
		}
		if _, err := mutate(r, f.exec.AccessToken, "execute", uuid.New()); err == nil {
			t.Fatal("unapproved execution succeeded")
		}
		if _, err := mutate(r, audToken, "approve", uuid.Nil); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("audit administrator gained approval authority: %v", err)
		}
		r, err = mutate(r, sysToken, "approve", uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mutate(r, sysToken, "approve", uuid.Nil); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("same person approved twice: %v", err)
		}
		if _, err := mutate(r, f.exec.AccessToken, "execute", uuid.New()); err == nil {
			t.Fatal("one approval allowed execution")
		}
		r, err = mutate(r, secToken, "approve", uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		if r.ExecuteBefore.Sub(*r.ApprovedAt) != 15*time.Minute || len(r.Approvals) != 2 {
			t.Fatal("incorrect execution window or approval count")
		}
		before, err := repo.GetPrincipal(ctx, f.target)
		if err != nil {
			t.Fatal(err)
		}
		delegated := insertDelegatedToken(t, db, readAccessTokenByHash(t, db, f.targetSession.AccessToken), time.Now().UTC(), "emergency-target")
		execution := uuid.New()
		r, err = mutate(r, f.exec.AccessToken, "execute", execution)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "executed" || r.Outcome == nil || *r.Outcome != "revoked" || r.ExecutionID == nil || *r.ExecutionID != execution {
			t.Fatal("missing immutable execution receipt")
		}
		if isActive(f.targetSession) {
			t.Fatal("target family not revoked")
		}
		for _, table := range []string{"system.access_tokens", "system.refresh_tokens", "system.resource_access_tickets"} {
			if countActiveFamilyRows(t, db, table, f.targetSession.FamilyID) != 0 {
				t.Fatal("target family derivative remained active")
			}
		}
		assertDelegatedTokenRevoked(t, db, delegated)
		if !isActive(f.otherSession) || !isActive(f.exec) {
			t.Fatal("unrelated family revoked")
		}
		resolver, _ := NewAuthContextService(repo)
		if _, err := resolver.ResolveFirstPartyAccessToken(ctx, f.targetSession.AccessToken); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("revoked access token still usable: %v", err)
		}
		if _, err := resolver.ResolveFirstPartyAccessToken(ctx, f.otherSession.AccessToken); err != nil {
			t.Fatalf("other session changed: %v", err)
		}
		after, _ := repo.GetPrincipal(ctx, f.target)
		if before.AuthorizationVersion != after.AuthorizationVersion {
			t.Fatal("single-family action changed global authorization version")
		}
		count := auditCount(r.ID)
		replayed, err := mutate(r, f.exec.AccessToken, "execute", execution)
		if err != nil || replayed.Version != r.Version || auditCount(r.ID) != count {
			t.Fatalf("same execution was not read-only history: %v", err)
		}
		if _, err := mutate(r, f.exec.AccessToken, "execute", uuid.New()); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("different execution repeated mutation: %v", err)
		}
		if _, err := service.Get(ctx, audToken, r.ID); err != nil {
			t.Fatalf("audit administrator cannot inspect: %v", err)
		}
		if count != 4 {
			t.Fatalf("creation/two approvals/execution audit count=%d", count)
		}
		t.Log("two distinct system/security approvals revoked exactly one tenant first-party Web family; other sessions and global authorization versions are unchanged; execution replay added no writes")
	})
	t.Run("scope_binding_step_up_and_changed_facts", func(t *testing.T) {
		f := newFixture("scope")
		other := newFixture("foreign")
		r := create(f)
		if _, err := service.Get(ctx, other.exec.AccessToken, r.ID); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("cross-tenant disclosure: %v", err)
		}
		if _, err := service.Create(ctx, CreateEmergencySessionInput{ID: uuid.New(), AccessToken: f.exec.AccessToken, TargetPrincipalID: other.target, TargetFamilyID: other.targetSession.FamilyID, Reason: "x", IncidentReference: "INC", Audit: audit}); err == nil {
			t.Fatal("cross-tenant target accepted")
		}
		if _, err := service.Create(ctx, CreateEmergencySessionInput{ID: uuid.New(), AccessToken: f.exec.AccessToken, TargetPrincipalID: f.target, TargetFamilyID: f.exec.FamilyID, Reason: "x", IncidentReference: "INC", Audit: audit}); err == nil {
			t.Fatal("family substitution accepted")
		}
		stale := issue(adminIDs["platform.system_administrator"], nil, nil, false)
		if _, err := mutate(r, stale.AccessToken, "approve", uuid.Nil); !errors.Is(err, ErrStepUpRequired) {
			t.Fatalf("approval without step-up: %v", err)
		}
		r = approve(r)
		noStep := issue(f.executor, f.tenant, f.executorM, false)
		if _, err := mutate(r, noStep.AccessToken, "execute", uuid.New()); !errors.Is(err, ErrStepUpRequired) {
			t.Fatalf("execution without step-up: %v", err)
		}
		if _, err := repo.IncrementPrincipalAuthorizationVersion(ctx, f.target); err != nil {
			t.Fatal(err)
		}
		if _, err := mutate(r, f.exec.AccessToken, "execute", uuid.New()); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("changed target accepted: %v", err)
		}
		if !isActive(f.targetSession) {
			t.Fatal("stale grant changed family")
		}
	})
	t.Run("execution_audit_failure_rolls_back_grant_and_family", func(t *testing.T) {
		f := newFixture("audit")
		r := approve(create(f))
		before := auditCount(r.ID)
		if err := db.Exec(`CREATE FUNCTION system.fail_emergency_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_name='iam.emergency_session_revocation.execute' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER emergency_audit_failure BEFORE INSERT ON system.audit_logs FOR EACH ROW EXECUTE FUNCTION system.fail_emergency_audit()`).Error; err != nil {
			t.Fatal(err)
		}
		_, failure := mutate(r, f.exec.AccessToken, "execute", uuid.New())
		if err := db.Exec(`DROP TRIGGER emergency_audit_failure ON system.audit_logs;DROP FUNCTION system.fail_emergency_audit()`).Error; err != nil {
			t.Fatal(err)
		}
		row, err := repo.emergencyRequest(ctx, r.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if failure == nil || !isActive(f.targetSession) || row.Version != r.Version || row.Status != "approved" || row.ExecutionID != nil || auditCount(r.ID) != before {
			t.Fatal("audit failure did not roll back all persisted effects")
		}
		if _, err := mutate(r, f.exec.AccessToken, "execute", uuid.New()); err != nil {
			t.Fatalf("explicit retry after rollback failed: %v", err)
		}
		t.Log("final execution audit failure rolled back the family revocation, request consumption and receipt; explicit retry succeeded")
	})
	t.Run("concurrent_execution_has_one_winner", func(t *testing.T) {
		f := newFixture("race")
		r := approve(create(f))
		results := make(chan error, 2)
		var wg sync.WaitGroup
		// Hold the actual principal lock so both real requests start before release.
		blocker := db.Begin()
		if blocker.Error != nil {
			t.Fatal(blocker.Error)
		}
		if err := blocker.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE", f.executor).Error; err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := mutate(r, f.exec.AccessToken, "execute", uuid.New())
				results <- err
			}()
		}
		deadline := time.Now().Add(2 * time.Second)
		waiters := int64(0)
		for time.Now().Before(deadline) {
			if err := db.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%system%principals%' AND pid<>pg_backend_pid()").Scan(&waiters).Error; err != nil {
				t.Fatal(err)
			}
			if waiters >= 2 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := blocker.Commit().Error; err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		close(results)
		wins, conflicts := 0, 0
		for err := range results {
			if err == nil {
				wins++
			} else if errors.Is(err, commonapi.ErrConflict) {
				conflicts++
			} else {
				t.Fatalf("unexpected concurrent result: %v", err)
			}
		}
		if waiters < 2 || wins != 1 || conflicts != 1 || auditCount(r.ID) != 4 || isActive(f.targetSession) || !isActive(f.otherSession) {
			t.Fatalf("waiters=%d wins=%d conflicts=%d", waiters, wins, conflicts)
		}
		t.Log("both execution transactions waited on the PostgreSQL lock; exactly one consumed the grant and one conflicted")
	})
	t.Run("immutable_deadlines_expiration_revoke_cancel_and_noop", func(t *testing.T) {
		f := newFixture("lifecycle")
		r := create(f)
		same, err := service.Create(ctx, CreateEmergencySessionInput{ID: r.ID, AccessToken: f.exec.AccessToken, TargetPrincipalID: f.target, TargetFamilyID: f.targetSession.FamilyID, Reason: r.Reason, IncidentReference: r.IncidentReference, Audit: audit})
		if err != nil || !same.ApprovalExpiresAt.Equal(r.ApprovalExpiresAt) {
			t.Fatalf("create replay renewed grant: %v", err)
		}
		if err := db.Exec("UPDATE system.emergency_session_requests SET approval_expires_at=approval_expires_at+interval '1 minute',version=version+1 WHERE id=?", r.ID).Error; err == nil {
			t.Fatal("database accepted deadline extension")
		}
		r = approve(r)
		r, err = mutate(r, secToken, "revoke", uuid.Nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mutate(r, f.exec.AccessToken, "execute", uuid.New()); err == nil {
			t.Fatal("revoked grant executed")
		}
		r = approve(create(f))
		r, err = mutate(r, f.exec.AccessToken, "cancel", uuid.Nil)
		if err != nil || r.Status != "cancelled" {
			t.Fatalf("approved cancellation failed: %v", err)
		}
		r = approve(create(f))
		if err := repo.RevokeTokenFamily(ctx, f.targetSession.FamilyID, time.Now().UTC(), "test_prior_logout"); err != nil {
			t.Fatal(err)
		}
		r, err = mutate(r, f.exec.AccessToken, "execute", uuid.New())
		if err != nil || r.Outcome == nil || *r.Outcome != "already_revoked" {
			t.Fatalf("prior logout not explicit no-op: %v", err)
		}
		// Persist a valid, already-expired history without altering any production clock or bypassing a guard.
		g := newFixture("expired")
		fresh := create(g)
		past := *fresh
		past.ID = uuid.New()
		past.RequestedAt = time.Now().UTC().Add(-time.Hour)
		past.ApprovalExpiresAt = past.RequestedAt.Add(EmergencyApprovalTTL)
		past.Approvals = nil
		if err := db.Create(&past).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := mutate(&past, sysToken, "approve", uuid.Nil); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("expired approval accepted: %v", err)
		}
		a1 := EmergencySessionApproval{RequestID: past.ID, RoleKey: "platform.system_administrator", PrincipalID: adminIDs["platform.system_administrator"], Reason: "historical approval", ApprovedAt: past.RequestedAt.Add(time.Minute)}
		a2 := EmergencySessionApproval{RequestID: past.ID, RoleKey: "platform.security_administrator", PrincipalID: adminIDs["platform.security_administrator"], Reason: "historical approval", ApprovedAt: past.RequestedAt.Add(2 * time.Minute)}
		for _, a := range []*EmergencySessionApproval{&a1, &a2} {
			p, err := repo.GetPrincipal(ctx, a.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			a.AuthorizationVersion = p.AuthorizationVersion
			if err := db.Create(a).Error; err != nil {
				t.Fatal(err)
			}
		}
		approved := a2.ApprovedAt
		deadline := approved.Add(EmergencyExecutionTTL)
		if err := db.Model(&EmergencySessionRequest{}).Where("id=?", past.ID).Updates(map[string]any{"version": 2, "status": "approved", "approved_at": approved, "execute_before": deadline}).Error; err != nil {
			t.Fatal(err)
		}
		past.Version = 2
		if _, err := mutate(&past, g.exec.AccessToken, "execute", uuid.New()); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("expired execution accepted: %v", err)
		}
		if !isActive(g.targetSession) {
			t.Fatal("expiry changed target")
		}
		t.Log("approval expires after ten minutes; a separately approved grant expires after fifteen minutes; no scheduler or renewed window is involved")
	})
	t.Run("execution_lock_wait_cannot_cross_deadline", func(t *testing.T) {
		f := newFixture("wait-expiry")
		source := create(f)
		past := *source
		past.ID = uuid.New()
		past.RequestedAt = time.Now().UTC().Add(-25*time.Minute + 3500*time.Millisecond)
		past.ApprovalExpiresAt = past.RequestedAt.Add(EmergencyApprovalTTL)
		past.Approvals = nil
		if err := db.Create(&past).Error; err != nil {
			t.Fatal(err)
		}
		for index, key := range []string{"platform.system_administrator", "platform.security_administrator"} {
			p, err := repo.GetPrincipal(ctx, adminIDs[key])
			if err != nil {
				t.Fatal(err)
			}
			approvedTime := past.RequestedAt
			if index == 1 {
				approvedTime = past.RequestedAt.Add(10*time.Minute - time.Second)
			}
			a := EmergencySessionApproval{RequestID: past.ID, RoleKey: key, PrincipalID: p.ID, AuthorizationVersion: p.AuthorizationVersion, Reason: "bounded historical fixture", ApprovedAt: approvedTime}
			if err := db.Create(&a).Error; err != nil {
				t.Fatal(err)
			}
		}
		approved := past.RequestedAt.Add(10*time.Minute - time.Second)
		deadline := approved.Add(EmergencyExecutionTTL)
		// Keep both approvals before the exact grant instant and inside the original ten-minute period.
		if err := db.Model(&EmergencySessionRequest{}).Where("id=?", past.ID).Updates(map[string]any{"version": 2, "status": "approved", "approved_at": approved, "execute_before": deadline}).Error; err != nil {
			t.Fatal(err)
		}
		past.Version = 2
		blocker := db.Begin()
		if err := blocker.Exec("SELECT id FROM system.refresh_token_families WHERE id=? FOR UPDATE", f.targetSession.FamilyID).Error; err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 1)
		go func() { _, err := mutate(&past, f.exec.AccessToken, "execute", uuid.New()); results <- err }()
		waiters := int64(0)
		until := time.Now().Add(time.Second)
		for time.Now().Before(until) {
			if err := db.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%refresh_token_families%' AND pid<>pg_backend_pid()").Scan(&waiters).Error; err != nil {
				t.Fatal(err)
			}
			if waiters > 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if delay := time.Until(deadline.Add(30 * time.Millisecond)); delay > 0 {
			time.Sleep(delay)
		}
		if err := blocker.Commit().Error; err != nil {
			t.Fatal(err)
		}
		err := <-results
		row, readErr := repo.emergencyRequest(ctx, past.ID, false)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if waiters == 0 || !errors.Is(err, commonapi.ErrConflict) || !isActive(f.targetSession) || row.Status != "approved" || row.ExecutionID != nil || auditCount(past.ID) != 0 {
			t.Fatalf("deadline after lock wait was not fail-closed: waiters=%d error=%v", waiters, err)
		}
		t.Log("real PostgreSQL target-family lock wait crossed the execution deadline; no revocation, consumption, receipt or audit survived")
	})
	t.Run("changed_approver_invalidates_previous_approval", func(t *testing.T) {
		f := newFixture("changed-reviewer")
		r := approve(create(f))
		if _, err := repo.IncrementPrincipalAuthorizationVersion(ctx, adminIDs["platform.system_administrator"]); err != nil {
			t.Fatal(err)
		}
		if _, err := mutate(r, f.exec.AccessToken, "execute", uuid.New()); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("stale independent approval accepted: %v", err)
		}
		if !isActive(f.targetSession) {
			t.Fatal("changed approval altered target family")
		}
		sysToken = issue(adminIDs["platform.system_administrator"], nil, nil, true).AccessToken
	})
	t.Run("platform_role_target_is_excluded", func(t *testing.T) {
		f := newFixture("platform")
		insertRoleAssignment(t, db, f.target, "platform.audit_administrator", "platform", nil, nil, nil, time.Now().UTC().Add(-time.Second), nil, "bootstrap")
		f.targetSession = issue(f.target, f.tenant, f.targetM, false)
		if _, err := service.Create(ctx, CreateEmergencySessionInput{ID: uuid.New(), AccessToken: f.exec.AccessToken, TargetPrincipalID: f.target, TargetFamilyID: f.targetSession.FamilyID, Reason: "must not enter administrator governance", IncidentReference: "INC", Audit: audit}); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("platform identity accepted: %v", err)
		}
	})
	t.Log(fmt.Sprintf("emergency revocation uses platform governance and an exact tenant-bound family; local services and databases are outside this disposable test"))
}
