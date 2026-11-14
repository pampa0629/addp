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

type healthTestTokenSource struct{}

func (healthTestTokenSource) Token(context.Context, uint) (string, error)   { return "token", nil }
func (healthTestTokenSource) PlatformToken(context.Context) (string, error) { return "token", nil }

func TestBusinessHealthAndReadyGateUseLocalChecksBeforeRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	localCheck := StaticCheck("local_dependencies", true, "")
	controller := NewBusiness("meta", commonclient.ModuleRuntimeRoleBackend, func(ctx context.Context) CheckResult {
		return localCheck(ctx)
	})
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
	if ready.Code != http.StatusOK {
		t.Fatalf("ready before registration = %d %s", ready.Code, ready.Body.String())
	}
	var body ReadyResponse
	if err := json.Unmarshal(ready.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.InstanceID == "" || body.InstanceID != liveBody["instance_id"] {
		t.Fatalf("ready instance ID = %q, live = %v", body.InstanceID, liveBody["instance_id"])
	}
	if body.RegistrationState != commonclient.ModuleRegistrationStarting || body.Status != "ready" || len(body.Checks) != 1 || body.Checks[0].Name != "local_dependencies" {
		t.Fatalf("ready body = %#v", body)
	}

	business := httptest.NewRecorder()
	router.ServeHTTP(business, httptest.NewRequest(http.MethodGet, "/api/v1/meta/items", nil))
	if business.Code != http.StatusNoContent {
		t.Fatalf("business before registration = %d", business.Code)
	}
	localCheck = StaticCheck("local_dependencies", false, "local_dependencies_unavailable")
	ready = httptest.NewRecorder()
	router.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready with failed local check = %d", ready.Code)
	}
	business = httptest.NewRecorder()
	router.ServeHTTP(business, httptest.NewRequest(http.MethodGet, "/api/v1/meta/items", nil))
	if business.Code != http.StatusServiceUnavailable || business.Body.String() != `{"error":"module is not ready","error_code":"module_not_ready"}` {
		t.Fatalf("business response = %d %s", business.Code, business.Body.String())
	}
}

func TestBusinessReadinessReportsRegistrationLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, request)
		}
	}))
	defer system.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := commonclient.NewSystemServiceClient(system.URL, healthTestTokenSource{}, system.Client())
	registration := client.RegisterAndHeartbeat(ctx, &commonclient.ModuleRegistrationRequest{
		ModuleName: "meta", ModuleURL: "http://meta:8082",
	})
	controller := NewBusiness("meta", commonclient.ModuleRuntimeRoleBackend)
	controller.AttachRegistration(registration)
	waitContext, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := registration.WaitUntilRegistered(waitContext); err != nil {
		t.Fatalf("wait for registration: %v", err)
	}

	response, ready := controller.Readiness(context.Background())
	if !ready || response.Status != "ready" || response.RegistrationState != commonclient.ModuleRegistrationRegistered {
		t.Fatalf("readiness = %#v ready=%t", response, ready)
	}
	cancel()
	select {
	case <-registration.Done():
	case <-time.After(time.Second):
		t.Fatal("registration lifecycle did not stop")
	}
	response, ready = controller.Readiness(context.Background())
	if !ready || response.RegistrationState != commonclient.ModuleRegistrationStopped {
		t.Fatalf("stopped registration changed HTTP readiness: %#v", response)
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
