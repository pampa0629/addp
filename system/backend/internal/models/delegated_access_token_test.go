package models

import "testing"

func TestDelegatedToolScopeRegistryMatchesPublishedManifest(t *testing.T) {
	expected := map[string][]string{
		"system":  {"engine.list"},
		"manager": {"data.search", "data.preview"},
		"meta":    {"resource.ancestors.get"},
		"develop": {"workflow.operators.list", "workflow.validate", "workflow.run", "execution.get"},
		"copilot": {"workflow.draft.generate"},
	}
	for audience, scopes := range expected {
		for _, scope := range scopes {
			if !IsDelegatedToolScopeAllowed(audience, scope) {
				t.Errorf("missing delegated Tool scope %s/%s", audience, scope)
			}
		}
	}
	if IsDelegatedToolScopeAllowed("system", "users.delete") || IsDelegatedToolScopeAllowed("unknown", "engine.list") {
		t.Fatal("unregistered delegated Tool scope was accepted")
	}
}
