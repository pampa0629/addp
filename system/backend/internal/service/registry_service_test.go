package service

import (
	"context"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	systemmodels "github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegisterBuiltinRuntimeCreatesThenUpdatesSameEngine(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&systemmodels.Engine{}); err != nil {
		t.Fatal(err)
	}
	service := NewRegistryService(repository.NewEngineRepository(db))

	firstID, err := service.RegisterBuiltinRuntime(context.Background(), "duckdb", "http://localhost:8104", "first")
	if err != nil {
		t.Fatalf("first RegisterBuiltinRuntime() error = %v", err)
	}
	secondID, err := service.RegisterBuiltinRuntime(context.Background(), "duckdb", "http://duckdb-engine:8104", "updated")
	if err != nil {
		t.Fatalf("second RegisterBuiltinRuntime() error = %v", err)
	}
	if firstID == 0 || secondID != firstID {
		t.Fatalf("runtime IDs = (%d, %d), want same non-zero ID", firstID, secondID)
	}

	var engines []systemmodels.Engine
	if err := db.Find(&engines).Error; err != nil {
		t.Fatal(err)
	}
	if len(engines) != 1 || engines[0].TenantID != nil || !engines[0].IsBuiltin || engines[0].EngineType != "duckdb" {
		t.Fatalf("registered engines = %#v", engines)
	}
	if engines[0].ConnectionInfo["host"] != "duckdb-engine" || engines[0].Description != "updated" {
		t.Fatalf("updated runtime = %#v", engines[0])
	}
}

func TestPrepareRegistrationCapabilitiesKeepsSubmittedSchemaForBuiltinExternalRuntime(t *testing.T) {
	service := NewRegistryService(&repository.EngineRepository{})
	submitted := &engineplugin.EngineCapabilities{
		SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
		EngineType:    "geopython_workflow",
		EngineFamily:  "workflow",
		Extensions:    map[string]interface{}{"runtime": true},
	}
	req := &models.CapabilityRegistrationRequest{
		EngineType:   "GeoPython_Workflow",
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
	if capabilities.EngineType != "geopython_workflow" || capabilities.EngineFamily != "workflow" {
		t.Fatalf("unexpected builtin capabilities identity: %#v", capabilities)
	}
	if req.EngineType != "geopython_workflow" {
		t.Fatalf("engine_type was not canonicalized: %q", req.EngineType)
	}
	if capabilities.Extensions == nil || capabilities.Extensions["runtime"] != true {
		t.Fatalf("builtin external runtime capabilities were not preserved: %#v", capabilities.Extensions)
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

func TestRuntimeConnectionInfo(t *testing.T) {
	connectionInfo, err := runtimeConnectionInfo("https://duckdb.internal:8104")
	if err != nil {
		t.Fatalf("runtimeConnectionInfo() error = %v", err)
	}
	if connectionInfo["protocol"] != "https" || connectionInfo["host"] != "duckdb.internal" || connectionInfo["port"] != 8104 {
		t.Fatalf("connection info = %#v", connectionInfo)
	}
	for _, invalid := range []string{"", "ftp://duckdb:8104", "http://duckdb:0", "http://duckdb:8104/api", "http://user@duckdb:8104"} {
		if _, err := runtimeConnectionInfo(invalid); err == nil {
			t.Fatalf("runtimeConnectionInfo(%q) error = nil", invalid)
		}
	}
}

func TestFilterComputeEnginesUsesStructuredSupportedEntrypoints(t *testing.T) {
	workflow := models.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"geopython_workflow",
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
		{EngineType: "geopython_workflow", Capabilities: &workflow},
		{EngineType: "empty_compute", Capabilities: &emptyCompute},
		{EngineType: "unsupported", Capabilities: &unsupported},
		{EngineType: "legacy", Capabilities: &legacy},
		{EngineType: "nil"},
	}

	filtered := filterComputeEngines(engines)
	if len(filtered) != 1 {
		t.Fatalf("filtered engines = %#v, want only workflow engine", filtered)
	}
	if filtered[0].EngineType != "geopython_workflow" {
		t.Fatalf("filtered engine_type = %q, want geopython_workflow", filtered[0].EngineType)
	}
}
