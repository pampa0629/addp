package models

import (
	"encoding/json"
	"testing"
)

func TestEngineDecodesCapabilitiesView(t *testing.T) {
	var engine Engine
	if err := json.Unmarshal([]byte(`{
		"id":1,
		"name":"GeoPython Workflow",
		"engine_type":"geopython_workflow",
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

func TestConnectionInfoScansStringAndBytes(t *testing.T) {
	var fromString ConnectionInfo
	if err := fromString.Scan(`{"host":"127.0.0.1","port":8103}`); err != nil {
		t.Fatalf("scan string: %v", err)
	}
	if got := fromString["host"]; got != "127.0.0.1" {
		t.Fatalf("host = %#v, want 127.0.0.1", got)
	}
	if got := fromString["port"]; got != float64(8103) {
		t.Fatalf("port = %#v, want 8103", got)
	}

	var fromBytes ConnectionInfo
	if err := fromBytes.Scan([]byte(`{"database":"gisdb"}`)); err != nil {
		t.Fatalf("scan bytes: %v", err)
	}
	if got := fromBytes["database"]; got != "gisdb" {
		t.Fatalf("database = %#v, want gisdb", got)
	}
}
