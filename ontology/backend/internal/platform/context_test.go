package platform

import "testing"

func TestPlatformDefinitionIsBoundedAndIndependentOfTenant(t *testing.T) {
	snapshot, err := CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	a, err := snapshot.Context()
	if err != nil {
		t.Fatal(err)
	}
	if a.KnowledgeKind != "platform_definition" || a.Availability != "not_observed" || a.Revision != 4 || len(a.Digest) != 64 || a.Operation.Tool != a.Capability {
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
