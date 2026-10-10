package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/repository"
)

func platformTestActor() models.PlatformActor {
	return models.PlatformActor{ContextType: "platform", PrincipalID: 11, PrincipalType: "user", AuthorizationVersion: 7}
}

func TestPlatformCatalogRestoreAndBudgets(t *testing.T) {
	ctx := context.Background()
	makeRecord := func(snapshot *platform.Snapshot) models.PlatformRevision {
		t.Helper()
		definition, err := snapshot.Context()
		if err != nil {
			t.Fatal(err)
		}
		return models.PlatformRevision{Capability: definition.Capability, Revision: definition.Revision, Digest: snapshot.Digest(), Payload: string(snapshot.CanonicalJSON())}
	}
	base := makeRecord(platformTestSnapshot(t, "transfer.task.create", 2))
	empty, err := platformCatalog(ctx, nil)
	if err != nil || empty.Capabilities == nil || len(empty.Capabilities) != 0 {
		t.Fatal(empty, err)
	}
	got, err := platformCatalog(ctx, []models.PlatformRevision{base})
	if err != nil || len(got.Capabilities) != 1 || got.Capabilities[0].Digest != base.Digest {
		t.Fatal(got, err)
	}
	bad := base
	bad.Payload = " " + bad.Payload
	if _, err := platformCatalog(ctx, []models.PlatformRevision{base, bad}); !errors.Is(err, repository.ErrIntegrity) {
		t.Fatal("corruption produced partial success", err)
	}
	if _, err := platformCatalog(ctx, make([]models.PlatformRevision, platform.CatalogMaxItems+1)); !errors.Is(err, ErrResultTooLarge) {
		t.Fatal(err)
	}
	definition, _ := platformTestSnapshot(t, "transfer.task.large", 3).Context()
	definition.Digest = ""
	for i := range definition.Concepts {
		definition.Concepts[i].Name["en"] = strings.Repeat("x", 512)
		definition.Concepts[i].Name["zh-cn"] = strings.Repeat("x", 512)
	}
	encoded, _ := json.Marshal(definition)
	large, err := platform.Compile(encoded, platformTestReview(t, definition))
	if err != nil {
		t.Fatal(err)
	}
	records := make([]models.PlatformRevision, platform.CatalogMaxItems)
	for i := range records {
		records[i] = makeRecord(large)
	}
	if _, err := platformCatalog(ctx, records); !errors.Is(err, ErrResultTooLarge) {
		t.Fatal("byte budget not enforced", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := NewPlatformRevisionService(repository.NewPlatformRevisionRepository(nil)).PlatformCapabilities(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func platformTestSnapshot(t *testing.T, capability string, revision uint64) *platform.Snapshot {
	t.Helper()
	release, err := platform.CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	definition, err := release.Context()
	if err != nil {
		t.Fatal(err)
	}
	definition.Capability, definition.Operation.Tool, definition.Revision, definition.Digest = capability, capability, revision, ""
	data, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := platform.Compile(data, platformTestReview(t, definition))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func platformTestReview(t *testing.T, d platform.Context) []byte {
	t.Helper()
	release, err := platform.CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	r, err := release.Review()
	if err != nil {
		t.Fatal(err)
	}
	r.Bindings = nil
	for _, subject := range platform.ReviewSubjects(d) {
		r.Bindings = append(r.Bindings, platform.SourceBinding{Subject: subject, Sources: []string{r.Sources[0].ID}})
	}
	r.Coverage[0].ID = d.Capability
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
	if _, err := s.PlatformCapabilityContext(canceled, "transfer.task.create"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.PlatformCapabilityContext(ctx, "Transfer.task.create"); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
}
