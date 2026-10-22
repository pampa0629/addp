package models

import "testing"

func TestCatalogProjectionUsesCurrentThenLatestAndPreservesIdentity(t *testing.T) {
	const id int64 = 9007199254740993
	domain := int64(31)
	for _, kind := range []string{"metric", "element", "glossary", "code_set", "document"} {
		aggregate := map[string]any{"id": id, "version": int64(4), "code": "stable_code", "scope_type": "domain", "owner_domain_id": domain, "lifecycle_state": "active",
			"current_revision": map[string]any{"id": id + 2, "revision_no": 1, "status": "published", "name": "Effective name", "definition": "Definition", "description": "Description"},
			"latest_revision":  map[string]any{"id": id + 3, "revision_no": 2, "status": "draft", "name": "Next name", "definition": "Next"}}
		result, err := ProjectCatalogResource(kind, aggregate)
		if err != nil || result.SourceIdentity != "9007199254740993" || result.Summary["revision_id"] != "9007199254740995" || result.Summary["name"] != "Effective name" || result.Summary["is_current"] != true || result.Summary["definition_complete"] != true {
			t.Fatalf("%s projection=%+v err=%v", kind, result, err)
		}
		delete(aggregate, "current_revision")
		result, err = ProjectCatalogResource(kind, aggregate)
		if err != nil || result.Summary["name"] != "Next name" || result.Summary["is_current"] != false {
			t.Fatalf("%s fallback=%+v err=%v", kind, result, err)
		}
		if _, ok := result.Summary["definition"]; ok {
			t.Fatal("definition body leaked into projection")
		}
	}
}
