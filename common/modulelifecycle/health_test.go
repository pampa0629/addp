package modulelifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/gin-gonic/gin"
)

func TestBusinessHealthAndReadyGateFollowRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller := NewBusiness("meta", commonclient.ModuleRuntimeRoleBackend)
	router := gin.New()
	controller.RegisterHealthRoutes(router)
	router.Use(controller.RequireReady())
	router.GET("/api/v1/meta/items", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	live := httptest.NewRecorder()
	router.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("live status = %d", live.Code)
	}
	var liveBody map[string]any
	if err := json.Unmarshal(live.Body.Bytes(), &liveBody); err != nil {
		t.Fatal(err)
	}
	if liveBody["status"] != "live" || liveBody["module"] != "meta" {
		t.Fatalf("live body = %#v", liveBody)
	}

	ready := httptest.NewRecorder()
	router.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status before registration = %d", ready.Code)
	}

	business := httptest.NewRecorder()
	router.ServeHTTP(business, httptest.NewRequest(http.MethodGet, "/api/v1/meta/items", nil))
	if business.Code != http.StatusServiceUnavailable || business.Body.String() != "{\"error\":\"module is not ready\",\"error_code\":\"module_not_ready\"}" {
		t.Fatalf("business response = %d %s", business.Code, business.Body.String())
	}
}

func TestStandaloneReadinessUsesOnlyLocalChecks(t *testing.T) {
	controller := NewStandalone("gateway", StaticCheck("system_registry_snapshot", true, ""))
	response, ready := controller.Readiness(context.Background())
	if !ready || response.Status != "ready" || response.Role != "" || response.RegistrationState != "" {
		t.Fatalf("readiness = %#v, ready=%t", response, ready)
	}
}

func TestLiveDoesNotRunReadinessChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := make(chan struct{}, 1)
	controller := NewStandalone("system", func(context.Context) CheckResult {
		called <- struct{}{}
		return CheckResult{Name: "local_dependencies", Status: CheckReady}
	})
	router := gin.New()
	controller.RegisterHealthRoutes(router)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	select {
	case <-called:
		t.Fatal("live probe executed a readiness check")
	case <-time.After(20 * time.Millisecond):
	}
}
