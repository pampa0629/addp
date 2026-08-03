package service

import (
	"bytes"
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
	"github.com/addp/common/engine/plugin"
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

func TestJupyterServiceOpenInteractiveSessionUsesStandardControlPlane(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/interactive-sessions" || r.Method != http.MethodPost {
			t.Fatalf("runtime request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service" {
			t.Fatalf("Authorization = %q", got)
		}
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["session_id"] != "abc-123" || request["tenant_id"] != float64(7) {
			t.Fatalf("request = %#v", request)
		}
		if request["owner_api_endpoint"] != "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors" || request["owner_capability_token"] != "addp_nkc_kernel-secret" {
			t.Fatalf("owner capability request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success", "session_id": "abc-123", "endpoint": runtimeServerURL(r),
			"runtime_token": "runtime-secret", "notebook_name": "analysis.ipynb",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	defer runtimeServer.Close()

	service := newJupyterServiceForRuntimeTest(t, runtimeServer.URL)
	session, controlURL, err := service.OpenInteractiveSession(context.Background(), 7, 10, plugin.InteractiveScriptSessionRequest{
		SessionID: "abc-123", TenantID: 7, UserID: 9, TaskID: 11,
		NotebookPath: "analysis.ipynb", Kernel: "python3",
		BasePath: "/api/v1/develop/notebook-sessions/abc-123/", TTLSeconds: 3600,
		OwnerAPIEndpoint:     "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors",
		OwnerCapabilityToken: "addp_nkc_kernel-secret",
	})
	if err != nil {
		t.Fatalf("OpenInteractiveSession() error = %v", err)
	}
	if controlURL != runtimeServer.URL || session.RuntimeToken != "runtime-secret" {
		t.Fatalf("session = %#v, controlURL = %q", session, controlURL)
	}
}

func TestJupyterServiceListQueryEnginesReturnsOnlyActiveQueryDescriptors(t *testing.T) {
	queryCapabilitiesJSON, err := dbbridge.GenerateCapabilities("postgresql")
	if err != nil {
		t.Fatal(err)
	}
	queryCapabilities := commonModels.JSONString(queryCapabilitiesJSON)
	notebookCapabilitiesJSON, err := dbbridge.GenerateCapabilities("jupyter")
	if err != nil {
		t.Fatal(err)
	}
	notebookCapabilities := commonModels.JSONString(notebookCapabilitiesJSON)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/runtime/engine-descriptors" || r.URL.Query().Get("page") != "1" {
			t.Fatalf("System request = %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []commonModels.EngineRuntimeDescriptor{
				{ID: 21, Name: "PostgreSQL", EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive, Capabilities: &queryCapabilities},
				{ID: 22, Name: "Disabled PostgreSQL", EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleDisabled, Capabilities: &queryCapabilities},
				{ID: 23, Name: "Jupyter", EngineType: "jupyter", LifecycleState: commonModels.EngineLifecycleActive, Capabilities: &notebookCapabilities},
			},
			"total": 3, "page": 1, "page_size": 100,
		})
	}))
	t.Cleanup(systemServer.Close)
	tokens := jupyterTestTokenSource("addp_at_service")
	service := NewJupyterService(commonClient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()), tokens)

	engines, err := service.ListQueryEngines(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListQueryEngines() error = %v", err)
	}
	if len(engines) != 1 || engines[0].ID != 21 {
		t.Fatalf("query engines = %#v", engines)
	}
	encoded, err := json.Marshal(engines)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || string(encoded) == "null" || bytes.Contains(encoded, []byte("connection_info")) {
		t.Fatalf("query engine response leaked connection info: %s", encoded)
	}
}

func runtimeServerURL(request *http.Request) string {
	return "http://" + request.Host
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
	tokens := jupyterTestTokenSource("addp_at_service")
	return NewJupyterService(
		commonClient.NewSystemServiceClient(systemServer.URL, tokens, systemServer.Client()),
		tokens,
	)
}

type jupyterTestTokenSource string

func (s jupyterTestTokenSource) Token(context.Context, uint) (string, error) {
	return string(s), nil
}

func (s jupyterTestTokenSource) PlatformToken(context.Context) (string, error) {
	return string(s), nil
}
