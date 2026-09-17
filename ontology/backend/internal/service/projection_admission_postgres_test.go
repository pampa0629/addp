package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
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

type testAuthorizationIssuer func(context.Context, string, client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error)

func (f testAuthorizationIssuer) Issue(ctx context.Context, token string, request client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
	return f(ctx, token, request)
}

// Called inside the existing owner fixture so gate ownership/cleanup covers
// every created execution. System's real IAM suite separately proves signing.
func testProjectionAdmission(t *testing.T, db *gorm.DB, s *RevisionService, actor models.Actor) {
	ctx := context.Background()
	publish := func() (*models.Revision, semantic.Scope) {
		d := testDefinition("admit_" + strings.ReplaceAll(uuid.NewString(), "-", ""))
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
		return r, d.Scope
	}
	valid := func(request client.IssueExecutionAuthorizationRequest) *client.IssuedExecutionAuthorization {
		copy := *request.InternalTask
		return &client.IssuedExecutionAuthorization{ID: "91", ExecutionID: request.ExecutionID, Audience: "ontology", InternalTask: &copy,
			TenantID: strconv.FormatUint(actor.TenantID, 10), ActorPrincipalID: strconv.FormatInt(actor.PrincipalID, 10), TenantMembershipID: strconv.FormatInt(actor.MembershipID, 10),
			IssuedAuthorizationVersion: strconv.FormatInt(actor.AuthorizationVersion, 10), SourceType: "user", ExpiresAt: time.Now().Add(time.Minute)}
	}
	load := func(r *models.Revision) execution.TaskExecution {
		var job execution.TaskExecution
		if err := db.Where("execution_id=?", r.BuildExecutionID).First(&job).Error; err != nil {
			t.Fatal(err)
		}
		return job
	}
	t.Run("complete_grant_then_shared_claim", func(t *testing.T) {
		r, scope := publish()
		issuer := testAuthorizationIssuer(func(_ context.Context, token string, req client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
			if token != "addp_at_test" || req.InternalTask.Digest != r.Digest || len(req.Accesses) != 0 {
				t.Fatal("invalid issue request")
			}
			return valid(req), nil
		})
		if err := s.AdmitProjection(ctx, actor, scope, r.Version, "addp_at_test", issuer); err != nil {
			t.Fatal(err)
		}
		job := load(r)
		if job.ExecutionAuthorizationID == nil || *job.ExecutionAuthorizationID != 91 || job.AuthorizationExpiresAt == nil {
			t.Fatal("partial grant")
		}
		var lease *execution.Lease
		if err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			_, lease, err = execution.ClaimNext(ctx, tx, execution.ClaimOptions{Module: models.Module, TaskType: models.ProjectionTaskType, WorkerID: "ontology-test", LeaseDuration: time.Minute, RequireAuthorization: true})
			return err
		}); err != nil || lease == nil || lease.ExecutionID != *r.BuildExecutionID {
			t.Fatalf("claim %v %v", lease, err)
		}
		if err := s.AdmitProjection(ctx, actor, scope, r.Version, "addp_at_test", issuer); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("repeat admission %v", err)
		}
	})
	t.Run("reject_mismatched_grants_and_close_pending", func(t *testing.T) {
		for _, mutate := range []func(*client.IssuedExecutionAuthorization){
			func(a *client.IssuedExecutionAuthorization) { a.TenantID = "102" }, func(a *client.IssuedExecutionAuthorization) { a.ActorPrincipalID = "99" },
			func(a *client.IssuedExecutionAuthorization) { a.InternalTask.Digest = strings.Repeat("b", 64) }, func(a *client.IssuedExecutionAuthorization) { a.ExecutionID = uuid.NewString() },
			func(a *client.IssuedExecutionAuthorization) {
				a.Accesses = []client.ExecutionEngineAccessScope{{EngineID: "1", Effects: []string{"read"}}}
			},
			func(a *client.IssuedExecutionAuthorization) { a.ExpiresAt = time.Now().Add(-time.Minute) },
		} {
			r, scope := publish()
			issuer := testAuthorizationIssuer(func(_ context.Context, _ string, req client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
				a := valid(req)
				mutate(a)
				return a, nil
			})
			if err := s.AdmitProjection(ctx, actor, scope, r.Version, "addp_at_test", issuer); err == nil {
				t.Fatal("mismatched grant accepted")
			}
			if job := load(r); job.Status != "failed" || job.ExecutionAuthorizationID != nil {
				t.Fatalf("invalid admission outcome: %+v", job)
			}
		}
	})
	t.Run("withdrawal_wins_over_late_grant", func(t *testing.T) {
		r, scope := publish()
		issuer := testAuthorizationIssuer(func(_ context.Context, _ string, req client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
			if _, err := s.Transition(ctx, actor, scope, r.Version, "withdraw"); err != nil {
				t.Fatal(err)
			}
			return valid(req), nil
		})
		if err := s.AdmitProjection(ctx, actor, scope, r.Version, "addp_at_test", issuer); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("late grant: %v", err)
		}
		if job := load(r); job.Status != "cancelled" || job.ExecutionAuthorizationID != nil {
			t.Fatal("withdrawn intent authorized")
		}
	})
	t.Run("cancelled_request_closes_unadmitted_intent", func(t *testing.T) {
		r, scope := publish()
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		issuer := testAuthorizationIssuer(func(_ context.Context, _ string, _ client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
			cancel()
			return nil, context.Canceled
		})
		if err := s.AdmitProjection(cancelCtx, actor, scope, r.Version, "addp_at_test", issuer); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if job := load(r); job.Status != "failed" || job.ExecutionAuthorizationID != nil {
			t.Fatal("cancelled request left claimable job")
		}
	})
	t.Run("different_actor_cannot_admit_or_fail_original", func(t *testing.T) {
		r, scope := publish()
		other := actor
		other.PrincipalID++
		issuer := testAuthorizationIssuer(func(context.Context, string, client.IssueExecutionAuthorizationRequest) (*client.IssuedExecutionAuthorization, error) {
			t.Fatal("issuer called for wrong actor")
			return nil, nil
		})
		if err := s.AdmitProjection(ctx, other, scope, r.Version, "addp_at_test", issuer); !errors.Is(err, repository.ErrConflict) {
			t.Fatal(err)
		}
		if job := load(r); job.Status != "pending" || job.ExecutionAuthorizationID != nil {
			t.Fatal("another actor changed original intent")
		}
	})
}
