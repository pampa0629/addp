package service

import (
	"context"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/ontology/internal/falkor"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type testProjectionAuthorizer func(context.Context, *repository.ProjectionWork, execution.Lease) (*client.InternalTaskAccess, error)

func (f testProjectionAuthorizer) Authorize(ctx context.Context, w *repository.ProjectionWork, l execution.Lease) (*client.InternalTaskAccess, error) {
	return f(ctx, w, l)
}

type testProjectionGraph struct {
	build  func(context.Context) error
	verify func(context.Context) error
}

func (g testProjectionGraph) Build(ctx context.Context, _ *falkor.Projection) error {
	if g.build != nil {
		return g.build(ctx)
	}
	return ctx.Err()
}
func (g testProjectionGraph) Verify(ctx context.Context, _ *falkor.Projection) error {
	if g.verify != nil {
		return g.verify(ctx)
	}
	return ctx.Err()
}

func testProjectionReceipt(_ context.Context, w *repository.ProjectionWork, l execution.Lease) (*client.InternalTaskAccess, error) {
	return &client.InternalTaskAccess{AuthorizationID: strconv.FormatInt(*w.Execution.ExecutionAuthorizationID, 10), ExecutionID: l.ExecutionID,
		TenantID: strconv.Itoa(l.TenantID), Audience: execution.AudienceOntology, Attempt: l.Attempt, InternalTask: w.Boundary(), ExpiresAt: *w.Execution.AuthorizationExpiresAt}, nil
}

func publishRuntimeFixture(t *testing.T, s *RevisionService, actor models.Actor, d semantic.Definition) *models.Revision {
	t.Helper()
	ctx := context.Background()
	r, err := s.CreateDraft(ctx, actor, d)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Transition(ctx, actor, d.Scope, r.Version, "submit")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Transition(ctx, actor, d.Scope, r.Version, "publish")
	if err != nil {
		t.Fatal(err)
	}
	err = s.AdmitProjection(ctx, actor, d.Scope, r.Version, *r.Generation, "addp_at_fixture", runtimeFixtureIssuer(actor))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func runtimeFixtureIssuer(actor models.Actor) testAuthorizationIssuer {
	return testAuthorizationIssuer(func(_ context.Context, _ string, req client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
		id := uuid.MustParse(req.ExecutionID)
		return &client.IssuedExecutionAuthorization{ID: strconv.FormatUint((binary.BigEndian.Uint64(id[:8])>>1)|1, 10), ExecutionID: req.ExecutionID, Audience: req.Audience, InternalTask: req.InternalTask,
			TenantID: strconv.FormatUint(actor.TenantID, 10), ActorPrincipalID: strconv.FormatInt(actor.PrincipalID, 10), TenantMembershipID: strconv.FormatInt(actor.MembershipID, 10),
			IssuedAuthorizationVersion: strconv.FormatInt(actor.AuthorizationVersion, 10), SourceType: "user", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})
}

func testProjectionRuntime(t *testing.T, db *gorm.DB, s *RevisionService, actor models.Actor) {
	ctx := context.Background()
	repo := repository.NewRevisionRepository(db)
	definition := func() semantic.Definition {
		return testDefinition("runtime_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
	}
	claim := func(r *models.Revision) execution.Lease {
		t.Helper()
		lease, err := repo.ClaimProjection(ctx, "test-runtime", 30*time.Second)
		if err != nil || lease == nil || lease.ExecutionID != *r.BuildExecutionID {
			t.Fatalf("claim: %v %v", lease, err)
		}
		return *lease
	}
	executor := func(graph ProjectionGraph, auth ProjectionAuthorizer) *ProjectionExecutor {
		t.Helper()
		e, err := NewProjectionExecutor(repo, graph, auth)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	assertState := func(r *models.Revision, projection, status string, active *uint64) {
		t.Helper()
		var p models.Projection
		var job execution.TaskExecution
		var head models.Ontology
		if err := db.Where("generation=?", r.Generation).First(&p).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("execution_id=?", r.BuildExecutionID).First(&job).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("tenant_id=? AND ontology_id=?", r.TenantID, r.OntologyID).First(&head).Error; err != nil {
			t.Fatal(err)
		}
		if p.Status != projection || job.Status != status || (active == nil) != (head.ActiveRevision == nil) || (active != nil && *active != *head.ActiveRevision) {
			t.Fatalf("states projection=%s job=%s head=%#v", p.Status, job.Status, head)
		}
	}
	auth := testProjectionAuthorizer(testProjectionReceipt)
	t.Run("activate_and_withdraw", func(t *testing.T) {
		d := definition()
		r := publishRuntimeFixture(t, s, actor, d)
		if _, err := s.Trial(ctx, actor, d.Scope.OntologyID, "valid_date", 1, *r.Generation, 1, nil); !errors.Is(err, repository.ErrNotActive) {
			t.Fatalf("pending trial exposed: %v", err)
		}
		if _, err := s.ListClasses(ctx, actor, d.Scope.OntologyID); !errors.Is(err, repository.ErrNotActive) {
			t.Fatalf("pending projection exposed: %v", err)
		}
		l := claim(r)
		if err := executor(testProjectionGraph{}, auth).Execute(ctx, l); err != nil {
			t.Fatal(err)
		}
		assertState(r, "ready", execution.ExecutionStatusSuccess, &r.Revision)
		directory, err := s.ListClasses(ctx, actor, d.Scope.OntologyID)
		if err != nil || directory.Revision != r.Revision || directory.Generation != *r.Generation || directory.Digest != r.Digest || directory.KnowledgeKind != "native_definition" || len(directory.Classes) != 1 {
			t.Fatalf("directory=%+v error=%v", directory, err)
		}
		semanticContext, err := s.ClassContext(ctx, actor, d.Scope.OntologyID, "activity", directory.Revision, directory.Generation, directory.ActivationVersion)
		if err != nil || len(semanticContext.Rules) != 1 || semanticContext.Rules[0].Basis == "" {
			t.Fatalf("context=%+v error=%v", semanticContext, err)
		}
		testActiveTrial(t, s, actor, directory)
		if _, err := s.ClassContext(ctx, actor, d.Scope.OntologyID, "activity", directory.Revision, directory.Generation, directory.ActivationVersion+1); !errors.Is(err, ErrActivationChanged) {
			t.Fatal(err)
		}
		if _, err := s.ClassContext(ctx, actor, d.Scope.OntologyID, "missing", directory.Revision, directory.Generation, directory.ActivationVersion); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := s.ListClasses(ctx, testActor(actor.TenantID+1), d.Scope.OntologyID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("cross tenant: %v", err)
		}
		if _, err := s.ListClasses(ctx, actor, "missing"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal(err)
		}
		draft := d
		draft.Scope.Revision++
		draft.Classes = []semantic.Class{{ID: "draft_only", Name: "草稿"}}
		draft.Properties, draft.Rules = nil, nil
		if _, err := s.CreateDraft(ctx, actor, draft); err != nil {
			t.Fatal(err)
		}
		stillActive, err := s.ListClasses(ctx, actor, d.Scope.OntologyID)
		if err != nil || stillActive.Revision != directory.Revision || stillActive.Classes[0].ID != "activity" {
			t.Fatalf("draft leaked: %+v %v", stillActive, err)
		}
		for _, statement := range []string{
			"UPDATE ontology.projections SET status='failed' WHERE generation=?",
			"UPDATE ontology.projections SET baseline_version=baseline_version+1 WHERE generation=?",
			"UPDATE ontology.projection_events SET attempt=attempt+1 WHERE generation=?",
			"UPDATE ontology.ontologies SET activation_version=activation_version+1 WHERE active_generation=?",
		} {
			if err := db.Exec(statement, r.Generation).Error; err == nil {
				t.Fatalf("immutable projection state changed: %s", statement)
			}
		}
		if _, err := s.Transition(ctx, actor, d.Scope, r.Version, "withdraw"); err != nil {
			t.Fatal(err)
		}
		assertState(r, "ready", execution.ExecutionStatusSuccess, nil)
		if _, err := s.ListClasses(ctx, actor, d.Scope.OntologyID); !errors.Is(err, repository.ErrNotActive) {
			t.Fatalf("withdrawn definition exposed: %v", err)
		}
		if _, err := s.Trial(ctx, actor, d.Scope.OntologyID, "valid_date", directory.Revision, directory.Generation, directory.ActivationVersion, nil); !errors.Is(err, repository.ErrNotActive) {
			t.Fatalf("withdrawn trial exposed: %v", err)
		}
		var events int64
		db.Model(&models.ProjectionEvent{}).Where("generation=?", r.Generation).Count(&events)
		if events != 2 {
			t.Fatalf("events %d", events)
		}
		if err := executor(testProjectionGraph{}, auth).Execute(ctx, l); err == nil {
			t.Fatal("terminal lease executed again")
		}
	})
	t.Run("failure_preserves_previous_active", func(t *testing.T) {
		d := definition()
		old := publishRuntimeFixture(t, s, actor, d)
		if err := executor(testProjectionGraph{}, auth).Execute(ctx, claim(old)); err != nil {
			t.Fatal(err)
		}
		d.Scope.Revision++
		r := publishRuntimeFixture(t, s, actor, d)
		if err := executor(testProjectionGraph{build: func(context.Context) error { return errors.New("injected graph failure") }}, auth).Execute(ctx, claim(r)); err == nil {
			t.Fatal("build failure accepted")
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, &old.Revision)
	})
	t.Run("authorization_rechecked_and_receipt_bound", func(t *testing.T) {
		for _, stage := range []string{"before", "after", "wrong_receipt"} {
			t.Run(stage, func(t *testing.T) {
				r := publishRuntimeFixture(t, s, actor, definition())
				calls := 0
				builds := 0
				a := testProjectionAuthorizer(func(ctx context.Context, w *repository.ProjectionWork, l execution.Lease) (*client.InternalTaskAccess, error) {
					calls++
					if (stage == "before" && calls == 1) || (stage == "after" && calls == 2) {
						return nil, errors.New("revoked")
					}
					receipt, err := testProjectionReceipt(ctx, w, l)
					if stage == "wrong_receipt" {
						receipt.Attempt++
					}
					return receipt, err
				})
				if err := executor(testProjectionGraph{build: func(context.Context) error { builds++; return nil }}, a).Execute(ctx, claim(r)); err == nil {
					t.Fatal("invalid authorization accepted")
				}
				if (stage == "before" || stage == "wrong_receipt") && builds != 0 {
					t.Fatal("unauthorized graph write")
				}
				assertState(r, "failed", execution.ExecutionStatusFailed, nil)
			})
		}
	})
	t.Run("withdraw_during_build", func(t *testing.T) {
		d := definition()
		r := publishRuntimeFixture(t, s, actor, d)
		g := testProjectionGraph{build: func(context.Context) error {
			_, err := s.Transition(ctx, actor, d.Scope, r.Version, "withdraw")
			return err
		}}
		if err := executor(g, auth).Execute(ctx, claim(r)); err == nil {
			t.Fatal("withdrawn activated")
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, nil)
	})
	t.Run("competing_completions_and_empty_pointer_ABA", func(t *testing.T) {
		d := definition()
		a := publishRuntimeFixture(t, s, actor, d)
		la := claim(a)
		d.Scope.Revision++
		b := publishRuntimeFixture(t, s, actor, d)
		lb := claim(b)
		wa, err := repo.ProjectionWork(ctx, la)
		if err != nil {
			t.Fatal(err)
		}
		ra, _ := testProjectionReceipt(ctx, wa, la)
		wb, err := repo.ProjectionWork(ctx, lb)
		if err != nil {
			t.Fatal(err)
		}
		rb, _ := testProjectionReceipt(ctx, wb, lb)
		if err := repo.BeginProjection(ctx, la, ra); err != nil {
			t.Fatal(err)
		}
		if err := repo.BeginProjection(ctx, lb, rb); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, v := range []struct {
			l execution.Lease
			r *client.InternalTaskAccess
		}{{la, ra}, {lb, rb}} {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; results <- repo.ActivateProjection(ctx, v.l, v.r) }()
		}
		close(start)
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatalf("concurrent activations=%d", success)
		}
		var head models.Ontology
		db.Where("tenant_id=? AND ontology_id=?", d.Scope.TenantID, d.Scope.OntologyID).First(&head)
		winner, loserLease := a, lb
		if *head.ActiveRevision == b.Revision {
			winner, loserLease = b, la
		}
		d.Scope.Revision = winner.Revision
		if _, err := s.Transition(ctx, actor, d.Scope, winner.Version, "withdraw"); err != nil {
			t.Fatal(err)
		}
		loserReceipt := rb
		if loserLease.ExecutionID == la.ExecutionID {
			loserReceipt = ra
		}
		if err := repo.ActivateProjection(ctx, loserLease, loserReceipt); err == nil {
			t.Fatal("empty-pointer ABA accepted")
		}
		if err := repo.FailProjection(ctx, loserLease, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("expired_lease_recovered_without_replay", func(t *testing.T) {
		r := publishRuntimeFixture(t, s, actor, definition())
		l := claim(r)
		w, err := repo.ProjectionWork(ctx, l)
		if err != nil {
			t.Fatal(err)
		}
		receipt, _ := testProjectionReceipt(ctx, w, l)
		if err := repo.BeginProjection(ctx, l, receipt); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", l.ExecutionID).Update("lease_expires_at", gorm.Expr("clock_timestamp()-interval '1 second'")).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.RecoverProjections(ctx); err != nil {
			t.Fatal(err)
		}
		if err := repo.ActivateProjection(ctx, l, receipt); err == nil {
			t.Fatal("stale lease activated")
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, nil)
	})
	t.Run("activation_audit_failure_rolls_back_pointer", func(t *testing.T) {
		if err := db.Exec(`CREATE FUNCTION ontology.test_reject_activation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='activated' THEN RAISE EXCEPTION 'injected activation audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_reject_activation BEFORE INSERT ON ontology.projection_events FOR EACH ROW EXECUTE FUNCTION ontology.test_reject_activation()`).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Exec(`DROP TRIGGER test_reject_activation ON ontology.projection_events; DROP FUNCTION ontology.test_reject_activation()`)
		r := publishRuntimeFixture(t, s, actor, definition())
		if err := executor(testProjectionGraph{}, auth).Execute(ctx, claim(r)); err == nil {
			t.Fatal("failed audit committed")
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, nil)
	})
	t.Run("last_write_fences_lease_expiry_during_activation", func(t *testing.T) {
		if err := db.Exec(`CREATE FUNCTION ontology.test_expire_activation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='activated' THEN UPDATE common.task_executions SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE execution_id=(SELECT execution_id::text FROM ontology.projections WHERE generation=NEW.generation); END IF; RETURN NEW; END $$; CREATE TRIGGER test_expire_activation BEFORE INSERT ON ontology.projection_events FOR EACH ROW EXECUTE FUNCTION ontology.test_expire_activation()`).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Exec(`DROP TRIGGER test_expire_activation ON ontology.projection_events; DROP FUNCTION ontology.test_expire_activation()`)
		r := publishRuntimeFixture(t, s, actor, definition())
		if err := executor(testProjectionGraph{}, auth).Execute(ctx, claim(r)); err == nil {
			t.Fatal("expired final lease activated")
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, nil)
	})
	t.Run("cancelled_build_is_terminal_without_activation", func(t *testing.T) {
		r := publishRuntimeFixture(t, s, actor, definition())
		request, cancel := context.WithCancel(ctx)
		defer cancel()
		g := testProjectionGraph{build: func(context.Context) error { cancel(); return nil }}
		if err := executor(g, auth).Execute(request, claim(r)); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled result: %v", err)
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, nil)
	})
	t.Run("lost_lease_cannot_activate_or_close_another_owner", func(t *testing.T) {
		r := publishRuntimeFixture(t, s, actor, definition())
		l := claim(r)
		g := testProjectionGraph{build: func(context.Context) error {
			return db.Model(&execution.TaskExecution{}).Where("execution_id=?", l.ExecutionID).Update("lease_token", uuid.NewString()).Error
		}}
		if err := executor(g, auth).Execute(ctx, l); err == nil {
			t.Fatal("lost lease activated")
		}
		assertState(r, "building", execution.ExecutionStatusRunning, nil)
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", l.ExecutionID).Update("lease_expires_at", gorm.Expr("clock_timestamp()-interval '1 second'")).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.RecoverProjections(ctx); err != nil {
			t.Fatal(err)
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, nil)
	})
	t.Run("supervisor_readiness_and_shutdown", func(t *testing.T) {
		r := publishRuntimeFixture(t, s, actor, definition())
		var ready atomic.Bool
		supervisor, err := NewProjectionSupervisor(repo, executor(testProjectionGraph{}, auth), "supervisor-fixture")
		if err != nil {
			t.Fatal(err)
		}
		run, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- supervisor.Run(run, ready.Load) }()
		time.Sleep(50 * time.Millisecond)
		assertState(r, "pending", execution.ExecutionStatusPending, nil)
		ready.Store(true)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			var p models.Projection
			db.Where("generation=?", r.Generation).First(&p)
			if p.Status == "ready" {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("supervisor did not stop")
		}
		assertState(r, "ready", execution.ExecutionStatusSuccess, &r.Revision)
	})
	t.Run("supervisor_renews_then_cancels_inflight_work", func(t *testing.T) {
		r := publishRuntimeFixture(t, s, actor, definition())
		started := make(chan time.Time, 1)
		g := testProjectionGraph{build: func(ctx context.Context) error {
			var job execution.TaskExecution
			if err := db.Where("execution_id=?", r.BuildExecutionID).First(&job).Error; err != nil {
				return err
			}
			started <- *job.LeaseExpiresAt
			<-ctx.Done()
			return ctx.Err()
		}}
		supervisor, err := NewProjectionSupervisor(repo, executor(g, auth), "renewal-fixture")
		if err != nil {
			t.Fatal(err)
		}
		run, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- supervisor.Run(run, func() bool { return true }) }()
		var initial time.Time
		select {
		case initial = <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("supervisor did not start work")
		}
		renewed := false
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			var job execution.TaskExecution
			if err := db.Where("execution_id=?", r.BuildExecutionID).First(&job).Error; err != nil {
				t.Fatal(err)
			}
			if job.LeaseExpiresAt != nil && job.LeaseExpiresAt.After(initial.Add(time.Second)) {
				renewed = true
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Fatal("inflight shutdown did not converge")
		}
		if !renewed {
			t.Fatal("lease not renewed during graph work")
		}
		assertState(r, "failed", execution.ExecutionStatusFailed, nil)
	})
}
