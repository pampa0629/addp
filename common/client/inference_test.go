package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	commoninference "github.com/addp/common/inference"
)

func TestInferenceClientRefreshesRejectedServiceTokenOnce(t *testing.T) {
	var tokenRequests atomic.Int32
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
	if response.ProfileVersion != 2 || tokenRequests.Load() != 2 || inferenceRequests.Load() != 2 {
		t.Fatalf("unexpected refresh result: response=%+v token_requests=%d inference_requests=%d", response, tokenRequests.Load(), inferenceRequests.Load())
	}
}
