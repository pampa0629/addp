package api

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListEnginesRejectsInvalidLifecycleStates(t *testing.T) {
	router := newEngineListTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/engines?lifecycle_states=invalid", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	var response models.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.TrimSpace(response.Error) == "" {
		t.Fatalf("response = %#v, want non-empty error", response)
	}
}

func TestListEnginesAcceptsAllLifecycleStates(t *testing.T) {
	router := newEngineListTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/engines?lifecycle_states=active,disabled,deleting", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var response []models.EngineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 0 {
		t.Fatalf("response = %#v, want empty list", response)
	}
}

func TestListEnginesReturnsCompleteArray(t *testing.T) {
	tenantID := uint(3)
	engines := make([]models.Engine, 12)
	for i := range engines {
		engines[i] = models.Engine{
			TenantID:       &tenantID,
			Name:           "engine-" + strconv.Itoa(i+1),
			EngineType:     "postgresql",
			EngineOrigin:   "general",
			ConnectionInfo: models.ConnectionInfo{},
			LifecycleState: models.EngineLifecycleActive,
		}
	}
	router := newEngineListTestRouter(t, engines...)

	req := httptest.NewRequest(http.MethodGet, "/engines", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var response []models.EngineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, rec.Body.String())
	}
	if len(response) != len(engines) {
		t.Fatalf("response = %#v, want all %d engines in one response", response, len(engines))
	}
}

func newEngineListTestRouter(t *testing.T, engines ...models.Engine) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if len(engines) > 0 {
		if err := db.Create(&engines).Error; err != nil {
			t.Fatalf("seed engines: %v", err)
		}
	}

	handler := NewEngineHandler(service.NewEngineService(repository.NewEngineRepository(db), nil, nil))
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, testIAMActorContext("tenant")); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	router.GET("/engines", handler.List)
	return router
}

func TestTestConnectionBeforeCreateProbesCustomWorkflowRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engineType := "acme_geo_workflow"
	healthChecked := false
	operatorsListed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			healthChecked = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			operatorsListed = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"operators": []map[string]interface{}{
					{
						"id":              "tiff_to_cog",
						"name":            "tiff_to_cog",
						"display_name":    "TIFF to COG",
						"engine_type":     engineType,
						"type":            "raster",
						"category":        "Raster",
						"category_path":   []string{"Raster"},
						"description":     "Convert TIFF to COG",
						"execution_modes": []string{"direct"},
						"effects":         []string{"read", "write"},
						"parameters":      []map[string]interface{}{},
						"output_ports": []map[string]interface{}{
							{"name": "default", "type": "object", "is_default": true},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	capabilities, err := engineplugin.MarshalEngineCapabilities(engineplugin.NewWorkflowCapabilities(engineType, engineplugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities: %v", err)
	}
	body := map[string]interface{}{
		"engine_type":     engineType,
		"engine_origin":   "extension",
		"name":            "ACME Geo Workflow",
		"connection_info": systemTestWorkflowConnectionInfo(t, server.URL),
		"capabilities":    json.RawMessage(capabilities),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	router := gin.New()
	handler := NewEngineHandler(nil)
	router.POST("/engines/test-connection", handler.TestConnectionBeforeCreate)

	req := httptest.NewRequest(http.MethodPost, "/engines/test-connection", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["success"] != true {
		t.Fatalf("response = %#v, want success=true", response)
	}
	probe, ok := response["probe"].(map[string]interface{})
	if !ok {
		t.Fatalf("probe = %#v, want object", response["probe"])
	}
	if probe["runtime_protocol"] != engineplugin.WorkflowRuntimeAPIAddpV1 || probe["operators_count"] != float64(1) {
		t.Fatalf("probe = %#v, want workflow protocol and operator count", probe)
	}
	if !healthChecked || !operatorsListed {
		t.Fatalf("healthChecked=%v operatorsListed=%v, want both true", healthChecked, operatorsListed)
	}
}

func TestProbeWorkflowRuntimeBeforeSaveRejectsMismatchedOperatorEngineType(t *testing.T) {
	engineType := "acme_geo_workflow"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/operators":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"operators": []map[string]interface{}{
					{
						"id":              "tiff_to_cog",
						"name":            "tiff_to_cog",
						"display_name":    "TIFF to COG",
						"engine_type":     "geopython_workflow",
						"type":            "raster",
						"category":        "Raster",
						"category_path":   []string{"Raster"},
						"description":     "Convert TIFF to COG",
						"execution_modes": []string{"direct"},
						"effects":         []string{"read", "write"},
						"parameters":      []map[string]interface{}{},
						"output_ports": []map[string]interface{}{
							{"name": "default", "type": "object", "is_default": true},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	capabilities, err := engineplugin.MarshalEngineCapabilities(engineplugin.NewWorkflowCapabilities(engineType, engineplugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("MarshalEngineCapabilities: %v", err)
	}
	capabilitiesJSON := models.JSONString(capabilities)
	handler := NewEngineHandler(nil)

	err = handler.probeWorkflowRuntimeBeforeSave(&models.EngineCreateRequest{
		EngineType:     engineType,
		EngineOrigin:   "extension",
		Name:           "ACME Geo Workflow",
		ConnectionInfo: models.ConnectionInfo(systemTestWorkflowConnectionInfo(t, server.URL)),
		Capabilities:   &capabilitiesJSON,
	})
	if err == nil {
		t.Fatal("probeWorkflowRuntimeBeforeSave() error = nil, want engine_type mismatch")
	}
	if !strings.Contains(err.Error(), "engine_type=geopython_workflow") || !strings.Contains(err.Error(), "runtime engine_type=acme_geo_workflow") {
		t.Fatalf("error = %v, want operator/runtime engine_type mismatch", err)
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
