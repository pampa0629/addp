package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNotebookEngineCatalogUnknownErrorKeepsFailureBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		respond  func(*gin.Context, error)
		expected string
	}{
		{name: "control plane", respond: respondNotebookEngineCatalogError, expected: "engine_catalog_control_plane_failed"},
		{name: "provider", respond: respondNotebookEngineCatalogProviderError, expected: "engine_catalog_provider_failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			test.respond(context, errors.New("unclassified failure"))

			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
			}
			var response IAMErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.ErrorCode == nil || *response.ErrorCode != test.expected {
				t.Fatalf("error_code = %v, want %q", response.ErrorCode, test.expected)
			}
		})
	}
}
