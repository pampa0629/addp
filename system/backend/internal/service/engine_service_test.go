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
