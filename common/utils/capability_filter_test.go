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
		"storage":{"families":["tabular"]},
		"compute":{"query":{"supported":true,"languages":["sql"]}}
	}`
	engine := &models.Engine{Capabilities: &caps}

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

func TestStructuredCapabilitiesDevModes(t *testing.T) {
	caps := `{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"jupyter",
		"engine_family":"script",
		"compute":{"script":{"supported":true,"modes":["notebook"],"languages":["python"]}}
	}`
	engine := &models.Engine{Capabilities: &caps}

	if !SupportsDevMode(engine, "notebook") {
		t.Fatal("expected script capability to support notebook dev mode")
	}
	if SupportsDevMode(engine, "workflow") {
		t.Fatal("did not expect workflow dev mode")
	}
}
