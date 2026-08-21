package selection

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
	engines := []models.Engine{*engine}
	if got := FilterEnginesByCapability(engines, CapabilityFilter{StorageTypes: []string{"tabular"}}); len(got) != 1 {
		t.Fatalf("tabular filter returned %d engines, want 1", len(got))
	}
	if got := FilterEnginesByCapability(engines, CapabilityFilter{StorageTypes: []string{"object"}}); len(got) != 0 {
		t.Fatalf("object filter returned %d engines, want 0", len(got))
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

func TestStructuredCapabilitiesInferenceEntrypoint(t *testing.T) {
	caps := `{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"inference_runtime",
		"engine_family":"inference",
		"compute":{"inference":{"supported":true,"runtime_api":"addp.inference/v1","operations":["chat"]}}
	}`
	capabilities := models.JSONString(caps)
	engine := &models.Engine{Capabilities: &capabilities}

	if !SupportsComputeEntrypoint(engine, "inference") {
		t.Fatal("expected inference capability to support inference compute entrypoint")
	}
	if got := GetSupportedComputeEntrypoints(engine); len(got) != 1 || got[0] != "inference" {
		t.Fatalf("compute entrypoints = %#v, want inference", got)
	}
}

func TestAvailableEngineCandidateRequiresActiveOnlineAndCapability(t *testing.T) {
	caps := models.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"postgresql",
		"engine_family":"tabular",
		"storage":{"catalog":{"supported":true}},
		"compute":{"query":{"supported":true,"languages":["sql"]}}
	}`)
	base := models.Engine{
		LifecycleState:   models.EngineLifecycleActive,
		ConnectionStatus: models.EngineConnectionOnline,
		Capabilities:     &caps,
	}

	if !IsAvailableForComputeEntrypoint(&base, "query") || !IsAvailableStorageEngine(&base) {
		t.Fatal("active online engine with matching capabilities must be available")
	}

	for _, test := range []struct {
		name       string
		lifecycle  string
		connection string
	}{
		{name: "offline", lifecycle: models.EngineLifecycleActive, connection: models.EngineConnectionOffline},
		{name: "unknown", lifecycle: models.EngineLifecycleActive, connection: models.EngineConnectionUnknown},
		{name: "checking", lifecycle: models.EngineLifecycleActive, connection: models.EngineConnectionChecking},
		{name: "disabled", lifecycle: models.EngineLifecycleDisabled, connection: models.EngineConnectionOnline},
		{name: "deleting", lifecycle: models.EngineLifecycleDeleting, connection: models.EngineConnectionOnline},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			candidate.LifecycleState = test.lifecycle
			candidate.ConnectionStatus = test.connection
			if IsAvailable(&candidate) || IsAvailableForComputeEntrypoint(&candidate, "query") || IsAvailableStorageEngine(&candidate) {
				t.Fatalf("candidate lifecycle=%q connection=%q must not be available", test.lifecycle, test.connection)
			}
		})
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
