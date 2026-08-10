package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	commoninference "github.com/addp/common/inference"
	commonmodels "github.com/addp/common/models"
)

func TestInferenceClientRefreshesRejectedServiceTokenOnce(t *testing.T) {
	var tokenRequests atomic.Int32
	var descriptorRequests atomic.Int32
	var inferenceRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			requestNumber := tokenRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_token-" + string(rune('0'+requestNumber)),
				"token_type":   "Bearer",
				"expires_in":   120,
				"scope":        "addp.api",
			})
		case "/api/v1/system/runtime/engine-descriptors":
			descriptorRequests.Add(1)
			host, portText, err := net.SplitHostPort(r.Host)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				t.Fatal(err)
			}
			capabilities := commonmodels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"inference_runtime","engine_family":"inference","compute":{"inference":{"supported":true,"runtime_api":"addp.inference/v1","operations":["embedding"]}}}`)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []commonmodels.EngineRuntimeDescriptor{{
					ID: 9, EngineType: "inference_runtime", IsBuiltin: true,
					LifecycleState: commonmodels.EngineLifecycleActive, Capabilities: &capabilities,
					RuntimeEndpoint: &commonmodels.EngineRuntimeEndpoint{Protocol: "http", Host: host, Port: port},
				}},
				"total": 1, "page": 1, "page_size": 100,
			})
		case "/api/v1/inference/internal/profiles/resolve":
			inferenceRequests.Add(1)
			if r.Header.Get("Authorization") == "Bearer addp_at_token-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(commoninference.ResolveProfileResponse{
				SchemaVersion:  commoninference.SchemaVersion,
				ModelProfileID: "profile-1",
				ProfileVersion: 2,
				DeploymentID:   "deployment-1",
				Dimension:      2560,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenSource, err := NewOAuthServiceTokenSource(server.URL, "addp-manager", "test-service-client-secret-32bytes", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewInferenceClient(server.URL, tokenSource, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ResolveProfile(context.Background(), commoninference.ResolveProfileRequest{
		SchemaVersion:  commoninference.SchemaVersion,
		TenantID:       7,
		ModelProfileID: "profile-1",
		Operation:      commoninference.OperationEmbedding,
		Modality:       commoninference.ModalityImage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.ProfileVersion != 2 || tokenRequests.Load() != 2 || descriptorRequests.Load() != 1 || inferenceRequests.Load() != 2 {
		t.Fatalf("unexpected refresh result: response=%+v token_requests=%d descriptor_requests=%d inference_requests=%d", response, tokenRequests.Load(), descriptorRequests.Load(), inferenceRequests.Load())
	}
}

func TestInferenceClientRequiresExactlyOneRuntimeDescriptor(t *testing.T) {
	capabilities := commonmodels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"inference_runtime","engine_family":"inference","compute":{"inference":{"supported":true,"runtime_api":"addp.inference/v1","operations":["embedding"]}}}`)
	for _, testCase := range []struct {
		name        string
		descriptors []commonmodels.EngineRuntimeDescriptor
		wantError   error
	}{
		{name: "missing", wantError: ErrInferenceRuntimeNotFound},
		{name: "ambiguous", descriptors: []commonmodels.EngineRuntimeDescriptor{
			{ID: 1, EngineType: "inference_runtime", IsBuiltin: true, LifecycleState: commonmodels.EngineLifecycleActive, Capabilities: &capabilities, RuntimeEndpoint: &commonmodels.EngineRuntimeEndpoint{Protocol: "http", Host: "runtime-1", Port: 8191}},
			{ID: 2, EngineType: "inference_runtime", IsBuiltin: true, LifecycleState: commonmodels.EngineLifecycleActive, Capabilities: &capabilities, RuntimeEndpoint: &commonmodels.EngineRuntimeEndpoint{Protocol: "http", Host: "runtime-2", Port: 8191}},
		}, wantError: ErrInferenceRuntimeAmbiguous},
		{name: "wrong runtime api", descriptors: []commonmodels.EngineRuntimeDescriptor{{
			ID: 3, EngineType: "inference_runtime", IsBuiltin: true, LifecycleState: commonmodels.EngineLifecycleActive,
			Capabilities: func() *commonmodels.JSONString {
				value := commonmodels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"inference_runtime","engine_family":"inference","compute":{"inference":{"supported":true,"runtime_api":"wrong/v1","operations":["embedding"]}}}`)
				return &value
			}(),
			RuntimeEndpoint: &commonmodels.EngineRuntimeEndpoint{Protocol: "http", Host: "runtime-3", Port: 8191},
		}}, wantError: ErrInferenceRuntimeNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/system/oauth/token":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"access_token": "addp_at_token", "token_type": "Bearer", "expires_in": 120, "scope": "addp.api",
					})
				case "/api/v1/system/runtime/engine-descriptors":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"data": testCase.descriptors, "total": len(testCase.descriptors), "page": 1, "page_size": 100,
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			tokenSource, err := NewOAuthServiceTokenSource(server.URL, "addp-manager", "test-service-client-secret-32bytes", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewInferenceClient(server.URL, tokenSource, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ResolveProfile(context.Background(), commoninference.ResolveProfileRequest{
				SchemaVersion: commoninference.SchemaVersion, TenantID: 7, ModelProfileID: "profile-1",
				Operation: commoninference.OperationEmbedding, Modality: commoninference.ModalityImage,
			})
			if !errors.Is(err, testCase.wantError) {
				t.Fatalf("ResolveProfile() error = %v, want %v", err, testCase.wantError)
			}
		})
	}
}
