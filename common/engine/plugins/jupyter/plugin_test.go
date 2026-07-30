package jupyter

import "testing"

func TestJupyterCapabilitiesMatchRuntimeRegistration(t *testing.T) {
	p := &JupyterPlugin{}
	caps := p.Capabilities()

	if caps.SchemaVersion != "engine.capabilities/v1" {
		t.Fatalf("schema_version = %q", caps.SchemaVersion)
	}
	if caps.EngineType != "jupyter" || caps.EngineFamily != "script" {
		t.Fatalf("unexpected engine identity: %#v", caps)
	}
	if caps.Compute == nil || caps.Compute.Script == nil || !caps.Compute.Script.Supported {
		t.Fatalf("missing script capability: %#v", caps.Compute)
	}
	if got, want := caps.Compute.Script.Modes, []string{"notebook"}; !sameStrings(got, want) {
		t.Fatalf("script modes = %#v, want %#v", got, want)
	}
	if got, want := caps.Compute.Script.Languages, []string{"python"}; !sameStrings(got, want) {
		t.Fatalf("script languages = %#v, want %#v", got, want)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
