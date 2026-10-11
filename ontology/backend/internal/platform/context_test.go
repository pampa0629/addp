package platform

import (
	"reflect"
	"testing"
)

func TestTransferConditionEvidenceAndDecisionSources(t *testing.T) {
	d := sourceDefinition(t)
	want := map[Relation]bool{
		{From: "confirmed_source", Kind: "reads_via", To: "source_discovery"}:                        true,
		{From: "confirmed_target_parent_and_name", Kind: "reads_via", To: "target_parent_discovery"}: true,
		{From: "confirmed_target_parent_and_name", Kind: "configured_by", To: "target"}:              true,
		{From: "confirmed_row_grain_and_fields", Kind: "reads_via", To: "source_discovery"}:          true,
		{From: "confirmed_row_grain_and_fields", Kind: "configured_by", To: "field_mapping"}:         true,
		{From: "bounded", Kind: "configured_by", To: "runtime_boundary"}:                             true,
		{From: "snapshot", Kind: "configured_by", To: "load_mode"}:                                   true,
		{From: "explicit_apply_policy", Kind: "configured_by", To: "apply_policy"}:                   true,
		{From: "user_review", Kind: "confirmed_by", To: "user_confirmation"}:                         true,
	}
	got := map[Relation]bool{}
	for _, r := range d.Relations {
		if r.Kind == "reads_via" || r.Kind == "configured_by" || r.Kind == "confirmed_by" {
			got[r] = true
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("condition evidence or decision sources changed", got)
	}
	for _, r := range d.Relations {
		if r.Kind == "reads_via" && r.To == "optional_draft_assistance" {
			t.Fatal("draft promoted to owner evidence")
		}
	}
}

func TestPlatformDefinitionIsBoundedAndIndependentOfTenant(t *testing.T) {
	snapshot, err := CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	a, err := snapshot.Context()
	if err != nil {
		t.Fatal(err)
	}
	if a.KnowledgeKind != "platform_definition" || a.Availability != "not_observed" || a.Revision != 10 || len(a.Digest) != 64 || a.Operation.Tool != a.Capability {
		t.Fatalf("invalid platform binding: %+v", a)
	}
	ids := map[string]bool{}
	for _, concept := range a.Concepts {
		if ids[concept.ID] || concept.ID == "" || concept.Name["en"] == "" || concept.Name["zh-cn"] == "" {
			t.Fatalf("invalid concept: %+v", concept)
		}
		ids[concept.ID] = true
	}
	for _, relation := range a.Relations {
		if !ids[relation.From] || !ids[relation.To] || relation.Kind == "" {
			t.Fatalf("dangling relation: %+v", relation)
		}
	}
	for _, condition := range a.Operation.InputsRequired {
		if !ids[condition] {
			t.Fatalf("condition has no semantic concept: %s", condition)
		}
		found := false
		for _, relation := range a.Relations {
			found = found || relation == (Relation{From: "transfer_task", Kind: "requires", To: condition})
		}
		if !found {
			t.Fatalf("condition has no requires relation: %s", condition)
		}
	}
	a.Concepts[0].Name["en"] = "mutated"
	b, err := snapshot.Context()
	if err != nil || b.Concepts[0].Name["en"] == "mutated" || a.Digest != b.Digest {
		t.Fatal("read modified the published definition")
	}
}
