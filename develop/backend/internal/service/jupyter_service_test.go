package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
)

func TestJupyterServiceListKernelsUsesBoundEngineAndTenantServiceToken(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/kernels" {
			t.Fatalf("runtime path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "success",
			"kernels": []map[string]string{{"name": "python3", "display_name": "Python 3", "language": "python"}},
		})
	}))
	defer runtimeServer.Close()

	service := newJupyterServiceForRuntimeTest(t, runtimeServer.URL)
	kernels, err := service.ListKernels(context.Background(), 7, 10)
	if err != nil {
		t.Fatalf("ListKernels() error = %v", err)
	}
	if len(kernels) != 1 || kernels[0].Name != "python3" {
		t.Fatalf("kernels = %#v", kernels)
	}
}

func TestJupyterServiceExecuteNotebookUsesBoundEngineAndTenantServiceToken(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/execute" {
			t.Fatalf("runtime path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["tenant_id"] != float64(7) || body["notebook_path"] != "analysis.ipynb" {
			t.Fatalf("body = %#v", body)
		}
		parameters, ok := body["parameters"].(map[string]interface{})
		if !ok || parameters["limit"] != float64(10) {
			t.Fatalf("parameters = %#v", body["parameters"])
		}
		if _, exists := body["data_sources"]; exists {
			t.Fatalf("legacy data_sources must not be sent: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success", "output_path": "tenant_7/executions/result.ipynb",
		})
	}))
	defer runtimeServer.Close()

	service := newJupyterServiceForRuntimeTest(t, runtimeServer.URL)
	result, err := service.ExecuteNotebook(
		context.Background(), 7, 10,
		JupyterRuntimeExecutionRequest{
			TenantID: 7, NotebookPath: "analysis.ipynb",
			Parameters: map[string]interface{}{"limit": 10}, Kernel: "python3",
		},
		30*time.Second,
	)
	if err != nil {
		t.Fatalf("ExecuteNotebook() error = %v", err)
	}
	if result.OutputPath != "tenant_7/executions/result.ipynb" {
		t.Fatalf("OutputPath = %q", result.OutputPath)
	}
}

func TestValidateNotebookEngineDescriptorRejectsScriptEngineWithoutNotebookMode(t *testing.T) {
	capabilities := commonModels.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"custom_script",
		"engine_family":"compute",
		"compute":{"script":{"supported":true,"modes":["batch"],"languages":["python"]}}
	}`)
	err := validateNotebookEngineDescriptor(&commonModels.EngineRuntimeDescriptor{
		ID: 11, EngineType: "custom_script", LifecycleState: commonModels.EngineLifecycleActive,
		Capabilities: &capabilities,
	})
	if err == nil {
		t.Fatal("expected non-notebook Script Engine to be rejected")
	}
}

func newJupyterServiceForRuntimeTest(t *testing.T, runtimeURL string) *JupyterService {
	t.Helper()
	parsed, err := url.Parse(runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON, err := dbbridge.GenerateCapabilities("jupyter")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := commonModels.JSONString(capabilitiesJSON)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/runtime/engine-descriptors/10" {
			t.Fatalf("System path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service" {
			t.Fatalf("System Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{
			ID: 10, Name: "Jupyter Engine", EngineType: "jupyter",
			LifecycleState: commonModels.EngineLifecycleActive, Capabilities: &capabilities,
			RuntimeEndpoint: &commonModels.EngineRuntimeEndpoint{
				Protocol: parsed.Scheme, Host: parsed.Hostname(), Port: port,
			},
		})
	}))
	t.Cleanup(systemServer.Close)
	tokens := staticServiceTokenSource("addp_at_service")
	return NewJupyterService(
		commonClient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()),
		tokens,
	)
}
