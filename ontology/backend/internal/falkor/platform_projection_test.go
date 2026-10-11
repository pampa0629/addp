package falkor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/addp/ontology/internal/platform"
	"github.com/google/uuid"
)

func platformFixture(t *testing.T) *platform.Snapshot {
	t.Helper()
	snapshot, err := platform.CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func maximumPlatformFixture(t *testing.T) *platform.Snapshot {
	t.Helper()
	d, _ := platformFixture(t).Context()
	d.Digest = ""
	d.Concepts = nil
	d.Relations = []platform.Relation{}
	for i := 0; i < 32; i++ {
		names := map[string]string{"en": "Concept", "zh-cn": "概念"}
		for j := 0; j < 14; j++ {
			names[fmt.Sprintf("locale_%d", j)] = "Name"
		}
		d.Concepts = append(d.Concepts, platform.Concept{ID: fmt.Sprintf("concept_%d", i), Name: names})
		for j := 0; j < 2; j++ {
			d.Relations = append(d.Relations, platform.Relation{From: fmt.Sprintf("concept_%d", i), To: "concept_0", Kind: fmt.Sprintf("relation_%d", j)})
		}
	}
	return compilePlatformFixture(t, d)
}

func maximumEvidenceFixture(t *testing.T) *platform.Snapshot {
	t.Helper()
	d, _ := platformFixture(t).Context()
	d.Digest = ""
	d.Concepts = nil
	d.Relations = []platform.Relation{}
	d.Requirements = nil
	d.Operation.InputsRequired = nil
	tools := make([]string, 32)
	for i := range tools {
		tools[i] = fmt.Sprintf("engine.tool_%d", i)
	}
	for i := 0; i < 16; i++ {
		condition, entry := fmt.Sprintf("condition_%d", i), fmt.Sprintf("entry_%d", i)
		d.Operation.InputsRequired = append(d.Operation.InputsRequired, condition)
		d.Requirements = append(d.Requirements, platform.Requirement{ID: entry, Tools: tools})
		for _, id := range []string{condition, entry} {
			d.Concepts = append(d.Concepts, platform.Concept{ID: id, Name: map[string]string{"en": "Concept", "zh-cn": "概念"}})
		}
		for j := 0; j < 4; j++ {
			d.Relations = append(d.Relations, platform.Relation{From: condition, Kind: "reads_via", To: fmt.Sprintf("entry_%d", j)})
		}
	}
	return compilePlatformFixture(t, d)
}

func compilePlatformFixture(t *testing.T, d platform.Context) *platform.Snapshot {
	t.Helper()
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := platformFixture(t).Review()
	r.Bindings = nil
	for _, subject := range platform.ReviewSubjects(d) {
		r.Bindings = append(r.Bindings, platform.SourceBinding{Subject: subject, Sources: []string{r.Sources[0].ID}})
	}
	reviewData, _ := json.Marshal(r)
	snapshot, err := platform.Compile(data, reviewData)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPlatformPlan(t *testing.T) {
	snapshot := platformFixture(t)
	generation := uuid.NewString()
	p, err := PlanPlatform(snapshot, generation)
	if err != nil {
		t.Fatal(err)
	}
	q, err := PlanPlatform(snapshot, generation)
	d, _ := snapshot.Context()
	prefix := fmt.Sprintf("ontology:p:%s:r%d:g", d.Capability, d.Revision)
	if err != nil || !reflect.DeepEqual(p, q) || !strings.HasPrefix(p.key, prefix) || strings.Contains(p.key, "t0:") {
		t.Fatal("unstable or unscoped plan", p, err)
	}
	if len(p.nodes) != 83 || len(p.edges) != 118 || len(p.dependencyRows) != 25 || len(p.conditionRows) != 8 {
		t.Fatal("missing capability dependencies", len(p.nodes), len(p.edges), len(p.dependencyRows), len(p.conditionRows))
	}
	for _, want := range [][]any{
		{"capability:transfer.task.create", "condition:confirmed_source", "requires"},
		{"capability:transfer.task.create", "effect:business_data.written", "excludes"},
		{"requirement:source_discovery", "tool:resource.facts.get", "uses_tool"},
		{"condition:confirmed_source", "concept:confirmed_source", "described_by"},
		{"condition:confirmed_source", "requirement:source_discovery", "reads_via"},
		{"requirement:source_discovery", "concept:source_discovery", "described_by"},
		{"concept:user_review", "concept:user_confirmation", "confirmed_by"},
	} {
		if !slices.ContainsFunc(p.edgeRows, func(row []any) bool { return reflect.DeepEqual(row, want) }) {
			t.Fatal("missing declared dependency", want)
		}
	}
	if !reflect.DeepEqual(p.conditionRows, [][]any{
		{"confirmed_row_grain_and_fields", "source_discovery", "data.search"},
		{"confirmed_row_grain_and_fields", "source_discovery", "resource.ancestors.get"},
		{"confirmed_row_grain_and_fields", "source_discovery", "resource.facts.get"},
		{"confirmed_source", "source_discovery", "data.search"},
		{"confirmed_source", "source_discovery", "resource.ancestors.get"},
		{"confirmed_source", "source_discovery", "resource.facts.get"},
		{"confirmed_target_parent_and_name", "target_parent_discovery", "resource.ancestors.get"},
		{"confirmed_target_parent_and_name", "target_parent_discovery", "resource.children.list"},
	}) {
		t.Fatal("incorrect condition evidence paths", p.conditionRows)
	}
	for _, row := range p.edgeRows {
		if strings.HasPrefix(row[0].(string), "condition:") && row[2] == "uses_tool" {
			t.Fatal("invented condition/fact correspondence", row)
		}
	}
	other, _ := PlanPlatform(snapshot, uuid.NewString())
	if other.key == p.key {
		t.Fatal("generation reused")
	}
	for _, bad := range []*platform.Snapshot{nil, {}} {
		if _, err := PlanPlatform(bad, generation); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{"", uuid.Nil.String(), strings.ToUpper(generation)} {
		if _, err := PlanPlatform(snapshot, bad); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := parameters(map[string]any{"nodes": p.nodes}); err != nil {
		t.Fatal(err)
	}
	if _, err := parameters(map[string]any{"edges": p.edges}); err != nil {
		t.Fatal(err)
	}
	maximum, err := PlanPlatform(maximumPlatformFixture(t), uuid.NewString())
	if err != nil || len(maximum.nodeRows) != 573 || len(maximum.edgeRows) != 637 {
		t.Fatal("platform plan limit", err)
	}
	if len(maximum.conditionRows) != 0 {
		t.Fatal("undeclared evidence paths inferred", maximum.conditionRows)
	}
	evidence, err := PlanPlatform(maximumEvidenceFixture(t), uuid.NewString())
	if err != nil || len(evidence.conditionRows) != 64*32 {
		t.Fatal("condition path result bound", err)
	}
}

func TestPlatformDependencyReadParametersAndCancellation(t *testing.T) {
	encoded, err := parameters(platformDependencyParameters("transfer.task.create", 26))
	if err != nil || !strings.Contains(encoded, `capability="capability:transfer.task.create"`) || !strings.Contains(encoded, "limit=26") || !strings.Contains(encoded, "has_requirement") {
		t.Fatal("unencodable dependency parameters", encoded, err)
	}
	c, _ := New(Config{Address: "127.0.0.1:1", Password: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.PlatformContext(ctx, platformFixture(t), uuid.NewString()); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled graph read was not rejected", err)
	}
	if _, err := c.PlatformContext(context.Background(), nil, uuid.NewString()); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid release reached network", err)
	}
}
