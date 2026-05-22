package service

import (
	"errors"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/repository"
)

func TestGenerateDefaultCapabilitiesUsesStructuredPluginSchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)

	capabilitiesJSON := service.generateDefaultCapabilities("postgresql")

	capabilities, err := engineplugin.ParseEngineCapabilities(capabilitiesJSON)
	if err != nil {
		t.Fatalf("parse capabilities: %v", err)
	}
	if capabilities.SchemaVersion != engineplugin.CapabilitiesSchemaVersion {
		t.Fatalf("schema version = %q, want %q", capabilities.SchemaVersion, engineplugin.CapabilitiesSchemaVersion)
	}
	if capabilities.EngineFamily != "tabular" || capabilities.Storage == nil {
		t.Fatalf("engine family/storage = %q/%#v, want tabular storage", capabilities.EngineFamily, capabilities.Storage)
	}
}

func TestValidateCapabilitiesRejectsLegacySchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	legacy := toJSONStringPtr(`{"storage":[{"type":"relational_db","engine":"postgresql"}]}`)

	if err := service.validateCapabilities(legacy); err == nil {
		t.Fatal("expected legacy capabilities without schema_version to be rejected")
	}
}

func TestShouldRefreshCapabilitiesKeepsValidStructuredSchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	valid := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"python_workflow",
		"engine_family":"workflow",
		"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}},
		"extensions":{"workflow_runtime":{"features":["dag"]}}
	}`)

	if service.shouldRefreshCapabilities(valid) {
		t.Fatal("expected valid structured capabilities with extensions to be kept")
	}
}

func TestShouldRefreshCapabilitiesRefreshesEmptyOrLegacySchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	legacy := toJSONStringPtr(`{"compute":[{"dev_modes":["workflow"]}]}`)

	if !service.shouldRefreshCapabilities(nil) {
		t.Fatal("expected nil capabilities to be refreshed")
	}
	if !service.shouldRefreshCapabilities(legacy) {
		t.Fatal("expected legacy capabilities to be refreshed")
	}
}

func TestValidateSystemEngineTypeRejectsSQLiteAndSpatiaLite(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)

	for _, engineType := range []string{"sqlite", "spatialite"} {
		if err := service.ValidateSystemEngineType(engineType); !errors.Is(err, ErrUnsupportedEngineType) {
			t.Fatalf("ValidateSystemEngineType(%q) error = %v, want ErrUnsupportedEngineType", engineType, err)
		}
	}
}

func TestValidateSystemEngineTypeAcceptsRegisteredPlugin(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)

	if err := service.ValidateSystemEngineType("postgresql"); err != nil {
		t.Fatalf("ValidateSystemEngineType(postgresql): %v", err)
	}
}
