package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

func TestProtectionEndpointsRejectSelectorsAndUntrustedFieldsBeforeDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewTaskHandler(&service.TaskService{})
	router.GET("/options", handler.SourceProtectionOptions)
	router.POST("/checks", handler.CheckSourceProtection)
	for _, query := range []string{"", "?source_locator=x&source_locator=y", "?source_locator=x&user_id=9", "?source_locator=x&role_keys=admin"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/options"+query, nil))
		if response.Code != 400 {
			t.Fatalf("invalid selectors reached service: %s -> %d", query, response.Code)
		}
	}
	for _, body := range []string{`{}`, `{"source_locator":"source","protection_choices":[],"role_keys":["admin"]}`, `{"source_locator":"source","protection_choices":[],"user_id":9}`, `{"source_locator":"source","protection_choices":null}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/checks", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("untrusted role assertion reached service: %d", response.Code)
		}
	}
}
