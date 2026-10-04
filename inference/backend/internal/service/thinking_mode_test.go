package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	commoninference "github.com/addp/common/inference"
	"github.com/addp/inference/internal/models"
)

func TestDeploymentThinkingModeValidationAndPersistence(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	control := NewControlPlane(store, testEncryptionKey)
	actor := Actor{ContextType: models.ScopePlatform, PrincipalID: 61}
	provider, err := control.CreateProvider(ctx, actor, ProviderInput{
		Name: "thinking-control", ScopeType: models.ScopePlatform, AdapterType: AdapterOpenAICompatible,
		Endpoint: "https://example.test", AllowAllTenants: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := DeploymentInput{ProviderConnectionID: provider.ID, Name: "chat", UpstreamModel: "arbitrary-model", Operations: []string{"chat"}, Modalities: []string{"text"}}
	deployment, err := control.CreateDeployment(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetDeployment(ctx, deployment.ID)
	if err != nil || stored.ChatThinkingMode != ChatThinkingModeUpstreamDefault {
		t.Fatalf("default stored mode: %+v, %v", stored, err)
	}
	for _, mode := range []string{ChatThinkingModeDisabled, ChatThinkingModeUpstreamDefault} {
		input.ChatThinkingMode = mode
		updated, err := control.UpdateDeployment(ctx, actor, deployment.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		stored, err = store.GetDeployment(ctx, deployment.ID)
		if err != nil || stored.ChatThinkingMode != mode || updated.ChatThinkingMode != mode {
			t.Fatalf("updated mode = %+v, err=%v", stored, err)
		}
	}
	for _, mode := range []string{"enabled", "auto", "unknown"} {
		input.ChatThinkingMode = mode
		if _, err := control.CreateDeployment(ctx, actor, input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("create mode %q: %v", mode, err)
		}
		if _, err := control.UpdateDeployment(ctx, actor, deployment.ID, input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("update mode %q: %v", mode, err)
		}
	}
	stored, err = store.GetDeployment(ctx, deployment.ID)
	if err != nil || stored.ChatThinkingMode != ChatThinkingModeUpstreamDefault {
		t.Fatalf("rejected update changed stored mode: %+v, %v", stored, err)
	}
	provider.AdapterType = AdapterDashScopeMultimodal
	if err := store.DB().Save(&provider.ProviderConnection).Error; err != nil {
		t.Fatal(err)
	}
	input.ChatThinkingMode = ChatThinkingModeDisabled
	if _, err := control.CreateDeployment(ctx, actor, input); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unsupported adapter accepted thinking control: %v", err)
	}
}

func TestRuntimeThinkingModeKeepsRequiredToolChoice(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		status     int
	}{
		{"default", ChatThinkingModeUpstreamDefault, http.StatusOK},
		{"disabled", ChatThinkingModeDisabled, http.StatusOK},
		{"disabled upstream rejection", ChatThinkingModeDisabled, http.StatusBadRequest},
		{"invalid stored value", "enabled", http.StatusOK},
		{"empty stored value", "", http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			mode := test.mode
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/chat/completions" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if body["tool_choice"] != "required" {
					t.Errorf("tool_choice changed: %v", body["tool_choice"])
				}
				if mode == ChatThinkingModeDisabled {
					thinking, ok := body["thinking"].(map[string]interface{})
					if !ok || len(thinking) != 1 || thinking["type"] != "disabled" {
						t.Errorf("thinking = %#v", body["thinking"])
					}
				} else if _, exists := body["thinking"]; exists {
					t.Error("default mode must omit thinking")
				}
				w.Header().Set("Content-Type", "application/json")
				if test.status != http.StatusOK {
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"route","arguments":"{}"}}]}}]}`))
			}))
			defer upstream.Close()
			ctx := context.Background()
			store := newTestStore(t)
			control := NewControlPlane(store, testEncryptionKey)
			actor := Actor{ContextType: models.ScopePlatform, PrincipalID: 61}
			provider, err := control.CreateProvider(ctx, actor, ProviderInput{Name: "any-provider", ScopeType: models.ScopePlatform, AdapterType: AdapterOpenAICompatible, Endpoint: upstream.URL, AllowAllTenants: true})
			if err != nil {
				t.Fatal(err)
			}
			deployment, err := control.CreateDeployment(ctx, actor, DeploymentInput{ProviderConnectionID: provider.ID, Name: "chat", UpstreamModel: "unrelated-name", Operations: []string{"chat"}, Modalities: []string{"text"}})
			if err != nil {
				t.Fatal(err)
			}
			// Test corrupt stored settings too; Runtime must not silently recover them.
			if err := store.DB().Model(deployment).Update("chat_thinking_mode", mode).Error; err != nil {
				t.Fatal(err)
			}
			profile, err := control.CreateProfile(ctx, actor, ProfileInput{Name: "chat", Code: "thinking-test", ScopeType: models.ScopePlatform, ModelDeploymentID: deployment.ID})
			if err != nil {
				t.Fatal(err)
			}
			response, err := NewRuntime(store, testEncryptionKey).Chat(ctx, commoninference.ChatRequest{
				SchemaVersion: commoninference.SchemaVersion, TenantID: 8, ModelProfileID: profile.ID,
				Messages: []commoninference.Message{{Role: "user", Content: "route"}},
				Tools:    []commoninference.ToolDefinition{{Name: "route", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}, ToolChoice: "required",
			})
			if mode != ChatThinkingModeUpstreamDefault && mode != ChatThinkingModeDisabled {
				if !errors.Is(err, ErrProfileUnavailable) || calls.Load() != 0 {
					t.Fatalf("invalid mode: err=%v, calls=%d", err, calls.Load())
				}
				return
			}
			if test.status != http.StatusOK {
				if !errors.Is(err, ErrUpstreamFailed) || calls.Load() != 1 {
					t.Fatalf("upstream rejection changed request or retried: err=%v, calls=%d", err, calls.Load())
				}
				return
			}
			if err != nil || response == nil || len(response.Message.ToolCalls) != 1 || calls.Load() != 1 {
				t.Fatalf("tool response=%+v, err=%v, calls=%d", response, err, calls.Load())
			}
		})
	}
}
