package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

func platformTestActor() models.PlatformActor {
	return models.PlatformActor{ContextType: "platform", PrincipalID: 11, PrincipalType: "user", AuthorizationVersion: 7}
}

func platformTestSnapshot(t *testing.T, capability string, revision uint64) *platform.Snapshot {
	t.Helper()
	definition, err := platform.TransferContext()
	if err != nil {
		t.Fatal(err)
	}
	definition.Capability, definition.Operation.Tool, definition.Revision, definition.Digest = capability, capability, revision, ""
	data, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := platform.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPlatformCommandsRejectBeforeDatabaseAccess(t *testing.T) {
	s := NewPlatformRevisionService(repository.NewPlatformRevisionRepository(nil))
	ctx, actor := context.Background(), platformTestActor()
	snapshot := platformTestSnapshot(t, "transfer.task.create", 2)
	for _, bad := range []models.PlatformActor{{},
		{ContextType: "tenant", PrincipalID: 11, PrincipalType: "user", AuthorizationVersion: 7},
		{ContextType: "platform", PrincipalType: "user", AuthorizationVersion: 7},
		{ContextType: "platform", PrincipalID: 11, PrincipalType: "llm", AuthorizationVersion: 7},
		{ContextType: "platform", PrincipalID: 11, PrincipalType: "user"},
	} {
		if _, err := s.Store(ctx, bad, snapshot, 0); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, bad, "transfer.task.create", 2); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := s.Head(ctx, bad, "transfer.task.create"); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, bad := range []*platform.Snapshot{nil, {}} {
		if _, err := s.Store(ctx, actor, bad, 0); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.Store(ctx, actor, snapshot, math.MaxUint64); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	for _, baseline := range []uint64{2, 3} {
		if _, err := s.Store(ctx, actor, snapshot, baseline); !errors.Is(err, repository.ErrConflict) {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"", "transfer", "Transfer.task", "transfer.task;drop", string(make([]byte, 129))} {
		if _, err := s.Head(ctx, actor, id); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, actor, id, 1); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, revision := range []uint64{0, math.MaxUint64} {
		if _, err := s.Get(ctx, actor, "transfer.task.create", revision); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Store(canceled, actor, snapshot, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Get(canceled, actor, "transfer.task.create", 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Head(canceled, actor, "transfer.task.create"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
