package service

import (
	"errors"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	commonutils "github.com/addp/common/utils"
	"github.com/addp/system/internal/models"
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

func TestDecryptSensitiveFieldsRejectsPlainSensitiveValue(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	_, err := service.decryptSensitiveFields(models.ConnectionInfo{
		"host":     "localhost",
		"password": "plain-password",
	})
	if err == nil {
		t.Fatal("decryptSensitiveFields succeeded, want error for plaintext sensitive value")
	}
	if !strings.Contains(err.Error(), "解密字段 password 失败") {
		t.Fatalf("error = %q, want password decrypt failure", err.Error())
	}
}

func TestDecryptSensitiveFieldsReturnsPlainConnectionInfo(t *testing.T) {
	key := []byte("addp-dev-encryption-key-2025!!!!")
	secret, err := commonutils.Encrypt("plain-password", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := NewEngineService(&repository.EngineRepository{}, nil, key, nil)

	connInfo, err := service.decryptSensitiveFields(models.ConnectionInfo{
		"host":     "localhost",
		"password": secret,
	})
	if err != nil {
		t.Fatalf("decryptSensitiveFields: %v", err)
	}
	if connInfo["password"] != "plain-password" {
		t.Fatalf("password = %q, want plaintext", connInfo["password"])
	}
	if connInfo["host"] != "localhost" {
		t.Fatalf("host = %q, want localhost", connInfo["host"])
	}
}
