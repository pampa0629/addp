package utils

import (
	"testing"

	"github.com/addp/common/models"
)

func TestStructuredCapabilitiesStorageFilter(t *testing.T) {
	caps := `{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"postgresql",
		"engine_family":"tabular",
		"storage":{"catalog":{"supported":true}},
		"compute":{"query":{"supported":true,"languages":["sql"]}}
	}`
	capabilities := models.JSONString(caps)
	engine := &models.Engine{Capabilities: &capabilities}

	if !HasStorageCapability(engine) {
		t.Fatal("expected structured capabilities to expose storage capability")
	}
	if !HasStorageType(engine, "tabular") {
		t.Fatal("expected tabular family to match")
	}
	if HasStorageType(engine, "object") {
		t.Fatal("did not expect object family to match")
	}
}

func TestStructuredCapabilitiesComputeEntrypoints(t *testing.T) {
	caps := `{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"jupyter",
		"engine_family":"script",
		"compute":{"script":{"supported":true,"modes":["notebook"],"languages":["python"]}}
	}`
	capabilities := models.JSONString(caps)
	engine := &models.Engine{Capabilities: &capabilities}

	if !SupportsComputeEntrypoint(engine, "notebook") {
		t.Fatal("expected script capability to support notebook compute entrypoint")
	}
	if SupportsComputeEntrypoint(engine, "workflow") {
		t.Fatal("did not expect workflow compute entrypoint")
	}
}

func TestCapabilityFilterRejectsLegacyCapabilities(t *testing.T) {
	capabilities := models.JSONString(`{"compute":[{"dev_modes":["workflow"]}]}`)
	engine := &models.Engine{Capabilities: &capabilities}

	if HasStorageCapability(engine) {
		t.Fatal("legacy capabilities should not expose storage capability")
	}
	if SupportsComputeEntrypoint(engine, "workflow") {
		t.Fatal("legacy capabilities should not expose workflow entrypoint")
	}
	if _, err := ParseCapabilities(&capabilities); err == nil {
		t.Fatal("ParseCapabilities() error = nil, want legacy schema rejection")
	}
}

func TestCapabilityFilterRejectsUnsupportedSchemaVersion(t *testing.T) {
	capabilities := models.JSONString(`{
		"schema_version":"engine.capabilities/v0",
		"engine_type":"geopython_workflow",
		"engine_family":"workflow",
		"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1"}}
	}`)
	engine := &models.Engine{Capabilities: &capabilities}

	if SupportsComputeEntrypoint(engine, "workflow") {
		t.Fatal("unsupported capabilities schema_version should not expose workflow entrypoint")
	}
	if _, err := ParseCapabilities(&capabilities); err == nil {
		t.Fatal("ParseCapabilities() error = nil, want schema version rejection")
	}
}
