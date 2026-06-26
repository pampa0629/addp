package models

import (
	"encoding/json"
	"testing"
)

func TestEngineDecodesCapabilitiesView(t *testing.T) {
	var engine Engine
	if err := json.Unmarshal([]byte(`{
		"id":1,
		"name":"Python Workflow",
		"engine_type":"python_workflow",
		"connection_info":{},
		"capabilities_view":{
			"summary":[{"id":"workflow","label_key":"system.engine.capabilityView.summary.workflow"}],
			"sections":[{"id":"compute","title_key":"system.engine.capabilityView.sections.compute"}],
			"json_view":[{"key":"schema_version","value":"engine.capabilities/v1"}]
		}
	}`), &engine); err != nil {
		t.Fatalf("unmarshal engine: %v", err)
	}

	if engine.CapabilitiesView == nil {
		t.Fatal("CapabilitiesView is nil")
	}
	if got := engine.CapabilitiesView.Summary[0].ID; got != "workflow" {
		t.Fatalf("summary id = %q, want workflow", got)
	}
	if got := engine.CapabilitiesView.Sections[0].ID; got != "compute" {
		t.Fatalf("section id = %q, want compute", got)
	}
	if got := engine.CapabilitiesView.JSONView[0].Value; got != "engine.capabilities/v1" {
		t.Fatalf("json view value = %q, want engine.capabilities/v1", got)
	}
}
