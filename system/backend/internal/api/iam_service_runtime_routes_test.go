package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonauth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestServiceRuntimeEngineDescriptorsAreTenantScopedAndMasked(t *testing.T) {
	router := newEngineDescriptorServiceRuntimeRouter(t, serviceRuntimeDescriptorContext(t, "tenant", true))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/engine-descriptors?page=1&page_size=10", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data  []models.EngineRuntimeDescriptor `json:"data"`
		Total int64                            `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 2 || len(payload.Data) != 2 {
		t.Fatalf("descriptor count = %d/%d, want 2/2", payload.Total, len(payload.Data))
	}
	if payload.Data[0].RuntimeEndpoint == nil || payload.Data[0].RuntimeEndpoint.Host != "workflow.internal" {
		t.Fatalf("workflow runtime endpoint = %#v", payload.Data[0].RuntimeEndpoint)
	}
	if payload.Data[1].RuntimeEndpoint != nil {
		t.Fatalf("data engine exposed runtime endpoint: %#v", payload.Data[1].RuntimeEndpoint)
	}
	for _, forbidden := range []string{"connection_info", "secret-value", "tenant-8.internal", "other_database"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("descriptor response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestServiceRuntimeEngineDescriptorsRequireTenantPermission(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		contextType string
		permission  bool
	}{
		{name: "platform context", contextType: "platform", permission: true},
		{name: "missing permission", contextType: "tenant", permission: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := newEngineDescriptorServiceRuntimeRouter(
				t,
				serviceRuntimeDescriptorContext(t, testCase.contextType, testCase.permission),
			)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/engine-descriptors", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403, body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestRegisterIAMServiceRuntimeRoutesRejectsMissingAuthorizationHandlers(t *testing.T) {
	router := gin.New()
	api := router.Group("/api/v1/system")
	runtime := &IAMRuntime{
		Authentication:       func(c *gin.Context) { c.Next() },
		ServiceCredential:    func(c *gin.Context) { c.Next() },
		InternalAuditHandler: &IAMInternalAuditHandler{},
	}
	if err := RegisterIAMServiceRuntimeRoutes(
		api,
		runtime,
		NewModuleRegistryHandler(nil),
		NewTaskProviderHandler(nil),
		&EngineHandler{},
	); err == nil {
		t.Fatal("RegisterIAMServiceRuntimeRoutes() accepted missing authorization handlers")
	}
}

func newEngineDescriptorServiceRuntimeRouter(t *testing.T, authContext commonauth.AuthContext) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	tenant3 := uint(3)
	tenant8 := uint(8)
	capabilities, err := engineplugin.MarshalEngineCapabilities(
		engineplugin.NewWorkflowCapabilities("geopython_workflow", engineplugin.WorkflowRuntimeAPIAddpV1),
	)
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON := models.JSONString(capabilities)
	for _, engine := range []models.Engine{
		{
			TenantID: &tenant3, Name: "Workflow", EngineType: "geopython_workflow",
			EngineOrigin: "extension", LifecycleState: models.EngineLifecycleActive,
			Capabilities: &capabilitiesJSON, ConnectionStatus: "online",
			ConnectionInfo: models.ConnectionInfo{
				"protocol": "http", "host": "workflow.internal", "port": 8099,
			},
		},
		{
			TenantID: &tenant3, Name: "PostgreSQL", EngineType: "postgresql",
			EngineOrigin: "general", LifecycleState: models.EngineLifecycleActive,
			ConnectionInfo: models.ConnectionInfo{
				"host": "database.internal", "port": 5432, "database": "business", "password": "secret-value",
			},
		},
		{
			TenantID: &tenant8, Name: "Other Tenant", EngineType: "postgresql",
			EngineOrigin: "general", LifecycleState: models.EngineLifecycleActive,
			ConnectionInfo: models.ConnectionInfo{
				"host": "tenant-8.internal", "port": 5432, "database": "other_database",
			},
		},
	} {
		engine := engine
		if err := db.Create(&engine).Error; err != nil {
			t.Fatal(err)
		}
	}

	runtime := &IAMRuntime{
		Authentication: func(c *gin.Context) {
			if err := sharedauth.SetAuthContextForGin(c, authContext); err != nil {
				panic(err)
			}
			c.Next()
		},
		ServiceCredential:               func(c *gin.Context) { c.Next() },
		InternalAuditHandler:            &IAMInternalAuditHandler{},
		ExecutionAuthorizationHandler:   &IAMExecutionAuthorizationHandler{},
		TaskAuthorizationSubjectHandler: &IAMTaskAuthorizationSubjectHandler{},
		CatalogReferenceHandler:         &IAMCatalogReferenceHandler{},
		PlatformTenantHandler:           &IAMPlatformTenantHandler{},
	}
	router := gin.New()
	api := router.Group("/api/v1/system")
	engineHandler := NewEngineHandler(service.NewEngineService(repository.NewEngineRepository(db), nil, nil))
	if err := RegisterIAMServiceRuntimeRoutes(
		api,
		runtime,
		NewModuleRegistryHandler(nil),
		NewTaskProviderHandler(nil),
		engineHandler,
	); err != nil {
		t.Fatal(err)
	}
	return router
}

func serviceRuntimeDescriptorContext(t *testing.T, contextType string, permission bool) commonauth.AuthContext {
	t.Helper()
	authContext := testIAMServiceActorContext(contextType, "addp-develop")
	if permission {
		scope := commonauth.AssignmentScope{Type: "platform"}
		if contextType == "tenant" {
			tenantID := "3"
			scope = commonauth.AssignmentScope{Type: "tenant", TenantID: &tenantID}
		}
		authContext.Authorization.RoleAssignments = []commonauth.RoleAssignment{{
			AssignmentID: "901",
			RoleKey:      "tenant.develop_runtime",
			Scope:        scope,
			Permissions:  []string{"system.engine_descriptor.read"},
			SourceType:   "bootstrap",
			ValidFrom:    authContext.Token.IssuedAt,
		}}
	}
	return authContext
}
