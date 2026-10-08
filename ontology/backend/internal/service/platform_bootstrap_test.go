package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

func TestPlatformBootstrapWaitsForReadyAndNeverRetriesFailure(t *testing.T) {
	snapshot := platformTestSnapshot(t, "transfer.task.create", 2)
	denied := errors.New("publication denied")
	checks, compiles, admissions := 0, 0, 0
	p, err := NewPlatformPublisher(repository.NewPlatformRevisionRepository(nil), testPlatformGraph{}, testPlatformAuthorizer(
		func(context.Context, *platform.Snapshot) (models.PlatformActor, error) {
			admissions++
			return models.PlatformActor{}, denied
		}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = p.PublishOnReady(ctx, func() bool {
		checks++
		if admissions != 0 {
			t.Fatal("admission before readiness")
		}
		return checks == 2
	}, func() (*platform.Snapshot, error) { compiles++; return snapshot, nil })
	if !errors.Is(err, denied) || checks != 2 || compiles != 1 || admissions != 1 {
		t.Fatalf("err=%v checks=%d compiles=%d admissions=%d", err, checks, compiles, admissions)
	}
}

func TestPlatformBootstrapCancellationAndInvalidReleaseHaveNoAdmission(t *testing.T) {
	p, _ := NewPlatformPublisher(repository.NewPlatformRevisionRepository(nil), testPlatformGraph{}, testPlatformAuthorizer(
		func(context.Context, *platform.Snapshot) (models.PlatformActor, error) {
			t.Fatal("unexpected admission")
			return models.PlatformActor{}, nil
		}))
	snapshot := platformTestSnapshot(t, "transfer.task.create", 2)
	ctx, cancel := context.WithCancel(context.Background())
	compiles, checks := 0, 0
	err := p.PublishOnReady(ctx, func() bool { checks++; cancel(); return false }, func() (*platform.Snapshot, error) { compiles++; return snapshot, nil })
	if !errors.Is(err, context.Canceled) || compiles != 1 || checks != 1 {
		t.Fatal(err, compiles, checks)
	}
	if err := p.PublishOnReady(ctx, func() bool { t.Fatal("readiness after cancellation"); return true }, func() (*platform.Snapshot, error) { t.Fatal("compile after cancellation"); return nil, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, s := range []*platform.Snapshot{nil, {}} {
		if err := p.PublishOnReady(context.Background(), func() bool { t.Fatal("readiness for invalid release"); return true }, func() (*platform.Snapshot, error) { return s, nil }); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	compileErr := errors.New("invalid deployment")
	if err := p.PublishOnReady(context.Background(), func() bool { t.Fatal("readiness for compile error"); return true }, func() (*platform.Snapshot, error) { return nil, compileErr }); !errors.Is(err, compileErr) {
		t.Fatal(err)
	}
}
