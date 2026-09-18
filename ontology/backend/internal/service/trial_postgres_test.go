package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

// Invoked by the existing isolated PostgreSQL owner gate, after activation.
func testActiveTrial(t *testing.T, s *RevisionService, actor models.Actor, b *ClassDirectory) {
	ctx := context.Background()
	trial := func(facts map[string]semantic.Fact) (*TrialResult, error) {
		return s.Trial(ctx, actor, b.OntologyID, "valid_date", b.Revision, b.Generation, b.ActivationVersion, facts)
	}
	for _, tc := range []struct {
		name    string
		fact    semantic.Fact
		outcome semantic.Outcome
	}{
		{"true", semantic.Fact{State: semantic.Known, Value: true}, semantic.Matched},
		{"false", semantic.Fact{State: semantic.Known, Value: false}, semantic.NotMatched},
		{"absent", semantic.Fact{State: semantic.Absent}, semantic.NotMatched},
		{"unknown", semantic.Fact{State: semantic.Unknown}, semantic.Undetermined},
		{"invalid", semantic.Fact{State: semantic.Invalid}, semantic.Failed},
		{"wrong_type", semantic.Fact{State: semantic.Known, Value: "private-hypothesis"}, semantic.Failed},
	} {
		t.Run("trial_"+tc.name, func(t *testing.T) {
			got, err := trial(map[string]semantic.Fact{"has_date": tc.fact})
			if err != nil || got.Decision.Outcome != tc.outcome || got.Decision.Mode != "hypothetical" || got.SemanticBinding != b.SemanticBinding || got.Decision.Digest != b.Digest || got.Decision.Basis == "" {
				t.Fatalf("%+v %v", got, err)
			}
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "private-hypothesis") {
				t.Fatal("raw value leaked")
			}
		})
	}
	if got, err := trial(nil); err != nil || got.Decision.Outcome != semantic.Undetermined {
		t.Fatalf("omitted input: %+v %v", got, err)
	}
	if _, err := trial(map[string]semantic.Fact{"undeclared": {State: semantic.Unknown}}); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		actor      models.Actor
		revision   uint64
		generation string
		activation uint64
		rule       string
		err        error
	}{
		{"foreign", testActor(actor.TenantID + 1), b.Revision, b.Generation, b.ActivationVersion, "valid_date", repository.ErrNotFound},
		{"revision", actor, b.Revision + 1, b.Generation, b.ActivationVersion, "valid_date", ErrActivationChanged},
		{"generation", actor, b.Revision, "different", b.ActivationVersion, "valid_date", ErrActivationChanged},
		{"activation", actor, b.Revision, b.Generation, b.ActivationVersion + 1, "valid_date", ErrActivationChanged},
		{"missing_rule", actor, b.Revision, b.Generation, b.ActivationVersion, "missing", repository.ErrNotFound},
	} {
		if _, err := s.Trial(ctx, tc.actor, b.OntologyID, tc.rule, tc.revision, tc.generation, tc.activation, nil); !errors.Is(err, tc.err) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Trial(canceled, actor, b.OntologyID, "valid_date", b.Revision, b.Generation, b.ActivationVersion, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
