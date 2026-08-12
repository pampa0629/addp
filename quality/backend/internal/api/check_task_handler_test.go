package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateCheckTaskRejectsRemovedEnabledField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/check-tasks", NewCheckTaskHandler(nil, nil).Create)

	req := httptest.NewRequest(http.MethodPost, "/check-tasks", strings.NewReader(`{
		"name":"quality check",
		"engine_id":1,
		"schema_name":"public",
		"table_name":"orders",
		"enabled":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown field") {
		t.Fatalf("body = %s, want unknown field error", w.Body.String())
	}
}
