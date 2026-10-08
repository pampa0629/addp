package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/ontology/internal/falkor"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

type PlatformPublicationAuthorizer interface {
	Check(context.Context, *platform.Snapshot) (models.PlatformActor, error)
}

// PlatformPublisher is a bounded synchronous machine publication, not a queue.
// It never activates from an actor supplied by a user or resumes graph writes.
type PlatformPublisher struct {
	repo       *repository.PlatformRevisionRepository
	graph      ProjectionGraph
	authorizer PlatformPublicationAuthorizer
}

func NewPlatformPublisher(repo *repository.PlatformRevisionRepository, graph ProjectionGraph, authorizer PlatformPublicationAuthorizer) (*PlatformPublisher, error) {
	if repo == nil || graph == nil || authorizer == nil {
		return nil, repository.ErrInvalid
	}
	return &PlatformPublisher{repo: repo, graph: graph, authorizer: authorizer}, nil
}

func (p *PlatformPublisher) check(ctx context.Context, snapshot *platform.Snapshot) (models.PlatformActor, error) {
	actor, err := p.authorizer.Check(ctx, snapshot)
	if err != nil {
		return models.PlatformActor{}, err
	}
	if repository.ValidatePlatformActor(actor) != nil || actor.PrincipalType != "service_principal" {
		return models.PlatformActor{}, repository.ErrInvalid
	}
	return actor, ctx.Err()
}

// Publish records or reuses an exact release, builds a new generation and then
// activates via PG CAS. On error the attempt may be failed, superseded or left
// building after cancellation/uncertain commit; callers must not infer success.
func (p *PlatformPublisher) Publish(ctx context.Context, snapshot *platform.Snapshot) (*models.PlatformProjection, error) {
	d, err := snapshot.Context()
	if err != nil {
		return nil, repository.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	actor, err := p.check(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	record, err := p.repo.Get(ctx, actor, d.Capability, d.Revision)
	if errors.Is(err, repository.ErrNotFound) {
		head, headErr := p.repo.Head(ctx, actor, d.Capability)
		var baseline uint64
		if headErr == nil {
			baseline = head.LastRevision
		} else if !errors.Is(headErr, repository.ErrNotFound) {
			return nil, headErr
		}
		_, err = p.repo.Store(ctx, actor, snapshot, baseline)
	} else if err == nil && (record.Digest != snapshot.Digest() || record.Payload != string(snapshot.CanonicalJSON())) {
		err = repository.ErrIntegrity
	}
	if err != nil {
		return nil, err
	}
	initialPrincipal := actor.PrincipalID
	actor, err = p.check(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if actor.PrincipalID != initialPrincipal {
		return nil, repository.ErrInvalid
	}
	attempt, err := p.repo.BeginPlatformProjection(ctx, actor, snapshot)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*models.PlatformProjection, error) {
		// Never revive a cancelled operation just to write a final status.
		finishErr := p.repo.FinishPlatformProjection(ctx, actor, snapshot, attempt, "failed")
		if finishErr == nil {
			attempt.Status = "failed"
		}
		return attempt, errors.Join(cause, finishErr)
	}
	plan, err := falkor.PlanPlatform(snapshot, attempt.Generation)
	if err != nil {
		return fail(err)
	}
	if err := p.graph.Build(ctx, plan); err != nil {
		return fail(err)
	}
	if err := p.graph.Verify(ctx, plan); err != nil {
		return fail(err)
	}
	nextActor, err := p.check(ctx, snapshot)
	if err != nil {
		return fail(err)
	}
	if nextActor.PrincipalID != actor.PrincipalID {
		return fail(repository.ErrInvalid)
	}
	actor = nextActor
	if err := p.repo.FinishPlatformProjection(ctx, actor, snapshot, attempt, "ready"); err != nil {
		return fail(err)
	}
	attempt.Status = "ready"
	return attempt, nil
}
