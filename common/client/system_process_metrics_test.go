package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/config"
	"github.com/addp/common/models"
)

func TestOptionalProcessSourceFailureDoesNotBlockRegistration(t *testing.T) {
	t.Setenv(config.ProcessMetricsDeploymentsEnv, `[{"secret":"must-not-be-logged"}]`)
	registered := make(chan ModuleRegistrationRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/runtime/modules" {
			var request ModuleRegistrationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			registered <- request
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	lifecycle := client.RegisterAndHeartbeat(ctx, &ModuleRegistrationRequest{ModuleName: "monitor", Role: "worker", RoutePrefix: "/monitor", ProcessMetrics: &models.ProcessMetricsDeclaration{SchemaVersion: models.ProcessMetricsSchema, Endpoint: "https://other:18100/metrics"}})
	select {
	case request := <-registered:
		if request.ProcessMetrics != nil || request.InstanceID == "" {
			t.Fatal("SDK accepted forged source or lost identity")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("source failure blocked business registration")
	}
	if err := lifecycle.WaitUntilRegistered(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-lifecycle.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle cleanup blocked")
	}
}

func TestUnboundCurrentProcessIdentityIsValidOnlyWithExplicitDeclaration(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	i := models.ObservabilityModuleIdentity{ModuleName: "monitor", InstanceID: "self", Role: "worker", ProcessStartedAt: &started, LeaseExpiresAt: now.Add(time.Minute), ProcessMetrics: &models.ProcessMetricsDeclaration{SchemaVersion: models.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:18100/metrics"}}
	s := models.ObservabilityIdentitySnapshot{ObservedAt: now, Nodes: []models.ObservabilityNodeIdentity{}, ModuleInstances: []models.ObservabilityModuleIdentity{i}}
	if err := validateObservabilityIdentities(s, now, now); err != nil {
		t.Fatalf("unbound current process: %v", err)
	}
	for _, mutate := range []func(*models.ObservabilityModuleIdentity){
		func(i *models.ObservabilityModuleIdentity) { i.ProcessMetrics = nil },
		func(i *models.ObservabilityModuleIdentity) { i.ProcessStartedAt = nil },
		func(i *models.ObservabilityModuleIdentity) {
			future := now.Add(time.Hour)
			i.ProcessStartedAt = &future
		},
		func(i *models.ObservabilityModuleIdentity) { i.NodeID = "unknown" },
		func(i *models.ObservabilityModuleIdentity) {
			i.ProcessMetrics = &models.ProcessMetricsDeclaration{SchemaVersion: "wrong", Endpoint: "https://127.0.0.1:18100/metrics"}
		},
	} {
		copy := i
		mutate(&copy)
		s.ModuleInstances = []models.ObservabilityModuleIdentity{copy}
		if err := validateObservabilityIdentities(s, now, now); err == nil {
			t.Fatal("invalid independent process identity accepted")
		}
	}
}
