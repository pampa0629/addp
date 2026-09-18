package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/ontology/internal/falkor"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

type ProjectionGraph interface {
	Build(context.Context, *falkor.Projection) error
	Verify(context.Context, *falkor.Projection) error
}

type ProjectionAuthorizer interface {
	Authorize(context.Context, *repository.ProjectionWork, execution.Lease) (*client.InternalTaskAccess, error)
}

type SystemProjectionAuthorizer struct{ client *client.SystemServiceClient }

func NewSystemProjectionAuthorizer(c *client.SystemServiceClient) (*SystemProjectionAuthorizer, error) {
	if c == nil {
		return nil, repository.ErrInvalid
	}
	return &SystemProjectionAuthorizer{client: c}, nil
}

func (a *SystemProjectionAuthorizer) Authorize(ctx context.Context, w *repository.ProjectionWork, lease execution.Lease) (*client.InternalTaskAccess, error) {
	if w == nil || w.Execution.ExecutionAuthorizationID == nil || w.Execution.TenantID <= 0 {
		return nil, repository.ErrInvalid
	}
	return a.client.WithTenantID(uint(w.Execution.TenantID)).GetInternalTaskAccess(ctx, strconv.FormatInt(*w.Execution.ExecutionAuthorizationID, 10),
		client.InternalTaskAccessRequest{ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, InternalTask: w.Boundary()})
}

type ProjectionExecutor struct {
	repo       *repository.RevisionRepository
	graph      ProjectionGraph
	authorizer ProjectionAuthorizer
}

func NewProjectionExecutor(repo *repository.RevisionRepository, graph ProjectionGraph, authorizer ProjectionAuthorizer) (*ProjectionExecutor, error) {
	if repo == nil || graph == nil || authorizer == nil {
		return nil, repository.ErrInvalid
	}
	return &ProjectionExecutor{repo: repo, graph: graph, authorizer: authorizer}, nil
}

// Execute consumes only a Common-owned lease. No User Token, graph key or
// caller-supplied snapshot can enter this runtime path.
func (e *ProjectionExecutor) Execute(ctx context.Context, lease execution.Lease) (result error) {
	defer func() {
		if result == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		terminal, err := e.repo.ProjectionTerminal(cleanup, lease)
		if err != nil {
			result = errors.Join(result, err)
			return
		}
		if terminal {
			return
		}
		err = e.repo.FailProjection(cleanup, lease, false)
		if err != nil {
			err = errors.Join(err, e.repo.FailProjection(cleanup, lease, true))
		}
		result = errors.Join(result, err)
	}()
	w, err := e.repo.ProjectionWork(ctx, lease)
	if err != nil {
		return err
	}
	snapshot, err := semantic.Restore([]byte(w.Revision.Payload), w.Revision.Digest)
	if err != nil || snapshot.Scope() != (semantic.Scope{TenantID: w.Revision.TenantID, OntologyID: w.Revision.OntologyID, Revision: w.Revision.Revision}) {
		return repository.ErrIntegrity
	}
	plan, err := falkor.Plan(snapshot, w.Projection.Generation)
	if err != nil {
		return err
	}
	receipt, err := e.authorizer.Authorize(ctx, w, lease)
	if err != nil {
		return err
	}
	if err := e.repo.BeginProjection(ctx, lease, receipt); err != nil {
		return err
	}
	if err := e.graph.Build(ctx, plan); err != nil {
		return err
	}
	if err := e.graph.Verify(ctx, plan); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	receipt, err = e.authorizer.Authorize(ctx, w, lease)
	if err != nil {
		return err
	}
	return e.repo.ActivateProjection(ctx, lease, receipt)
}
