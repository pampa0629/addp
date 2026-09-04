package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMaterializedTargetDecommissionRejectsArbitrarySQLField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.DELETE("/logical-tables/:id/materialized-target", NewMaterializedTargetHandler(nil).Decommission)
	request := httptest.NewRequest(http.MethodDelete, "/logical-tables/7/materialized-target", strings.NewReader(`{
		"version": 1,
		"target_parent_locator": "addp://engine/2/path/outdoor?type=schema",
		"target_name": "metric",
		"sql": "DROP SCHEMA outdoor CASCADE"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer addp_at_test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"error_code":"invalid_request"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBearerCredentialRequiresOneBearerToken(t *testing.T) {
	if got := bearerCredential("Bearer addp_at_token"); got != "addp_at_token" {
		t.Fatalf("credential = %q", got)
	}
	for _, value := range []string{"", "Basic value", "Bearer", "Bearer one two"} {
		if got := bearerCredential(value); got != "" {
			t.Fatalf("header %q produced %q", value, got)
		}
	}
}
