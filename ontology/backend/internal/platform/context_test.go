package platform

import "testing"

func TestPlatformDefinitionIsBoundedAndIndependentOfTenant(t *testing.T) {
	a, err := TransferContext()
	if err != nil {
		t.Fatal(err)
	}
	if a.KnowledgeKind != "platform_definition" || a.Availability != "not_observed" || a.Revision != 1 || len(a.Digest) != 64 || a.Operation.Tool != a.Capability {
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
	a.Concepts[0].Name["en"] = "mutated"
	b, err := TransferContext()
	if err != nil || b.Concepts[0].Name["en"] == "mutated" || a.Digest != b.Digest {
		t.Fatal("read modified the published definition")
	}
}
