package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/ontology/internal/falkor"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

type testPlatformAuthorizer func(context.Context, *platform.Snapshot) (models.PlatformActor, error)

func (f testPlatformAuthorizer) Check(ctx context.Context, s *platform.Snapshot) (models.PlatformActor, error) {
	return f(ctx, s)
}

type testPlatformGraph struct {
	build  func(context.Context, *falkor.Projection) error
	verify func(context.Context, *falkor.Projection) error
}

func (g testPlatformGraph) Build(ctx context.Context, p *falkor.Projection) error {
	if g.build != nil {
		return g.build(ctx, p)
	}
	return ctx.Err()
}
func (g testPlatformGraph) Verify(ctx context.Context, p *falkor.Projection) error {
	if g.verify != nil {
		return g.verify(ctx, p)
	}
	return ctx.Err()
}

func platformMachineActor() models.PlatformActor {
	return models.PlatformActor{ContextType: "platform", PrincipalID: 41, PrincipalType: "service_principal", AuthorizationVersion: 9}
}

func TestPlatformPublisherRejectsBeforeDatabaseAccess(t *testing.T) {
	repo := repository.NewPlatformRevisionRepository(nil)
	graph := testPlatformGraph{}
	auth := testPlatformAuthorizer(func(ctx context.Context, _ *platform.Snapshot) (models.PlatformActor, error) {
		return platformMachineActor(), ctx.Err()
	})
	p, err := NewPlatformPublisher(repo, graph, auth)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []*platform.Snapshot{nil, {}} {
		if _, err := p.Publish(context.Background(), snapshot); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	snapshot := platformTestSnapshot(t, "transfer.task.create", 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Publish(ctx, snapshot); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, actor := range []models.PlatformActor{{}, platformTestActor(), {ContextType: "tenant", PrincipalID: 41, PrincipalType: "service_principal", AuthorizationVersion: 9}} {
		p, _ := NewPlatformPublisher(repo, graph, testPlatformAuthorizer(func(context.Context, *platform.Snapshot) (models.PlatformActor, error) { return actor, nil }))
		if _, err := p.Publish(context.Background(), snapshot); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := repo.BeginPlatformProjection(context.Background(), actor, snapshot); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
		if err := repo.FinishPlatformProjection(context.Background(), actor, snapshot, nil, "ready"); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, snapshot := range []*platform.Snapshot{nil, {}} {
		if _, err := repo.BeginPlatformProjection(context.Background(), platformMachineActor(), snapshot); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := NewPlatformPublisher(nil, graph, auth); err == nil {
		t.Fatal("nil repository")
	}
	if _, err := NewPlatformPublisher(repo, nil, auth); err == nil {
		t.Fatal("nil graph")
	}
	if _, err := NewPlatformPublisher(repo, graph, nil); err == nil {
		t.Fatal("nil authorizer")
	}
}
