package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func TestNotebookFramePolicyPreservesDirectivesAndReplacesFrameAncestors(t *testing.T) {
	policy := notebookFramePolicy("default-src 'self'; frame-ancestors 'none'; report-uri /api/security/csp-report")
	if !strings.Contains(policy, "default-src 'self'") || !strings.Contains(policy, "report-uri /api/security/csp-report") {
		t.Fatalf("unrelated CSP directives were lost: %q", policy)
	}
	if strings.Contains(policy, "frame-ancestors 'none'") || !strings.Contains(policy, "frame-ancestors 'self' http://localhost:*") {
		t.Fatalf("frame ancestor policy was not replaced: %q", policy)
	}
}

func TestListSessionEngineDescriptorsRequiresKernelCapability(t *testing.T) {
	called := false
	handler := &NotebookHandler{listSessionEngineDescriptors: func(context.Context, string, string) ([]commonModels.EngineRuntimeDescriptor, error) {
		called = true
		return nil, nil
	}}
	response := requestSessionEngineDescriptors(t, handler, "")
	if response.Code != http.StatusUnauthorized || called {
		t.Fatalf("status = %d, called = %t, body = %s", response.Code, called, response.Body.String())
	}
}

func TestListSessionEngineDescriptorsReturnsMaskedQueryDescriptors(t *testing.T) {
	handler := &NotebookHandler{listSessionEngineDescriptors: func(_ context.Context, sessionID, token string) ([]commonModels.EngineRuntimeDescriptor, error) {
		if sessionID != "session-1" || token != "addp_nkc_kernel-secret" {
			t.Fatalf("sessionID = %q, token = %q", sessionID, token)
		}
		return []commonModels.EngineRuntimeDescriptor{{
			ID: 21, Name: "PostgreSQL", EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive,
		}}, nil
	}}
	response := requestSessionEngineDescriptors(t, handler, "Bearer addp_nkc_kernel-secret")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "connection_info") {
		t.Fatalf("headers = %#v, body = %s", response.Header(), response.Body.String())
	}
	var descriptors []commonModels.EngineRuntimeDescriptor
	if err := json.Unmarshal(response.Body.Bytes(), &descriptors); err != nil || len(descriptors) != 1 || descriptors[0].ID != 21 {
		t.Fatalf("descriptors = %#v, error = %v", descriptors, err)
	}
}

func TestListSessionEngineDescriptorsRejectsExpiredCapability(t *testing.T) {
	handler := &NotebookHandler{listSessionEngineDescriptors: func(context.Context, string, string) ([]commonModels.EngineRuntimeDescriptor, error) {
		return nil, service.ErrNotebookSessionNotFound
	}}
	response := requestSessionEngineDescriptors(t, handler, "Bearer addp_nkc_expired")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func requestSessionEngineDescriptors(t *testing.T, handler *NotebookHandler, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/notebook-kernel-sessions/:session_id/engine-descriptors", handler.ListSessionEngineDescriptors)
	request := httptest.NewRequest(http.MethodGet, "/notebook-kernel-sessions/session-1/engine-descriptors", nil)
	request.Header.Set("Authorization", authorization)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
