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

func TestEnsureCapabilitiesForEngineUsesStructuredPluginSchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)

	capabilitiesJSON, err := service.ensureCapabilitiesForEngine("postgresql", nil)
	if err != nil {
		t.Fatalf("ensure capabilities: %v", err)
	}

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

func TestEnsureCapabilitiesForPluginEngineIgnoresSubmittedCapabilities(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	submitted := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"postgresql",
		"engine_family":"custom",
		"extensions":{"runtime":true}
	}`)

	capabilitiesJSON, err := service.ensureCapabilitiesForEngine("postgresql", submitted)
	if err != nil {
		t.Fatalf("ensure capabilities: %v", err)
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(capabilitiesJSON)
	if err != nil {
		t.Fatalf("parse capabilities: %v", err)
	}
	if capabilities.EngineFamily != "tabular" {
		t.Fatalf("plugin capabilities should win, engine_family = %q", capabilities.EngineFamily)
	}
	if capabilities.Extensions != nil {
		t.Fatalf("plugin capabilities should ignore submitted extensions: %#v", capabilities.Extensions)
	}
}

func TestEnsureCapabilitiesForCustomEngineRequiresSubmittedCapabilities(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)

	if _, err := service.ensureCapabilitiesForEngine("custom_runtime", nil); !errors.Is(err, ErrUnsupportedEngineType) {
		t.Fatalf("ensure capabilities error = %v, want ErrUnsupportedEngineType", err)
	}
}

func TestValidateCapabilitiesRejectsLegacySchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	legacy := toJSONStringPtr(`{"storage":[{"type":"relational_db","engine":"postgresql"}]}`)

	if err := service.validateCapabilities(legacy); err == nil {
		t.Fatal("expected legacy capabilities without schema_version to be rejected")
	}
}

func TestValidateCapabilitiesRejectsUnsupportedSchemaVersion(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	unsupported := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v0",
		"engine_type":"postgresql",
		"engine_family":"tabular"
	}`)

	if err := service.validateCapabilities(unsupported); err == nil {
		t.Fatal("expected unsupported capabilities schema_version to be rejected")
	}
}

func TestShouldRefreshCapabilitiesKeepsValidStructuredSchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	valid := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"postgresql",
		"engine_family":"tabular",
		"storage":{"catalog":{"supported":true,"real_time":true}},
		"extensions":{"vendor":{"distribution":"community"}}
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

func TestPrepareEngineCapabilitiesUsesPluginSchemaForBuiltinEngine(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	submitted := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"python_workflow",
		"engine_family":"workflow",
		"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}},
		"extensions":{"vendor":{"distribution":"runtime"}}
	}`)
	engine := &models.Engine{
		EngineType:   "python_workflow",
		IsBuiltin:    true,
		Capabilities: submitted,
	}

	if err := service.prepareEngineCapabilities(engine); err != nil {
		t.Fatalf("prepareEngineCapabilities: %v", err)
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil {
		t.Fatalf("parse capabilities: %v", err)
	}
	if capabilities.EngineType != "python_workflow" || capabilities.EngineFamily != "workflow" {
		t.Fatalf("unexpected capabilities identity: %#v", capabilities)
	}
	if capabilities.Extensions != nil {
		t.Fatalf("builtin capabilities should come from plugin schema without runtime-submitted extensions: %#v", capabilities.Extensions)
	}
}

func TestPrepareEngineCapabilitiesKeepsStructuredCapabilitiesForNonBuiltinEngine(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	submitted := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"custom_runtime",
		"engine_family":"custom",
		"extensions":{"vendor":{"distribution":"runtime"}}
	}`)
	engine := &models.Engine{
		EngineType:   "custom_runtime",
		IsBuiltin:    false,
		Capabilities: submitted,
	}

	if err := service.prepareEngineCapabilities(engine); err != nil {
		t.Fatalf("prepareEngineCapabilities: %v", err)
	}
	if string(*engine.Capabilities) != string(*submitted) {
		t.Fatalf("non-builtin capabilities changed: got %s want %s", *engine.Capabilities, *submitted)
	}
}

func TestPrepareEngineCapabilitiesRejectsMismatchedCustomCapabilities(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil, nil)
	submitted := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"other_runtime",
		"engine_family":"custom"
	}`)
	engine := &models.Engine{
		EngineType:   "custom_runtime",
		IsBuiltin:    false,
		Capabilities: submitted,
	}

	if err := service.prepareEngineCapabilities(engine); err == nil || !strings.Contains(err.Error(), "engine_type 必须为 custom_runtime") {
		t.Fatalf("prepareEngineCapabilities error = %v, want engine_type mismatch", err)
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
