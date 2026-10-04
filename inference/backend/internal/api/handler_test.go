package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/inference/internal/service"
	"github.com/gin-gonic/gin"
)

func TestProviderInputRejectsCredentialField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/provider-connections", strings.NewReader(`{
		"name":"provider",
		"scope_type":"platform",
		"adapter_type":"openai_compatible",
		"endpoint":"https://example.test/v1",
		"credential":"must-use-dedicated-operation"
	}`))
	context.Request.Header.Set("Content-Type", "application/json")

	var input service.ProviderInput
	if bind(context, &input) {
		t.Fatal("ordinary provider input must reject credential fields")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestDeploymentInputAcceptsExplicitThinkingControlButNotArbitraryExtras(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, body string
		want       bool
	}{
		{"thinking control", `{"provider_connection_id":"provider","name":"chat","upstream_model":"model","operations":["chat"],"modalities":["text"],"chat_thinking_mode":"disabled"}`, true},
		{"arbitrary extras", `{"provider_connection_id":"provider","name":"chat","upstream_model":"model","operations":["chat"],"modalities":["text"],"chat_thinking_mode":"disabled","extra_body":{"thinking":{"type":"enabled"}}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/model-deployments", strings.NewReader(test.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			var input service.DeploymentInput
			if got := bind(ctx, &input); got != test.want {
				t.Fatalf("bind=%v, want %v", got, test.want)
			}
			if test.want && input.ChatThinkingMode != service.ChatThinkingModeDisabled {
				t.Fatalf("thinking mode = %q", input.ChatThinkingMode)
			}
			if !test.want && response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
}
