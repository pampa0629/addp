package api

import (
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

	var upstreamTenantID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/internal/engines" {
			http.NotFound(w, request)
			return
		}
		upstreamTenantID = request.URL.Query().Get("tenant_id")
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

	handler := NewSystemEngineHandler(commonClient.NewSystemClientWithInternalKey(upstream.URL, "test-internal-key"))
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
	if upstreamTenantID != "7" {
		t.Fatalf("upstream tenant_id = %q, want 7", upstreamTenantID)
	}
	var engines []commonModels.Engine
	if err := json.Unmarshal(response.Body.Bytes(), &engines); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(engines) != 1 || engines[0].ID != 41 {
		t.Fatalf("engines = %#v, want tenant engine 41", engines)
	}
}
