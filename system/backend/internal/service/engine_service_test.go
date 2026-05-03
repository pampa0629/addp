package service

import (
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
	if capabilities.Storage == nil || len(capabilities.Storage.Families) != 1 || capabilities.Storage.Families[0] != "tabular" {
		t.Fatalf("storage families = %#v, want [tabular]", capabilities.Storage)
	}
}

func TestValidateCapabilitiesRejectsLegacySchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	legacy := toJSONStringPtr(`{"storage":[{"type":"relational_db","engine":"postgresql"}]}`)

	if err := service.validateCapabilities(legacy); err == nil {
		t.Fatal("expected legacy capabilities without schema_version to be rejected")
	}
}
