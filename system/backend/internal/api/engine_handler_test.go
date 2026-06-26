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
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

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
						"engine_type":     "python_workflow",
						"type":            "raster",
						"category":        "Raster",
						"category_path":   []string{"Raster"},
						"description":     "Convert TIFF to COG",
						"execution_modes": []string{"direct"},
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
	if !strings.Contains(err.Error(), "engine_type=python_workflow") || !strings.Contains(err.Error(), "runtime engine_type=acme_geo_workflow") {
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
