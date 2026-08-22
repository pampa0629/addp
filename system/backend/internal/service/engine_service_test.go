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
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
	"github.com/addp/common/events"
	commonsecurity "github.com/addp/common/security"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListReturnsCompleteFilteredResult(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	engineRepo := repository.NewEngineRepository(db)
	tenantID := uint(1)

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
			LifecycleState: models.EngineLifecycleActive,
			TenantID:       &tenantID,
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
			LifecycleState: models.EngineLifecycleActive,
			IsBuiltin:      i == 0,
			TenantID:       &tenantID,
			Capabilities:   computeCapabilities,
		}); err != nil {
			t.Fatalf("create workflow engine %d: %v", i, err)
		}
	}

	engineService := NewEngineService(engineRepo, nil, nil)
	engines, err := engineService.List(EngineListFilter{
		CapabilityGroups: []string{"compute"},
		EngineOrigins:    []string{"extension"},
		IncludeBuiltin:   true,
	}, tenantID)
	if err != nil {
		t.Fatalf("list filtered engines: %v", err)
	}
	if len(engines) != 4 {
		t.Fatalf("filtered list size = %d, want 4", len(engines))
	}

	engines, err = engineService.List(EngineListFilter{
		CapabilityGroups: []string{"compute"},
		EngineOrigins:    []string{"extension"},
		IncludeBuiltin:   false,
	}, tenantID)
	if err != nil {
		t.Fatalf("list filtered non-builtin engines: %v", err)
	}
	if len(engines) != 3 {
		t.Fatalf("non-builtin filtered list size = %d, want 3", len(engines))
	}
}

func TestGetForExecutionRejectsOfflineEngine(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	repo := repository.NewEngineRepository(db)
	tenantID := uint(7)
	engine := &models.Engine{
		Name:             "offline database",
		EngineType:       "postgresql",
		EngineOrigin:     "general",
		ConnectionInfo:   models.ConnectionInfo{"host": "database.internal", "port": 5432},
		LifecycleState:   models.EngineLifecycleActive,
		ConnectionStatus: models.EngineConnectionOffline,
		TenantID:         &tenantID,
	}
	if err := repo.Create(engine); err != nil {
		t.Fatalf("create engine: %v", err)
	}

	engineService := NewEngineService(repo, nil, nil)
	if _, err := engineService.GetForExecution(engine.ID, tenantID); !errors.Is(err, ErrResourceForbidden) {
		t.Fatalf("GetForExecution() error = %v, want ErrResourceForbidden", err)
	}

	engine.ConnectionStatus = models.EngineConnectionOnline
	if err := repo.Update(engine); err != nil {
		t.Fatalf("mark engine online: %v", err)
	}
	if _, err := engineService.GetForExecution(engine.ID, tenantID); err != nil {
		t.Fatalf("GetForExecution() with online engine error = %v", err)
	}
}

func TestRuntimeDescriptorsExposeComputeRuntimeEndpointsAndNoDataConnection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewEngineRepository(db)
	tenantID := uint(7)
	workflowCapabilities, err := engineplugin.MarshalEngineCapabilities(
		engineplugin.NewWorkflowCapabilities("acme_workflow", engineplugin.WorkflowRuntimeAPIAddpV1),
	)
	if err != nil {
		t.Fatal(err)
	}
	workflowCapabilitiesJSON := models.JSONString(workflowCapabilities)
	inferenceCapabilities, err := engineplugin.MarshalEngineCapabilities(
		engineplugin.NewInferenceCapabilities("inference_runtime", []string{"chat", "embedding", "rerank"}, []string{"text", "image"}, false),
	)
	if err != nil {
		t.Fatal(err)
	}
	inferenceCapabilitiesJSON := models.JSONString(inferenceCapabilities)
	workflow := &models.Engine{
		TenantID: &tenantID, Name: "workflow", EngineType: "acme_workflow",
		EngineOrigin: "extension", LifecycleState: models.EngineLifecycleActive,
		Capabilities: &workflowCapabilitiesJSON,
		ConnectionInfo: models.ConnectionInfo{
			"protocol": "http", "host": "workflow.internal", "port": 8099,
		},
	}
	inference := &models.Engine{
		Name: "inference", EngineType: "inference_runtime", EngineOrigin: "extension",
		LifecycleState: models.EngineLifecycleActive, IsBuiltin: true,
		Capabilities: &inferenceCapabilitiesJSON,
		ConnectionInfo: models.ConnectionInfo{
			"protocol": "http", "host": "inference.internal", "port": 8191,
		},
	}
	data := &models.Engine{
		TenantID: &tenantID, Name: "database", EngineType: "postgresql",
		EngineOrigin: "general", LifecycleState: models.EngineLifecycleActive,
		Capabilities: toJSONStringPtr(`{"schema_version":"engine.capabilities/v1","engine_type":"postgresql","engine_family":"tabular","storage":{}}`),
		ConnectionInfo: models.ConnectionInfo{
			"host": "database.internal", "port": 5432, "database": "business", "password": "must-not-be-read",
		},
	}
	if err := repo.Create(workflow); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(inference); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(data); err != nil {
		t.Fatal(err)
	}

	descriptors, total, err := NewEngineService(repo, nil, nil).ListRuntimeDescriptors(
		1, 10, EngineListFilter{IncludeBuiltin: true}, tenantID,
	)
	if err != nil {
		t.Fatalf("ListRuntimeDescriptors() error = %v", err)
	}
	if total != 3 || len(descriptors) != 3 {
		t.Fatalf("descriptors total/len = %d/%d", total, len(descriptors))
	}
	if descriptors[0].RuntimeEndpoint == nil || descriptors[0].RuntimeEndpoint.Host != "workflow.internal" {
		t.Fatalf("workflow descriptor = %#v", descriptors[0])
	}
	if descriptors[1].RuntimeEndpoint == nil || descriptors[1].RuntimeEndpoint.Host != "inference.internal" {
		t.Fatalf("inference descriptor = %#v", descriptors[1])
	}
	if descriptors[2].RuntimeEndpoint != nil {
		t.Fatalf("data descriptor exposed runtime endpoint: %#v", descriptors[2])
	}
	encoded, err := json.Marshal(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"connection_info", "database.internal", "business", "must-not-be-read"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("descriptor serialization leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestEnsureCapabilitiesForEngineUsesStructuredPluginSchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)

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
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
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
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)

	if _, err := service.ensureCapabilitiesForEngine("custom_runtime", nil); !errors.Is(err, ErrUnsupportedEngineType) {
		t.Fatalf("ensure capabilities error = %v, want ErrUnsupportedEngineType", err)
	}
}

func TestValidateCapabilitiesRejectsLegacySchema(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
	legacy := toJSONStringPtr(`{"storage":[{"type":"relational_db","engine":"postgresql"}]}`)

	if err := service.validateCapabilities(legacy); err == nil {
		t.Fatal("expected legacy capabilities without schema_version to be rejected")
	}
}

func TestValidateCapabilitiesRejectsUnsupportedSchemaVersion(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
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
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
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
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
	legacy := toJSONStringPtr(`{"compute":[{"dev_modes":["workflow"]}]}`)

	if !service.shouldRefreshCapabilities(nil) {
		t.Fatal("expected nil capabilities to be refreshed")
	}
	if !service.shouldRefreshCapabilities(legacy) {
		t.Fatal("expected legacy capabilities to be refreshed")
	}
}

func TestPrepareEngineCapabilitiesKeepsSubmittedSchemaForBuiltinExternalRuntime(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
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
	if capabilities.Extensions == nil || capabilities.Extensions["vendor"] == nil {
		t.Fatalf("builtin external runtime capabilities were not preserved: %#v", capabilities.Extensions)
	}
}

func TestPrepareEngineCapabilitiesKeepsStructuredCapabilitiesForNonBuiltinEngine(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
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

func TestEnrichInstanceCapabilitiesBindsRuntimeProvidingRequiredDirectOperator(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}

	repo := repository.NewEngineRepository(db)
	service := NewEngineService(repo, nil, nil)
	tenantID := uint(1)
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"operators": []map[string]interface{}{
				systemTestWorkflowOperator("tenant_runtime_a", "unrelated.operator", []string{"direct"}),
			},
		})
	}))
	defer firstServer.Close()
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"operators": []map[string]interface{}{
				systemTestWorkflowOperator("tenant_runtime_b", "datasource.enable_postgresql", []string{"direct"}),
			},
		})
	}))
	defer secondServer.Close()
	firstRuntime := &models.Engine{
		Name:           "Tenant Runtime A",
		EngineType:     "tenant_runtime_a",
		TenantID:       &tenantID,
		LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo(systemTestWorkflowConnectionInfo(t, firstServer.URL)),
		Capabilities:   systemTestWorkflowCapabilities(t, "tenant_runtime_a"),
	}
	if err := repo.Create(firstRuntime); err != nil {
		t.Fatalf("create first runtime: %v", err)
	}
	secondRuntime := &models.Engine{
		Name:           "Tenant Runtime B",
		EngineType:     "tenant_runtime_b",
		TenantID:       &tenantID,
		LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo(systemTestWorkflowConnectionInfo(t, secondServer.URL)),
		Capabilities:   systemTestWorkflowCapabilities(t, "tenant_runtime_b"),
	}
	if err := repo.Create(secondRuntime); err != nil {
		t.Fatalf("create second runtime: %v", err)
	}

	caps := engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{})
	caps.Extensions = map[string]interface{}{
		engineplugin.EngineExtensionSpatialWorkspaces: []engineplugin.SpatialWorkspaceFact{
			{
				Ecosystem:         "supermap",
				Kind:              engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL,
				State:             engineplugin.SpatialWorkspaceStateNotDetected,
				BackendEngineType: "postgresql",
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
	if workspaces[0].BoundRuntimeEngineID == nil || *workspaces[0].BoundRuntimeEngineID != secondRuntime.ID {
		t.Fatalf("bound runtime engine id = %#v, want capable runtime id %d", workspaces[0].BoundRuntimeEngineID, secondRuntime.ID)
	}
	if !workspaces[0].CanEnable {
		t.Fatalf("can_enable = false, want true when runtime exists and workspace is not detected")
	}
}

func TestWorkflowRuntimeOnlineReconcilesExistingSpatialWorkspaceBinding(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}

	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/operators" {
			http.NotFound(w, r)
			return
		}
		operators := make([]map[string]interface{}, 0, len(supermapworkflow.RequiredTableOperators()))
		for _, operatorName := range supermapworkflow.RequiredTableOperators() {
			operators = append(operators, systemTestWorkflowOperator("supermap_workflow", operatorName, []string{"direct"}))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"operators": operators})
	}))
	defer runtimeServer.Close()

	repo := repository.NewEngineRepository(db)
	service := NewEngineService(repo, nil, nil)
	tenantID := uint(1)
	runtimeEngine := &models.Engine{
		Name:             "SuperMap Workflow Runtime",
		EngineType:       "supermap_workflow",
		IsBuiltin:        true,
		LifecycleState:   models.EngineLifecycleActive,
		ConnectionStatus: "unknown",
		ConnectionInfo:   models.ConnectionInfo(systemTestWorkflowConnectionInfo(t, runtimeServer.URL)),
		Capabilities:     systemTestWorkflowCapabilities(t, "supermap_workflow"),
	}
	if err := repo.Create(runtimeEngine); err != nil {
		t.Fatalf("create runtime engine: %v", err)
	}

	spatialCapabilities := engineplugin.NewTabularCapabilities("postgresql", "schema", engineplugin.TabularCapabilityOptions{})
	spatialCapabilities.Extensions = map[string]interface{}{
		engineplugin.EngineExtensionSpatialWorkspaces: []engineplugin.SpatialWorkspaceFact{
			{
				Ecosystem:         "supermap",
				Kind:              engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL,
				State:             engineplugin.SpatialWorkspaceStateDetected,
				BackendEngineType: "postgresql",
				RiskLevel:         engineplugin.SpatialWorkspaceRiskHigh,
			},
		},
	}
	spatialPayload, err := engineplugin.MarshalEngineCapabilities(spatialCapabilities)
	if err != nil {
		t.Fatalf("marshal spatial capabilities: %v", err)
	}
	spatialEngine := &models.Engine{
		Name:             "SuperMap SDX+ for PostgreSQL",
		EngineType:       "postgresql",
		TenantID:         &tenantID,
		LifecycleState:   models.EngineLifecycleActive,
		ConnectionStatus: "online",
		Capabilities:     toJSONStringPtr(spatialPayload),
	}
	if err := repo.Create(spatialEngine); err != nil {
		t.Fatalf("create spatial engine: %v", err)
	}

	if err := service.RecordConnectionStatus(runtimeEngine.ID, "online", "连接正常"); err != nil {
		t.Fatalf("RecordConnectionStatus: %v", err)
	}

	updated, err := repo.GetByID(spatialEngine.ID)
	if err != nil {
		t.Fatalf("load spatial engine: %v", err)
	}
	parsed, err := engineplugin.ParseEngineCapabilities(string(*updated.Capabilities))
	if err != nil {
		t.Fatalf("parse spatial capabilities: %v", err)
	}
	workspaces, err := engineplugin.SpatialWorkspacesFromExtensions(parsed.Extensions)
	if err != nil {
		t.Fatalf("read spatial workspaces: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].BoundRuntimeEngineID == nil || *workspaces[0].BoundRuntimeEngineID != runtimeEngine.ID {
		t.Fatalf("bound runtime = %#v, want runtime engine %d", workspaces, runtimeEngine.ID)
	}
}

func TestPrepareEngineCapabilitiesRejectsMismatchedCustomCapabilities(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
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
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate models: %v", err)
	}

	var listCalls int
	var invokeCalls int
	var invokePayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			listCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"operators": []map[string]interface{}{
					systemTestWorkflowOperator("tenant_supermap_runtime", "datasource.enable_postgis", []string{"direct"}),
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
					"kind":    engineplugin.SpatialWorkspaceSuperMapSDXPostGIS,
					"enabled": true,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engineRepo := repository.NewEngineRepository(db)
	service := NewEngineService(engineRepo, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	tenantID := uint(1)
	runtimeURL := systemTestWorkflowConnectionInfo(t, server.URL)
	runtimeEngine := &models.Engine{
		Name:           "SuperMap Runtime",
		EngineType:     "tenant_supermap_runtime",
		TenantID:       &tenantID,
		LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo(runtimeURL),
		Capabilities:   systemTestWorkflowCapabilities(t, "tenant_supermap_runtime"),
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
					Ecosystem:            "supermap",
					Kind:                 engineplugin.SpatialWorkspaceSuperMapSDXPostGIS,
					State:                engineplugin.SpatialWorkspaceStateNotDetected,
					BackendEngineType:    "postgresql",
					BoundRuntimeEngineID: &runtimeEngine.ID,
					CanEnable:            true,
					RiskLevel:            engineplugin.SpatialWorkspaceRiskHigh,
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
		LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo{"host": "127.0.0.1", "port": 5432, "database": "gisdb", "user": "supermap"},
		Capabilities:   toJSONStringPtr(targetCapabilities),
	}
	if err := engineRepo.Create(targetEngine); err != nil {
		t.Fatalf("create target engine: %v", err)
	}

	updated, err := service.EnableSpatialWorkspace(
		context.Background(),
		targetEngine.ID,
		"supermap",
		engineplugin.SpatialWorkspaceSuperMapSDXPostGIS,
		tenantID,
	)
	if err != nil {
		t.Fatalf("EnableSpatialWorkspace: %v", err)
	}
	if updated == nil || updated.ID != targetEngine.ID {
		t.Fatalf("updated engine = %#v, want target engine %d", updated, targetEngine.ID)
	}
	if listCalls != 1 || invokeCalls != 1 {
		t.Fatalf("runtime calls = list:%d invoke:%d, want 1/1", listCalls, invokeCalls)
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

func systemTestWorkflowCapabilities(t *testing.T, engineType string) *models.JSONString {
	t.Helper()
	payload, err := engineplugin.MarshalEngineCapabilities(engineplugin.NewWorkflowCapabilities(engineType, engineplugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("marshal workflow capabilities: %v", err)
	}
	return toJSONStringPtr(payload)
}

func systemTestWorkflowOperator(engineType, name string, modes []string) map[string]interface{} {
	return map[string]interface{}{
		"id":              name,
		"name":            name,
		"display_name":    name,
		"engine_type":     engineType,
		"type":            "table",
		"category":        "SuperMap",
		"category_path":   []string{"SuperMap"},
		"description":     "SuperMap direct operator",
		"parameters":      []map[string]interface{}{},
		"output_ports":    []map[string]interface{}{},
		"execution_modes": modes,
		"effects":         []string{"ddl"},
	}
}

func TestValidateSystemEngineTypeRejectsSQLiteAndSpatiaLite(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)

	for _, engineType := range []string{"sqlite", "spatialite"} {
		if err := service.ValidateSystemEngineType(engineType); !errors.Is(err, ErrUnsupportedEngineType) {
			t.Fatalf("ValidateSystemEngineType(%q) error = %v, want ErrUnsupportedEngineType", engineType, err)
		}
	}
}

func TestValidateSystemEngineTypeAcceptsRegisteredPlugin(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)

	if err := service.ValidateSystemEngineType("postgresql"); err != nil {
		t.Fatalf("ValidateSystemEngineType(postgresql): %v", err)
	}
}

func TestDecryptStoredConnectionInfoRejectsPlainSensitiveValue(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	_, err := service.decryptStoredConnectionInfo("postgresql", models.ConnectionInfo{
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
	secret, err := commonsecurity.Encrypt("plain-password", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	service := NewEngineService(&repository.EngineRepository{}, key, nil)

	connInfo, err := service.decryptStoredConnectionInfo("postgresql", models.ConnectionInfo{
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
	service := NewEngineService(&repository.EngineRepository{}, key, nil)
	original := models.ConnectionInfo{
		"bootstrap_servers": "broker.internal:9092",
		"password":          "business_password",
		"tls_client_key":    "private-key-pem",
		"tls_client_cert":   "client-cert-pem",
	}

	stored, err := service.encryptConnectionInfoForStorage("kafka", original)
	if err != nil {
		t.Fatalf("encryptConnectionInfoForStorage: %v", err)
	}
	if stored["password"] == original["password"] || stored["tls_client_key"] == original["tls_client_key"] {
		t.Fatal("stored sensitive fields must be encrypted")
	}
	if stored["tls_client_cert"] != original["tls_client_cert"] {
		t.Fatalf("stored tls_client_cert = %q, want unchanged certificate", stored["tls_client_cert"])
	}

	plain, err := service.decryptStoredConnectionInfo("kafka", stored)
	if err != nil {
		t.Fatalf("decryptStoredConnectionInfo: %v", err)
	}
	for key, value := range original {
		if plain[key] != value {
			t.Fatalf("round-trip %s = %q, want %q", key, plain[key], value)
		}
	}
}

func TestKafkaSensitiveFieldsUsePluginDeclaration(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
	masked := service.maskSensitiveFields("kafka", models.ConnectionInfo{
		"bootstrap_servers": "broker.internal:9092",
		"password":          "secret",
		"tls_client_cert":   "client-cert-pem",
		"tls_client_key":    "private-key-pem",
	})

	if masked["password"] != "******" || masked["tls_client_key"] != "******" {
		t.Fatalf("masked Kafka secrets = %#v", masked)
	}
	if masked["tls_client_cert"] != "client-cert-pem" {
		t.Fatalf("tls_client_cert = %q, want unchanged certificate", masked["tls_client_cert"])
	}
}

func TestMergePlainConnectionInfoPreservesKafkaTLSClientKeyPlaceholder(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
	merged := service.mergePlainConnectionInfo(
		"kafka",
		models.ConnectionInfo{"tls_client_key": "private-key-pem"},
		models.ConnectionInfo{"tls_client_key": "********"},
	)

	if merged["tls_client_key"] != "private-key-pem" {
		t.Fatalf("tls_client_key = %q, want original private key", merged["tls_client_key"])
	}
}

func TestMergePlainConnectionInfoPreservesSensitiveValueForMaskedOverride(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	merged := service.mergePlainConnectionInfo(
		"postgresql",
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
	service := NewEngineService(&repository.EngineRepository{}, []byte("addp-dev-encryption-key-2025!!!!"), nil)

	merged := service.mergePlainConnectionInfo(
		"postgresql",
		models.ConnectionInfo{"password": "old-password"},
		models.ConnectionInfo{"password": "new-password"},
	)

	if merged["password"] != "new-password" {
		t.Fatalf("password = %q, want new-password", merged["password"])
	}
}

func TestValidateConnectionIdentityUnchangedForPostgreSQL(t *testing.T) {
	service := NewEngineService(&repository.EngineRepository{}, nil, nil)
	original := models.ConnectionInfo{
		"host":     "db.internal",
		"database": "analytics",
	}

	for _, test := range []struct {
		name    string
		updated models.ConnectionInfo
		wantErr bool
	}{
		{
			name: "default port is equivalent to explicit port",
			updated: models.ConnectionInfo{
				"host":     "db.internal",
				"port":     5432,
				"database": "analytics",
			},
		},
		{
			name: "credentials may change",
			updated: models.ConnectionInfo{
				"host":     "db.internal",
				"database": "analytics",
				"user":     "new-user",
				"password": "new-password",
			},
		},
		{
			name: "host is immutable",
			updated: models.ConnectionInfo{
				"host":     "db-new.internal",
				"database": "analytics",
			},
			wantErr: true,
		},
		{
			name: "port is immutable",
			updated: models.ConnectionInfo{
				"host":     "db.internal",
				"port":     5433,
				"database": "analytics",
			},
			wantErr: true,
		},
		{
			name: "database is immutable",
			updated: models.ConnectionInfo{
				"host":     "db.internal",
				"database": "warehouse",
			},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := service.validateConnectionIdentityUnchanged("postgresql", original, test.updated)
			if test.wantErr && !errors.Is(err, ErrEngineIdentityImmutable) {
				t.Fatalf("validateConnectionIdentityUnchanged() error = %v, want ErrEngineIdentityImmutable", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateConnectionIdentityUnchanged() error = %v", err)
			}
		})
	}
}

func TestMongoConnectionIdentityUsesAuthenticationPrincipal(t *testing.T) {
	service := &EngineService{}
	original := models.ConnectionInfo{
		"host": "mongo.internal", "port": 27017, "user": "reader",
		"auth_source": "", "database": "business", "password": "old",
	}

	passwordRotation := models.ConnectionInfo{
		"host": "mongo.internal", "port": 27017, "user": "reader",
		"auth_source": "admin", "database": "Outdoor", "password": "new",
	}
	if err := service.validateConnectionIdentityUnchanged("mongodb", original, passwordRotation); err != nil {
		t.Fatalf("default database and password must be mutable: %v", err)
	}

	changedUser := models.ConnectionInfo{
		"host": "mongo.internal", "port": 27017, "user": "writer", "auth_source": "admin",
	}
	if err := service.validateConnectionIdentityUnchanged("mongodb", original, changedUser); !errors.Is(err, ErrEngineIdentityImmutable) {
		t.Fatalf("changing MongoDB user must change identity, got %v", err)
	}
}

func TestUpdateRejectsInvalidLifecycleAndDeletingEngine(t *testing.T) {
	repo := newEngineServiceTestRepository(t)
	tenantID := uint(7)
	engine := &models.Engine{
		Name:           "Runtime",
		EngineType:     "custom_runtime",
		EngineOrigin:   "extension",
		TenantID:       &tenantID,
		LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo{"protocol": "http", "host": "runtime.internal", "port": 8080},
		Capabilities:   customRuntimeCapabilities(),
	}
	if err := repo.Create(engine); err != nil {
		t.Fatalf("create engine: %v", err)
	}
	service := NewEngineService(repo, nil, nil)

	invalid := "deleting"
	if _, err := service.Update(engine.ID, tenantID, &models.EngineUpdateRequest{LifecycleState: &invalid}); !errors.Is(err, ErrInvalidEngineLifecycle) {
		t.Fatalf("Update() error = %v, want ErrInvalidEngineLifecycle", err)
	}

	engine.LifecycleState = models.EngineLifecycleDeleting
	if err := repo.Update(engine); err != nil {
		t.Fatalf("mark deleting: %v", err)
	}
	description := "updated"
	if _, err := service.Update(engine.ID, tenantID, &models.EngineUpdateRequest{Description: &description}); !errors.Is(err, ErrEngineDeleting) {
		t.Fatalf("Update() error = %v, want ErrEngineDeleting", err)
	}
}

func TestUpdateMetadataAndLifecycleDoesNotProbeOfflineEngine(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewEngineRepository(db)
	tenantID := uint(7)
	capabilities := toJSONStringPtr(`{"schema_version":"engine.capabilities/v1","engine_type":"postgresql","engine_family":"tabular","storage":{}}`)
	engine := &models.Engine{
		TenantID: &tenantID, Name: "offline", EngineType: "postgresql", EngineOrigin: "general",
		LifecycleState: models.EngineLifecycleActive, ConnectionStatus: "offline", Capabilities: capabilities,
		ConnectionInfo: models.ConnectionInfo{"host": "127.0.0.1", "port": 1, "database": "offline", "user": "offline"},
	}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}

	description := "仍可维护的离线引擎"
	disabled := models.EngineLifecycleDisabled
	updated, err := NewEngineService(repo, nil, nil).Update(engine.ID, tenantID, &models.EngineUpdateRequest{
		Description: &description, LifecycleState: &disabled,
	})
	if err != nil {
		t.Fatalf("Update() probed an offline engine: %v", err)
	}
	if updated.Description != description || updated.LifecycleState != disabled {
		t.Fatalf("updated engine = %#v", updated)
	}
	if updated.Capabilities == nil || string(*updated.Capabilities) != string(*capabilities) {
		t.Fatalf("capabilities changed: got %v want %v", updated.Capabilities, capabilities)
	}
}

func TestBeginDeletionWaitsForCleanupBeforeDeletingEngine(t *testing.T) {
	repo := newEngineServiceTestRepository(t)
	tenantID := uint(7)
	actorID := uint(42)
	engine := createDeletionTestEngine(t, repo, tenantID)
	impact := deletionTestImpact(t, events.CleanupImpactRebindable, "dev_task:12")
	cleanup := &engineDeletionCleanupStub{
		assessmentTaskIDs:  []string{"assessment-1", "validation-1"},
		assessmentStatuses: []string{"completed", "completed"},
		assessmentImpacts:  []events.CleanupImpactData{impact, impact},
		executeTaskID:      "execute-1",
		executeStatus:      "completed",
		validationGate:     make(chan struct{}),
	}
	service := NewEngineService(repo, nil, nil)
	service.cleanup = cleanup

	assessmentID, err := service.CreateDeletionAssessment(engine.ID, tenantID, actorID, models.ExternalArtifactPolicyAbandon)
	if err != nil {
		t.Fatalf("CreateDeletionAssessment() error = %v", err)
	}
	started, err := service.BeginDeletion(engine.ID, tenantID, actorID, &models.EngineDeleteRequest{
		AssessmentID:           assessmentID,
		ConfirmationToken:      engine.Name,
		ExternalArtifactPolicy: models.ExternalArtifactPolicyAbandon,
	})
	if err != nil {
		t.Fatalf("BeginDeletion() error = %v", err)
	}
	if started.LifecycleState != models.EngineLifecycleDeleting || started.DeletionScanTaskID == nil || *started.DeletionScanTaskID != "validation-1" {
		t.Fatalf("deletion start = %#v", started)
	}
	stored, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatalf("engine was deleted before cleanup completed: %v", err)
	}
	if stored.LifecycleState != models.EngineLifecycleDeleting {
		t.Fatalf("stored lifecycle_state = %q, want deleting", stored.LifecycleState)
	}
	if len(cleanup.assessmentContexts) != 2 || cleanup.assessmentContexts[0]["assessment_phase"] != "preflight" || cleanup.assessmentContexts[1]["assessment_phase"] != "validation" {
		t.Fatalf("assessment contexts = %#v", cleanup.assessmentContexts)
	}

	close(cleanup.validationGate)
	waitForEngineDeleted(t, repo, engine.ID)
	if cleanup.executeMode != events.CleanupModePhysical || cleanup.executeActorID != actorID || !cleanup.confirmation.Confirmed || cleanup.confirmation.ConfirmationToken != "CONFIRM" {
		t.Fatalf("execute request mode=%q actor=%d confirmation=%#v", cleanup.executeMode, cleanup.executeActorID, cleanup.confirmation)
	}
}

func TestBeginDeletionKeepsEngineWhenCleanupFails(t *testing.T) {
	repo := newEngineServiceTestRepository(t)
	tenantID := uint(7)
	engine := createDeletionTestEngine(t, repo, tenantID)
	impact := deletionTestImpact(t, events.CleanupImpactWillDisable, "transfer_task:9")
	cleanup := &engineDeletionCleanupStub{
		assessmentTaskIDs:  []string{"assessment-1", "validation-failed"},
		assessmentStatuses: []string{"completed", "completed_with_errors"},
		assessmentImpacts:  []events.CleanupImpactData{impact, impact},
	}
	service := NewEngineService(repo, nil, nil)
	service.cleanup = cleanup

	assessmentID, err := service.CreateDeletionAssessment(engine.ID, tenantID, 42, models.ExternalArtifactPolicyDelete)
	if err != nil {
		t.Fatalf("CreateDeletionAssessment() error = %v", err)
	}
	if _, err := service.BeginDeletion(engine.ID, tenantID, 42, &models.EngineDeleteRequest{
		AssessmentID:           assessmentID,
		ConfirmationToken:      engine.Name,
		ExternalArtifactPolicy: models.ExternalArtifactPolicyDelete,
	}); err != nil {
		t.Fatalf("BeginDeletion() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, err := repo.GetByID(engine.ID)
		if err != nil {
			t.Fatalf("engine deleted after failed cleanup: %v", err)
		}
		if strings.Contains(stored.DeletionError, "completed_with_errors") {
			if stored.LifecycleState != models.EngineLifecycleDeleting {
				t.Fatalf("lifecycle_state = %q, want deleting", stored.LifecycleState)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cleanup failure was not persisted")
}

type engineDeletionCleanupStub struct {
	assessmentTaskIDs  []string
	assessmentStatuses []string
	assessmentImpacts  []events.CleanupImpactData
	assessmentContexts []map[string]interface{}
	assessmentTasks    map[string]*models.TaskStatusResponse
	validationGate     chan struct{}
	executeTaskID      string
	executeStatus      string
	executeMode        string
	executeActorID     uint
	confirmation       CleanupExecuteConfirmation
}

func (s *engineDeletionCleanupStub) CreateEngineDeletionAssessment(_ context.Context, tenantID, _ uint, cleanupContext map[string]interface{}) (string, error) {
	index := len(s.assessmentContexts)
	if index >= len(s.assessmentTaskIDs) || index >= len(s.assessmentStatuses) || index >= len(s.assessmentImpacts) {
		return "", errors.New("unexpected deletion assessment")
	}
	contextCopy := make(map[string]interface{}, len(cleanupContext))
	for key, value := range cleanupContext {
		contextCopy[key] = value
	}
	s.assessmentContexts = append(s.assessmentContexts, contextCopy)
	if s.assessmentTasks == nil {
		s.assessmentTasks = make(map[string]*models.TaskStatusResponse)
	}
	taskID := s.assessmentTaskIDs[index]
	result := events.CleanupResultData{
		Module: "develop", Status: events.CleanupResultSuccess, Action: events.CleanupActionScan,
		TenantID: tenantID, TaskID: taskID, Impact: &s.assessmentImpacts[index],
	}
	s.assessmentTasks[taskID] = &models.TaskStatusResponse{
		TaskID:  taskID,
		Action:  events.CleanupActionScan,
		Status:  s.assessmentStatuses[index],
		Results: map[string]interface{}{"develop": result},
		Task: models.CleanupTask{
			TaskID: taskID, Action: events.CleanupActionScan, TenantID: tenantID,
			CauseEvent: events.CleanupCauseEngineDeleting, Status: s.assessmentStatuses[index],
			ExpectedModules: []string{"develop"}, Context: contextCopy,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}
	return taskID, nil
}

func (s *engineDeletionCleanupStub) CreateExecuteTask(_ context.Context, _ string, mode string, actorID uint, confirmation CleanupExecuteConfirmation) (string, error) {
	s.executeMode = mode
	s.executeActorID = actorID
	s.confirmation = confirmation
	return s.executeTaskID, nil
}

func (s *engineDeletionCleanupStub) GetTaskStatus(_ context.Context, taskID string) (*models.TaskStatusResponse, error) {
	if task, ok := s.assessmentTasks[taskID]; ok {
		if len(s.assessmentTaskIDs) > 1 && taskID == s.assessmentTaskIDs[1] && s.validationGate != nil {
			<-s.validationGate
		}
		return task, nil
	}
	return &models.TaskStatusResponse{Status: s.executeStatus}, nil
}

func deletionTestImpact(t *testing.T, disposition, stableRef string) events.CleanupImpactData {
	t.Helper()
	impact, err := events.BuildCleanupImpactData([]events.CleanupImpactItem{{
		StableRef: stableRef, Disposition: disposition,
	}}, "/develop/workflow")
	if err != nil {
		t.Fatalf("BuildCleanupImpactData() error = %v", err)
	}
	return impact
}

func newEngineServiceTestRepository(t *testing.T) *repository.EngineRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate engine: %v", err)
	}
	return repository.NewEngineRepository(db)
}

func customRuntimeCapabilities() *models.JSONString {
	return toJSONStringPtr(`{"schema_version":"engine.capabilities/v1","engine_type":"custom_runtime","engine_family":"workflow","compute":{"workflow":{"supported":true,"runtime_api":"addp.workflow/v1","dynamic_operators":true}}}`)
}

func createDeletionTestEngine(t *testing.T, repo *repository.EngineRepository, tenantID uint) *models.Engine {
	t.Helper()
	engine := &models.Engine{
		Name:                   "Deletion Test Engine",
		EngineType:             "custom_runtime",
		EngineOrigin:           "extension",
		TenantID:               &tenantID,
		LifecycleState:         models.EngineLifecycleActive,
		ExternalArtifactPolicy: models.ExternalArtifactPolicyDelete,
		ConnectionInfo:         models.ConnectionInfo{"protocol": "http", "host": "runtime.internal", "port": 8080},
		Capabilities:           customRuntimeCapabilities(),
	}
	if err := repo.Create(engine); err != nil {
		t.Fatalf("create engine: %v", err)
	}
	return engine
}

func waitForEngineDeleted(t *testing.T, repo *repository.EngineRepository, engineID uint) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := repo.GetByID(engineID); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("engine %d was not deleted after cleanup completed", engineID)
}
