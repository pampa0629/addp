package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authtest"
	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
)

func TestSystemEngineHandlerUsesCanonicalTenantContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamAuthorization string
	var upstreamLegacyHeaders bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/system/engines" {
			http.NotFound(w, request)
			return
		}
		upstreamAuthorization = request.Header.Get("Authorization")
		upstreamLegacyHeaders = request.Header.Get("X-Internal-API-Key") != "" || request.Header.Get("X-Tenant-ID") != ""
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"id": 41,
			"tenant_id": 7,
			"name": "Tenant PostgreSQL",
			"engine_type": "postgresql",
			"engine_origin": "general",
			"connection_info": {},
			"lifecycle_state": "active"
		}]`))
	}))
	defer upstream.Close()

	authContextServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer transfer-user": {"transfer.task.read"},
	})
	defer authContextServer.Close()

	handler := NewSystemEngineHandler(commonClient.NewSystemClient(upstream.URL, commonClient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 7 {
			t.Fatalf("token tenant ID = %d, want 7", tenantID)
		}
		return "addp_at_transfer_service_token", nil
	})))
	router := gin.New()
	router.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: authContextServer.URL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	router.GET("/system-engines", handler.List)

	request := httptest.NewRequest(http.MethodGet, "/system-engines", nil)
	request.Header.Set("Authorization", "Bearer transfer-user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if upstreamAuthorization != "Bearer addp_at_transfer_service_token" || upstreamLegacyHeaders {
		t.Fatalf("upstream authorization = %q legacy_headers=%v", upstreamAuthorization, upstreamLegacyHeaders)
	}
	var engines []commonModels.Engine
	if err := json.Unmarshal(response.Body.Bytes(), &engines); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(engines) != 1 || engines[0].ID != 41 {
		t.Fatalf("engines = %#v, want tenant engine 41", engines)
	}
}
