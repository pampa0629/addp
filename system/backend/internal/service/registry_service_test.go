package service

import (
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/addp/system/internal/repository"
)

func TestPrepareRegistrationCapabilitiesUsesPluginSchemaForBuiltin(t *testing.T) {
	service := NewRegistryService(&repository.EngineRepository{})
	submitted := &engineplugin.EngineCapabilities{
		SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
		EngineType:    "python_workflow",
		EngineFamily:  "workflow",
		Extensions:    map[string]interface{}{"runtime": true},
	}
	req := &models.CapabilityRegistrationRequest{
		EngineType:   "Python_Workflow",
		IsBuiltin:    true,
		Capabilities: submitted,
	}

	capabilitiesJSON, err := service.prepareRegistrationCapabilities(req)
	if err != nil {
		t.Fatalf("prepareRegistrationCapabilities: %v", err)
	}
	if capabilitiesJSON == nil {
		t.Fatal("capabilitiesJSON is nil")
	}

	capabilities, err := engineplugin.ParseEngineCapabilities(*capabilitiesJSON)
	if err != nil {
		t.Fatalf("parse capabilities: %v", err)
	}
	if capabilities.EngineType != "python_workflow" || capabilities.EngineFamily != "workflow" {
		t.Fatalf("unexpected builtin capabilities identity: %#v", capabilities)
	}
	if req.EngineType != "python_workflow" {
		t.Fatalf("engine_type was not canonicalized: %q", req.EngineType)
	}
	if capabilities.Extensions != nil {
		t.Fatalf("builtin registry capabilities should come from plugin schema without submitted extensions: %#v", capabilities.Extensions)
	}
}

func TestPrepareRegistrationCapabilitiesKeepsStructuredCapabilitiesForNonBuiltin(t *testing.T) {
	service := NewRegistryService(&repository.EngineRepository{})
	submitted := &engineplugin.EngineCapabilities{
		SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
		EngineType:    "custom_runtime",
		EngineFamily:  "custom",
		Extensions:    map[string]interface{}{"runtime": true},
	}

	capabilitiesJSON, err := service.prepareRegistrationCapabilities(&models.CapabilityRegistrationRequest{
		EngineType:   "custom_runtime",
		IsBuiltin:    false,
		Capabilities: submitted,
	})
	if err != nil {
		t.Fatalf("prepareRegistrationCapabilities: %v", err)
	}
	if capabilitiesJSON == nil {
		t.Fatal("capabilitiesJSON is nil")
	}

	capabilities, err := engineplugin.ParseEngineCapabilities(*capabilitiesJSON)
	if err != nil {
		t.Fatalf("parse capabilities: %v", err)
	}
	if capabilities.EngineType != "custom_runtime" || capabilities.EngineFamily != "custom" {
		t.Fatalf("unexpected non-builtin capabilities: %#v", capabilities)
	}
	if capabilities.Extensions == nil || capabilities.Extensions["runtime"] != true {
		t.Fatalf("non-builtin submitted extensions were not preserved: %#v", capabilities.Extensions)
	}
}

func TestPrepareRegistrationCapabilitiesRejectsMissingOrMismatchedNonBuiltinCapabilities(t *testing.T) {
	service := NewRegistryService(&repository.EngineRepository{})

	if _, err := service.prepareRegistrationCapabilities(&models.CapabilityRegistrationRequest{
		EngineType: "custom_runtime",
	}); err == nil || !strings.Contains(err.Error(), "capabilities is required") {
		t.Fatalf("missing capabilities error = %v, want capabilities is required", err)
	}

	_, err := service.prepareRegistrationCapabilities(&models.CapabilityRegistrationRequest{
		EngineType: "custom_runtime",
		Capabilities: &engineplugin.EngineCapabilities{
			SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
			EngineType:    "other_runtime",
			EngineFamily:  "custom",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match engine_type") {
		t.Fatalf("mismatched capabilities error = %v, want engine_type mismatch", err)
	}
}

func TestFilterComputeEnginesUsesStructuredSupportedEntrypoints(t *testing.T) {
	workflow := models.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"python_workflow",
		"engine_family":"workflow",
		"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1"}}
	}`)
	emptyCompute := models.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"empty_compute",
		"engine_family":"custom",
		"compute":{}
	}`)
	unsupported := models.JSONString(`{
		"schema_version":"engine.capabilities/v0",
		"engine_type":"legacy",
		"engine_family":"workflow",
		"compute":{"workflow":{"supported":true}}
	}`)
	legacy := models.JSONString(`{"compute":[{"dev_modes":["workflow"]}]}`)

	engines := []*models.Engine{
		{EngineType: "python_workflow", Capabilities: &workflow},
		{EngineType: "empty_compute", Capabilities: &emptyCompute},
		{EngineType: "unsupported", Capabilities: &unsupported},
		{EngineType: "legacy", Capabilities: &legacy},
		{EngineType: "nil"},
	}

	filtered := filterComputeEngines(engines)
	if len(filtered) != 1 {
		t.Fatalf("filtered engines = %#v, want only workflow engine", filtered)
	}
	if filtered[0].EngineType != "python_workflow" {
		t.Fatalf("filtered engine_type = %q, want python_workflow", filtered[0].EngineType)
	}
}
