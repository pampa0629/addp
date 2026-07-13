package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	commonutils "github.com/addp/common/utils"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListFiltersBeforePagination(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Engine{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	userRepo := repository.NewUserRepository(db)
	engineRepo := repository.NewEngineRepository(db)
	admin := &models.User{
		Username:     "engine-list-admin",
		Email:        "engine-list-admin@example.com",
		PasswordHash: "test",
		UserType:     models.UserTypeSuperAdmin,
		IsActive:     true,
	}
	if err := userRepo.Create(admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	storageCapabilities := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"postgresql",
		"engine_family":"tabular",
		"storage":{}
	}`)
	computeCapabilities := toJSONStringPtr(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"custom_workflow",
		"engine_family":"workflow",
		"compute":{"workflow":{"supported":true}}
	}`)
	for i := 0; i < 9; i++ {
		if err := engineRepo.Create(&models.Engine{
			Name:           fmt.Sprintf("Storage %d", i),
			EngineType:     "postgresql",
			EngineOrigin:   "general",
			ConnectionInfo: models.ConnectionInfo{},
			IsActive:       true,
			Capabilities:   storageCapabilities,
		}); err != nil {
			t.Fatalf("create storage engine %d: %v", i, err)
		}
	}
	for i := 0; i < 4; i++ {
		if err := engineRepo.Create(&models.Engine{
			Name:           fmt.Sprintf("Workflow %d", i),
			EngineType:     "custom_workflow",
			EngineOrigin:   "extension",
			ConnectionInfo: models.ConnectionInfo{},
			IsActive:       true,
			IsBuiltin:      i == 0,
			Capabilities:   computeCapabilities,
		}); err != nil {
			t.Fatalf("create workflow engine %d: %v", i, err)
		}
	}

	engineService := NewEngineService(engineRepo, userRepo, nil, nil)
	engines, total, err := engineService.List(1, 10, EngineListFilter{
		CapabilityGroups: []string{"compute"},
		EngineOrigins:    []string{"extension"},
		IncludeBuiltin:   true,
	}, admin.ID)
	if err != nil {
		t.Fatalf("list filtered engines: %v", err)
	}
	if total != 4 || len(engines) != 4 {
		t.Fatalf("filtered total/page size = %d/%d, want 4/4", total, len(engines))
	}

	engines, total, err = engineService.List(1, 10, EngineListFilter{
		CapabilityGroups: []string{"compute"},
		EngineOrigins:    []string{"extension"},
		IncludeBuiltin:   false,
	}, admin.ID)
	if err != nil {
		t.Fatalf("list filtered non-builtin engines: %v", err)
	}
	if total != 3 || len(engines) != 3 {
		t.Fatalf("non-builtin filtered total/page size = %d/%d, want 3/3", total, len(engines))
	}
}

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
		"engine_type":"geopython_workflow",
		"engine_family":"workflow",
		"compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}},
		"extensions":{"vendor":{"distribution":"runtime"}}
	}`)
	engine := &models.Engine{
		EngineType:   "geopython_workflow",
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
	if capabilities.EngineType != "geopython_workflow" || capabilities.EngineFamily != "workflow" {
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

func TestEnrichInstanceCapabilitiesBindsFirstAvailableSuperMapWorkflowEngine(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}

	repo := repository.NewEngineRepository(db)
	service := NewEngineService(repo, nil, nil, nil)
	tenantID := uint(1)
	firstRuntime := &models.Engine{
		Name:           "SuperMap Runtime A",
		EngineType:     "supermap_workflow",
		TenantID:       &tenantID,
		IsActive:       true,
		ConnectionInfo: models.ConnectionInfo{},
	}
	if err := repo.Create(firstRuntime); err != nil {
		t.Fatalf("create first runtime: %v", err)
	}
	secondRuntime := &models.Engine{
		Name:           "SuperMap Runtime B",
		EngineType:     "supermap_workflow",
		TenantID:       &tenantID,
		IsActive:       true,
		ConnectionInfo: models.ConnectionInfo{},
	}
	if err := repo.Create(secondRuntime); err != nil {
		t.Fatalf("create second runtime: %v", err)
	}

	caps := engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{})
	caps.Extensions = map[string]interface{}{
		engineplugin.EngineExtensionSpatialWorkspaces: []engineplugin.SpatialWorkspaceFact{
			{
				Ecosystem:         "supermap",
				Kind:              "sdx+",
				State:             engineplugin.SpatialWorkspaceStateNotDetected,
				BackendEngineType: "postgresql",
				RuntimeEngineType: "supermap_workflow",
				CanEnable:         true,
				RiskLevel:         engineplugin.SpatialWorkspaceRiskHigh,
			},
		},
	}
	payload, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities: %v", err)
	}

	enriched, err := service.enrichInstanceCapabilities(&models.Engine{TenantID: &tenantID, EngineType: "postgresql"}, payload)
	if err != nil {
		t.Fatalf("enrichInstanceCapabilities: %v", err)
	}
	parsed, err := engineplugin.ParseEngineCapabilities(enriched)
	if err != nil {
		t.Fatalf("parse enriched capabilities: %v", err)
	}
	workspaces, err := engineplugin.SpatialWorkspacesFromExtensions(parsed.Extensions)
	if err != nil {
		t.Fatalf("SpatialWorkspacesFromExtensions: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("spatial workspaces = %#v, want 1", workspaces)
	}
	if workspaces[0].BoundRuntimeEngineID == nil || *workspaces[0].BoundRuntimeEngineID != firstRuntime.ID {
		t.Fatalf("bound runtime engine id = %#v, want first runtime id %d", workspaces[0].BoundRuntimeEngineID, firstRuntime.ID)
	}
	if !workspaces[0].CanEnable {
		t.Fatalf("can_enable = false, want true when runtime exists and workspace is not detected")
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

func TestEnableSpatialWorkspaceInvokesBoundSuperMapWorkflowRuntime(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Engine{}); err != nil {
		t.Fatalf("auto migrate models: %v", err)
	}

	var healthCalls int
	var listCalls int
	var invokeCalls int
	var invokePayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			healthCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
				"dependencies": map[string]interface{}{
					"objectsjava": map[string]interface{}{
						"available": true,
						"path":      "/opt/supermap/objectsjava/bin_linux_arm64",
					},
					"gpa_libs": map[string]interface{}{
						"available": true,
						"path":      "/opt/supermap/gpa/libs",
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			listCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"operators": []map[string]interface{}{
					{
						"id":              "datasource.enable_postgis",
						"name":            "datasource.enable_postgis",
						"display_name":    "启用 PostGIS 空间工作区",
						"engine_type":     "supermap_workflow",
						"category":        "数据源",
						"category_path":   []string{"数据源"},
						"description":     "对已有 PostgreSQL/PostGIS 数据库执行 SuperMap SDX+ 初始化。",
						"execution_modes": []string{"direct"},
						"parameters": []map[string]interface{}{
							{"name": "connection_info", "type": "object", "param_type": "param", "required": true},
							{"name": "alias", "type": "string", "param_type": "param", "required": false},
						},
						"output_ports": []map[string]interface{}{
							{"name": "workspace", "type": "supermap.spatial_workspace", "is_default": true},
						},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/operators/datasource.enable_postgis/invoke":
			invokeCalls++
			if err := json.NewDecoder(r.Body).Decode(&invokePayload); err != nil {
				t.Fatalf("decode invoke payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":       "success",
				"execution_id": "invoke-1",
				"workspace": map[string]interface{}{
					"kind":    "sdx+",
					"enabled": true,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	userRepo := repository.NewUserRepository(db)
	engineRepo := repository.NewEngineRepository(db)
	service := NewEngineService(engineRepo, userRepo, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	tenantID := uint(1)
	user := &models.User{
		Username:     "tenant-admin",
		UserType:     models.UserTypeTenantAdmin,
		IsActive:     true,
		TenantID:     &tenantID,
		FullName:     "Tenant Admin",
		Email:        "tenant-admin@example.com",
		PasswordHash: "hash",
	}
	if err := userRepo.Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	runtimeURL := systemTestWorkflowConnectionInfo(t, server.URL)
	runtimeEngine := &models.Engine{
		Name:           "SuperMap Runtime",
		EngineType:     "supermap_workflow",
		TenantID:       &tenantID,
		IsActive:       true,
		ConnectionInfo: models.ConnectionInfo(runtimeURL),
	}
	if err := engineRepo.Create(runtimeEngine); err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}
	loadedRuntime, err := engineRepo.GetByID(runtimeEngine.ID)
	if err != nil {
		t.Fatalf("load runtime engine: %v", err)
	}
	if loadedRuntime.ConnectionInfo["host"] != "127.0.0.1" {
		t.Fatalf("loaded runtime host = %#v, want 127.0.0.1", loadedRuntime.ConnectionInfo["host"])
	}
	if port, ok := loadedRuntime.ConnectionInfo["port"].(float64); !ok || port <= 0 {
		t.Fatalf("loaded runtime port = %#v, want a positive port", loadedRuntime.ConnectionInfo["port"])
	}

	targetCapabilities, err := engineplugin.MarshalEngineCapabilities(engineplugin.EngineCapabilities{
		SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
		EngineType:    "custom_postgis",
		EngineFamily:  "tabular",
		Extensions: map[string]interface{}{
			engineplugin.EngineExtensionSpatialWorkspaces: []engineplugin.SpatialWorkspaceFact{
				{
					Ecosystem:         "supermap",
					Kind:              "sdx+",
					State:             engineplugin.SpatialWorkspaceStateNotDetected,
					BackendEngineType: "postgresql",
					RuntimeEngineType: "supermap_workflow",
					CanEnable:         true,
					RiskLevel:         engineplugin.SpatialWorkspaceRiskHigh,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}

	targetEngine := &models.Engine{
		Name:           "PostGIS 数据库",
		EngineType:     "custom_postgis",
		TenantID:       &tenantID,
		IsActive:       true,
		ConnectionInfo: models.ConnectionInfo{"host": "127.0.0.1", "port": 5432, "database": "gisdb", "user": "supermap"},
		Capabilities:   toJSONStringPtr(targetCapabilities),
	}
	if err := engineRepo.Create(targetEngine); err != nil {
		t.Fatalf("create target engine: %v", err)
	}

	updated, err := service.EnableSpatialWorkspace(context.Background(), targetEngine.ID, "supermap", "sdx+", user.ID)
	if err != nil {
		t.Fatalf("EnableSpatialWorkspace: %v", err)
	}
	if updated == nil || updated.ID != targetEngine.ID {
		t.Fatalf("updated engine = %#v, want target engine %d", updated, targetEngine.ID)
	}
	if healthCalls != 1 || listCalls != 1 || invokeCalls != 1 {
		t.Fatalf("runtime calls = health:%d list:%d invoke:%d, want 1/1/1", healthCalls, listCalls, invokeCalls)
	}
	if invokePayload == nil {
		t.Fatal("invoke payload not captured")
	}
	params, ok := invokePayload["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("invoke params = %#v, want object", invokePayload["params"])
	}
	if params["alias"] != targetEngine.Name {
		t.Fatalf("alias = %#v, want %q", params["alias"], targetEngine.Name)
	}
	connInfo, ok := params["connection_info"].(map[string]interface{})
	if !ok {
		t.Fatalf("connection_info = %#v, want object", params["connection_info"])
	}
	if connInfo["database"] != "gisdb" || connInfo["user"] != "supermap" {
		t.Fatalf("connection_info = %#v, want target PostGIS credentials", connInfo)
	}
}

func systemTestWorkflowConnectionInfo(t *testing.T, rawURL string) map[string]interface{} {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split test server host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return map[string]interface{}{
		"protocol": parsed.Scheme,
		"host":     host,
		"port":     port,
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

func TestDecryptStoredConnectionInfoRejectsPlainSensitiveValue(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	_, err := service.decryptStoredConnectionInfo(models.ConnectionInfo{
		"host":     "localhost",
		"password": "plain-password",
	})
	if err == nil {
		t.Fatal("decryptStoredConnectionInfo succeeded, want error for plaintext sensitive value")
	}
	if !strings.Contains(err.Error(), "解密字段 password 失败") {
		t.Fatalf("error = %q, want password decrypt failure", err.Error())
	}
}

func TestDecryptStoredConnectionInfoReturnsPlainConnectionInfo(t *testing.T) {
	key := []byte("addp-dev-encryption-key-2025!!!!")
	secret, err := commonutils.Encrypt("plain-password", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := NewEngineService(&repository.EngineRepository{}, nil, key, nil)

	connInfo, err := service.decryptStoredConnectionInfo(models.ConnectionInfo{
		"host":     "localhost",
		"password": secret,
	})
	if err != nil {
		t.Fatalf("decryptStoredConnectionInfo: %v", err)
	}
	if connInfo["password"] != "plain-password" {
		t.Fatalf("password = %q, want plaintext", connInfo["password"])
	}
	if connInfo["host"] != "localhost" {
		t.Fatalf("host = %q, want localhost", connInfo["host"])
	}
}

func TestConnectionInfoStorageRoundTrip(t *testing.T) {
	key := []byte("addp-dev-encryption-key-2025!!!!")
	service := NewEngineService(&repository.EngineRepository{}, nil, key, nil)
	original := models.ConnectionInfo{
		"host":       "localhost",
		"password":   "business_password",
		"access_key": "business-access-key",
	}

	stored, err := service.encryptConnectionInfoForStorage(original)
	if err != nil {
		t.Fatalf("encryptConnectionInfoForStorage: %v", err)
	}
	if stored["password"] == original["password"] || stored["access_key"] == original["access_key"] {
		t.Fatal("stored sensitive fields must be encrypted")
	}
	if stored["host"] != original["host"] {
		t.Fatalf("stored host = %q, want unchanged host", stored["host"])
	}

	plain, err := service.decryptStoredConnectionInfo(stored)
	if err != nil {
		t.Fatalf("decryptStoredConnectionInfo: %v", err)
	}
	for key, value := range original {
		if plain[key] != value {
			t.Fatalf("round-trip %s = %q, want %q", key, plain[key], value)
		}
	}
}

func TestMergePlainConnectionInfoPreservesSensitiveValueForMaskedOverride(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	merged := service.mergePlainConnectionInfo(
		models.ConnectionInfo{
			"host":     "localhost",
			"password": "business_password",
		},
		models.ConnectionInfo{
			"host":     "127.0.0.1",
			"password": "******",
		},
	)

	if merged["host"] != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1", merged["host"])
	}
	if merged["password"] != "business_password" {
		t.Fatalf("password = %q, want original plaintext", merged["password"])
	}
}

func TestMergePlainConnectionInfoReplacesSensitiveValue(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	merged := service.mergePlainConnectionInfo(
		models.ConnectionInfo{"password": "old-password"},
		models.ConnectionInfo{"password": "new-password"},
	)

	if merged["password"] != "new-password" {
		t.Fatalf("password = %q, want new-password", merged["password"])
	}
}
