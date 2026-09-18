package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func testProjectionRebuild(t *testing.T, db *gorm.DB, s *RevisionService, actor models.Actor) {
	ctx := context.Background()
	repo := repository.NewRevisionRepository(db)
	failure := testProjectionGraph{build: func(context.Context) error { return errors.New("graph failure") }}
	execute := func(t *testing.T, executionID string, graph ProjectionGraph) error {
		t.Helper()
		lease, err := repo.ClaimProjection(ctx, "rebuild-fixture", 30*time.Second)
		if err != nil || lease == nil || lease.ExecutionID != executionID {
			t.Fatalf("claim: %v %v", lease, err)
		}
		e, err := NewProjectionExecutor(repo, graph, testProjectionAuthorizer(testProjectionReceipt))
		if err != nil {
			t.Fatal(err)
		}
		return e.Execute(ctx, *lease)
	}
	failed := func(t *testing.T) (*models.Revision, semantic.Scope) {
		t.Helper()
		d := testDefinition("rebuild_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
		r := publishRuntimeFixture(t, s, actor, d)
		if err := execute(t, *r.BuildExecutionID, failure); err == nil {
			t.Fatal("injected failure succeeded")
		}
		return r, d.Scope
	}
	admit := func(t *testing.T, who models.Actor, r *models.Revision, scope semantic.Scope, p *models.Projection) {
		t.Helper()
		if err := s.AdmitProjection(ctx, who, scope, r.Version, p.Generation, "addp_at_fresh", runtimeFixtureIssuer(who)); err != nil {
			t.Fatal(err)
		}
	}
	assertState := func(t *testing.T, p *models.Projection, status, executionStatus string) {
		t.Helper()
		var projection models.Projection
		var job execution.TaskExecution
		if err := db.Where("generation=?", p.Generation).First(&projection).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("execution_id=?", p.ExecutionID).First(&job).Error; err != nil {
			t.Fatal(err)
		}
		if projection.Status != status || job.Status != executionStatus {
			t.Fatalf("state: %s %s", projection.Status, job.Status)
		}
	}
	t.Run("fresh_identity_actor_authorization_and_immutable_revision", func(t *testing.T) {
		r, scope := failed(t)
		who := actor
		who.PrincipalID++
		who.MembershipID++
		who.AuthorizationVersion++
		p, err := s.RebuildProjection(ctx, who, scope, r.Version, *r.Generation, 1)
		if err != nil {
			t.Fatal(err)
		}
		latest, err := s.LatestProjection(ctx, who, scope)
		if err != nil || latest.Generation != p.Generation || latest.PredecessorGeneration == nil || *latest.PredecessorGeneration != *r.Generation {
			t.Fatalf("lost response cannot resolve rebuilt generation: %+v %v", latest, err)
		}
		if p.Generation == *r.Generation || p.ExecutionID == *r.BuildExecutionID || p.PredecessorGeneration == nil || *p.PredecessorGeneration != *r.Generation || p.Digest != r.Digest {
			t.Fatal("rebuild changed semantic identity or reused execution")
		}
		if lease, err := repo.ClaimProjection(ctx, "unauthorized", time.Minute); err != nil || lease != nil {
			t.Fatal("unadmitted rebuild was claimable")
		}
		if err := s.AdmitProjection(ctx, actor, scope, r.Version, p.Generation, "addp_at_old_actor", runtimeFixtureIssuer(actor)); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("old actor admitted rebuild: %v", err)
		}
		admit(t, who, r, scope, p)
		if err := execute(t, p.ExecutionID, testProjectionGraph{}); err != nil {
			t.Fatal(err)
		}
		assertState(t, p, "ready", execution.ExecutionStatusSuccess)
		var head models.Ontology
		if err := db.Where("tenant_id=? AND ontology_id=?", scope.TenantID, scope.OntologyID).First(&head).Error; err != nil {
			t.Fatal(err)
		}
		if head.ActiveGeneration == nil || *head.ActiveGeneration != p.Generation || head.ActiveRevision == nil || *head.ActiveRevision != r.Revision || head.ActivationVersion != 2 {
			t.Fatal("rebuild not activated")
		}
		unchanged, err := s.Get(ctx, actor, scope)
		if err != nil || unchanged.Payload != r.Payload || unchanged.Digest != r.Digest || unchanged.Version != r.Version || *unchanged.Generation != *r.Generation || *unchanged.BuildExecutionID != *r.BuildExecutionID {
			t.Fatal("rebuild rewrote publication")
		}
		var events []models.ProjectionEvent
		if err := db.Where("generation=?", p.Generation).Order("id").Find(&events).Error; err != nil || len(events) != 3 {
			t.Fatalf("rebuild audit: %v %v", events, err)
		}
		for _, e := range events {
			if e.ActorPrincipalID != who.PrincipalID || e.ActorMembershipID != who.MembershipID || e.AuthorizationVersion != who.AuthorizationVersion {
				t.Fatal("audit borrowed original publisher identity")
			}
		}
		if _, err := s.RebuildProjection(ctx, who, scope, r.Version, p.Generation, 2); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("ready projection rebuilt")
		}
		if _, err := s.Transition(ctx, actor, scope, r.Version, "withdraw"); err != nil {
			t.Fatal(err)
		}
		if err := db.Where("tenant_id=? AND ontology_id=?", scope.TenantID, scope.OntologyID).First(&head).Error; err != nil || head.ActiveGeneration != nil {
			t.Fatal("withdraw did not clear rebuilt generation")
		}
	})
	t.Run("exact_scope_baseline_and_no_duplicate_branch", func(t *testing.T) {
		r, scope := failed(t)
		if _, err := s.RebuildProjection(ctx, actor, scope, r.Version, *r.Generation, 2); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("stale activation baseline accepted")
		}
		if _, err := s.RebuildProjection(ctx, actor, scope, r.Version+1, *r.Generation, 1); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("wrong revision version accepted")
		}
		other := actor
		other.TenantID++
		if _, err := s.RebuildProjection(ctx, other, scope, r.Version, *r.Generation, 1); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal("cross-tenant rebuild accepted")
		}
		if _, err := s.RebuildProjection(ctx, actor, scope, r.Version, "invalid", 1); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal("invalid generation accepted")
		}
		var wg sync.WaitGroup
		results := make(chan *models.Projection, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				p, err := s.RebuildProjection(ctx, actor, scope, r.Version, *r.Generation, 1)
				if err != nil && !errors.Is(err, repository.ErrConflict) {
					t.Error(err)
				}
				results <- p
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		var winner *models.Projection
		count := 0
		for p := range results {
			if p != nil {
				winner = p
				count++
			}
		}
		if count != 1 {
			t.Fatalf("concurrent successors=%d", count)
		}
		admit(t, actor, r, scope, winner)
		if err := execute(t, winner.ExecutionID, failure); err == nil {
			t.Fatal("injected failure succeeded")
		}
		duplicate := *winner
		duplicate.Generation, duplicate.ExecutionID = uuid.NewString(), uuid.NewString()
		duplicate.Status = "pending"
		if err := db.Create(&duplicate).Error; err == nil || !strings.Contains(err.Error(), "one_projection_successor") {
			t.Fatalf("database accepted duplicate successor: %v", err)
		}
		if err := db.Model(&models.Projection{}).Where("generation=?", winner.Generation).Update("predecessor_generation", nil).Error; err == nil {
			t.Fatal("database allowed rewriting predecessor")
		}
		if _, err := s.RebuildProjection(ctx, actor, scope, r.Version, *r.Generation, 1); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("historical ancestor fork accepted")
		}
		next, err := s.RebuildProjection(ctx, actor, scope, r.Version, winner.Generation, 1)
		if err != nil {
			t.Fatal(err)
		}
		admit(t, actor, r, scope, next)
		if err := execute(t, next.ExecutionID, testProjectionGraph{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("old_grant_rejected_and_new_admission_failure_is_rebuildable", func(t *testing.T) {
		r, scope := failed(t)
		p, err := s.RebuildProjection(ctx, actor, scope, r.Version, *r.Generation, 1)
		if err != nil {
			t.Fatal(err)
		}
		issuer := testAuthorizationIssuer(func(ctx context.Context, token string, req client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
			grant, err := runtimeFixtureIssuer(actor).Issue(ctx, token, req)
			grant.ExecutionID = *r.BuildExecutionID
			grant.InternalTask.Generation = *r.Generation
			return grant, err
		})
		if err := s.AdmitProjection(ctx, actor, scope, r.Version, p.Generation, "addp_at_old_grant", issuer); err == nil {
			t.Fatal("old grant reused")
		}
		assertState(t, p, "failed", execution.ExecutionStatusFailed)
		next, err := s.RebuildProjection(ctx, actor, scope, r.Version, p.Generation, 1)
		if err != nil {
			t.Fatal(err)
		}
		admit(t, actor, r, scope, next)
		// Late cleanup of the previous intent must not fail the successor.
		if err := repo.FailProjectionAdmission(ctx, actor, p); err != nil {
			t.Fatal(err)
		}
		if err := execute(t, next.ExecutionID, testProjectionGraph{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("activation_change_after_rebuild_request_keeps_new_active", func(t *testing.T) {
		r, scope := failed(t)
		p, err := s.RebuildProjection(ctx, actor, scope, r.Version, *r.Generation, 1)
		if err != nil {
			t.Fatal(err)
		}
		d := testDefinition(scope.OntologyID)
		d.Scope.Revision++
		newer := publishRuntimeFixture(t, s, actor, d)
		if err := execute(t, *newer.BuildExecutionID, testProjectionGraph{}); err != nil {
			t.Fatal(err)
		}
		admit(t, actor, r, scope, p)
		built := false
		if err := execute(t, p.ExecutionID, testProjectionGraph{build: func(context.Context) error { built = true; return nil }}); err == nil || built {
			t.Fatal("obsolete baseline reached graph build")
		}
		assertState(t, p, "failed", execution.ExecutionStatusFailed)
		var head models.Ontology
		if err := db.Where("tenant_id=? AND ontology_id=?", scope.TenantID, scope.OntologyID).First(&head).Error; err != nil {
			t.Fatal(err)
		}
		if head.ActiveGeneration == nil || *head.ActiveGeneration != *newer.Generation {
			t.Fatal("stale rebuild changed active graph")
		}
		if _, err := s.RebuildProjection(ctx, actor, scope, r.Version, p.Generation, 1); !errors.Is(err, repository.ErrConflict) {
			t.Fatal("outdated confirmation reused")
		}
	})
	t.Run("withdraw_blocks_pending_and_running_rebuilds", func(t *testing.T) {
		for _, running := range []bool{false, true} {
			r, scope := failed(t)
			p, err := s.RebuildProjection(ctx, actor, scope, r.Version, *r.Generation, 1)
			if err != nil {
				t.Fatal(err)
			}
			withdraw := func(context.Context) error {
				_, err := s.Transition(ctx, actor, scope, r.Version, "withdraw")
				return err
			}
			if running {
				admit(t, actor, r, scope, p)
				if err := execute(t, p.ExecutionID, testProjectionGraph{build: withdraw}); err == nil {
					t.Fatal("withdrawn rebuild activated")
				}
				assertState(t, p, "failed", execution.ExecutionStatusFailed)
			} else {
				issuer := testAuthorizationIssuer(func(ctx context.Context, token string, req client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
					if err := withdraw(ctx); err != nil {
						return nil, err
					}
					return runtimeFixtureIssuer(actor).Issue(ctx, token, req)
				})
				if err := s.AdmitProjection(ctx, actor, scope, r.Version, p.Generation, "addp_at_late", issuer); !errors.Is(err, repository.ErrConflict) {
					t.Fatalf("late admission: %v", err)
				}
				assertState(t, p, "failed", execution.ExecutionStatusCancelled)
			}
			if _, err := s.RebuildProjection(ctx, actor, scope, r.Version+1, p.Generation, 1); !errors.Is(err, repository.ErrConflict) {
				t.Fatal("withdrawn revision rebuilt")
			}
		}
	})
	t.Run("rebuild_audit_failure_rolls_back_execution_and_projection", func(t *testing.T) {
		r, scope := failed(t)
		if err := db.Exec(`CREATE FUNCTION ontology.test_reject_rebuild() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='rebuild_requested' THEN RAISE EXCEPTION 'injected rebuild audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER test_reject_rebuild BEFORE INSERT ON ontology.projection_events FOR EACH ROW EXECUTE FUNCTION ontology.test_reject_rebuild()`).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Exec(`DROP TRIGGER test_reject_rebuild ON ontology.projection_events; DROP FUNCTION ontology.test_reject_rebuild()`)
		if _, err := s.RebuildProjection(ctx, actor, scope, r.Version, *r.Generation, 1); err == nil || !strings.Contains(err.Error(), "injected rebuild audit failure") {
			t.Fatalf("expected injected audit failure, got: %v", err)
		}
		var projections, jobs int64
		if err := db.Model(&models.Projection{}).Where("ontology_id=?", scope.OntologyID).Count(&projections).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&execution.TaskExecution{}).Where("execution_config->>'ontology_id'=?", scope.OntologyID).Count(&jobs).Error; err != nil {
			t.Fatal(err)
		}
		if projections != 1 || jobs != 1 {
			t.Fatalf("rollback left projections=%d executions=%d", projections, jobs)
		}
	})
}
