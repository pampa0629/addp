package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	commoninference "github.com/addp/common/inference"
	"github.com/addp/common/logger"
	"github.com/addp/inference/internal/models"
)

func TestRuntimeUpstreamDiagnosticsPreserveErrorsAndExcludeSensitiveData(t *testing.T) {
	const secret = "sensitive-test-value"
	tests := []struct {
		name, payload, stage, code, errorType, param string
		status                                       int
		want                                         error
	}{
		{"parameter_rejected", `{"error":{"code":"unsupported_parameter","type":"invalid_request_error","param":"max_tokens","message":"` + secret + `"}}`, "http_status", "unsupported_parameter", "invalid_request_error", "max_tokens", 400, ErrUpstreamFailed},
		{"untrusted_metadata", `{"error":{"code":"` + secret + `","type":"` + secret + `","param":"` + secret + `","message":"` + secret + `"}}`, "http_status", "unclassified", "unclassified", "unclassified", 401, ErrUpstreamFailed},
		{"unavailable", `{"error":{"message":"` + secret + `"}}`, "http_status", "unclassified", "unclassified", "unclassified", 503, ErrUpstreamUnavailable},
		{"timeout", secret, "http_status", "unclassified", "unclassified", "unclassified", 504, ErrTimeout},
		{"invalid_json", secret, "decode_response", "unclassified", "unclassified", "unclassified", 200, ErrUpstreamFailed},
		{"short_response", secret, "read_response", "unclassified", "unclassified", "unclassified", 200, ErrUpstreamFailed},
		{"success", `{"result":"` + secret + `"}`, "", "", "", "", 200, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer "+secret {
					t.Error("credential was not sent to upstream")
				}
				if tc.name == "short_response" {
					w.Header().Set("Content-Length", "1000")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer upstream.Close()
			var output bytes.Buffer
			logger.Init(logger.Options{Writer: &output})
			t.Cleanup(func() { logger.Init(logger.Options{}) })
			resolved := &resolvedModel{
				provider:   &models.ProviderConnection{ID: "provider-id"},
				deployment: &models.ModelDeployment{ID: "deployment-id"}, credential: secret,
			}
			runtime := &Runtime{client: upstream.Client()}
			var response map[string]interface{}
			err := runtime.invokeAt(context.Background(), resolved, upstream.URL+"/v1/chat/completions?private="+secret,
				map[string]interface{}{"messages": secret, "tools": secret}, &response)
			if !errors.Is(err, tc.want) || calls.Load() != 1 {
				t.Fatalf("error=%v, calls=%d; want %v and exactly one call", err, calls.Load(), tc.want)
			}
			if strings.Contains(output.String(), secret) || strings.Contains(output.String(), upstream.URL) {
				t.Fatal("diagnostics exposed sensitive data or endpoint")
			}
			if tc.want == nil {
				if output.Len() != 0 {
					t.Fatal("successful call emitted failure diagnostics")
				}
				return
			}
			var event map[string]interface{}
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatalf("expected exactly one JSON diagnostic: %v", err)
			}
			for key, want := range map[string]interface{}{
				"stage": tc.stage, "upstream_http_status": float64(tc.status),
				"provider_connection_id": "provider-id", "model_deployment_id": "deployment-id",
				"upstream_error_code": tc.code, "upstream_error_type": tc.errorType, "upstream_error_param": tc.param,
			} {
				if event[key] != want {
					t.Errorf("%s=%v, want %v", key, event[key], want)
				}
			}
		})
	}
}

type failingDiagnosticTransport struct{ err error }

func (t failingDiagnosticTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func TestRuntimeTransportDiagnosticsDoNotLogRawErrors(t *testing.T) {
	for _, tc := range []struct {
		name, stage string
		cause, want error
	}{
		{"network", "transport", errors.New("sensitive-transport-error"), ErrUpstreamUnavailable},
		{"deadline", "timeout", context.DeadlineExceeded, ErrTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger.Init(logger.Options{Writer: &output})
			t.Cleanup(func() { logger.Init(logger.Options{}) })
			runtime := &Runtime{client: &http.Client{Transport: failingDiagnosticTransport{tc.cause}}}
			resolved := &resolvedModel{provider: &models.ProviderConnection{ID: "provider-id"}, deployment: &models.ModelDeployment{ID: "deployment-id"}}
			err := runtime.invokeAt(context.Background(), resolved, "https://sensitive-endpoint.test/v1", nil, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if strings.Contains(output.String(), "sensitive-") || strings.Contains(output.String(), tc.cause.Error()) {
				t.Fatal("transport diagnostics exposed raw error or endpoint")
			}
			var event map[string]interface{}
			if err := json.Unmarshal(output.Bytes(), &event); err != nil || event["stage"] != tc.stage {
				t.Fatalf("missing transport stage: %v", err)
			}
		})
	}
}

func TestChatResponseValidationDiagnosticsExcludeContent(t *testing.T) {
	for _, tc := range []struct{ name, payload, stage string }{
		{"empty", `{"choices":[],"private":"sensitive-content"}`, "empty_chat_choices"},
		{"invalid_role", `{"choices":[{"message":{"role":"user","content":"sensitive-content"}}]}`, "invalid_chat_message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.payload) }))
			defer upstream.Close()
			ctx := context.Background()
			control := NewControlPlane(newTestStore(t), testEncryptionKey)
			actor := Actor{ContextType: models.ScopeTenant, TenantID: 7, PrincipalID: 12}
			provider, err := control.CreateProvider(ctx, actor, ProviderInput{Name: "diagnostic", ScopeType: models.ScopeTenant, AdapterType: AdapterOpenAICompatible, Endpoint: upstream.URL})
			if err != nil {
				t.Fatal(err)
			}
			deployment, err := control.CreateDeployment(ctx, actor, DeploymentInput{ProviderConnectionID: provider.ID, Name: "diagnostic", UpstreamModel: "model-a", Operations: []string{"chat"}, Modalities: []string{"text"}})
			if err != nil {
				t.Fatal(err)
			}
			profile, err := control.CreateProfile(ctx, actor, ProfileInput{Name: "diagnostic", Code: "diagnostic", ScopeType: models.ScopeTenant, ModelDeploymentID: deployment.ID})
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			logger.Init(logger.Options{Writer: &output})
			t.Cleanup(func() { logger.Init(logger.Options{}) })
			runtime := NewRuntime(control.store, testEncryptionKey)
			_, err = runtime.Chat(ctx, commoninference.ChatRequest{SchemaVersion: commoninference.SchemaVersion, TenantID: 7, ModelProfileID: profile.ID, Messages: []commoninference.Message{{Role: "user", Content: "sensitive-content"}}})
			if !errors.Is(err, ErrUpstreamFailed) {
				t.Fatalf("error=%v, want upstream failed", err)
			}
			if strings.Contains(output.String(), "sensitive-content") {
				t.Fatal("chat validation diagnostics exposed content")
			}
			var event map[string]interface{}
			if err := json.Unmarshal(output.Bytes(), &event); err != nil || event["stage"] != tc.stage {
				t.Fatalf("missing chat validation stage: %v", err)
			}
		})
	}
}
