package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

func TestRasterPolicyHealthUsesWorkflowReadinessContract(t *testing.T) {
	body := `{"status":"healthy","service":"geo","version":"1","raster_resources":{"running":3}}`
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer runtime.Close()
	u, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	engine := &models.Engine{Name: "Geo", EngineType: "geopython_workflow", IsBuiltin: true,
		LifecycleState: models.EngineLifecycleActive,
		ConnectionInfo: models.ConnectionInfo{"protocol": u.Scheme, "host": u.Hostname(), "port": port}}
	if err := db.Create(engine).Error; err != nil {
		t.Fatal(err)
	}
	service := NewEngineRasterPolicyService(repository.NewEngineRasterPolicyRepository(db))
	health := service.RuntimeHealth(context.Background(), engine.ID)
	if health["running"] != float64(3) {
		t.Fatalf("healthy raster facts missing: %#v", health)
	}
	body = `{"status":"healthy","service":"geo","version":"1","dependencies":{"sdk":{"available":false,"required":true}},"raster_resources":{"running":3}}`
	if health := service.RuntimeHealth(context.Background(), engine.ID); health != nil {
		t.Fatalf("unready resource facts accepted: %#v", health)
	}
}
