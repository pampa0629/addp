package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestSystemObservabilityClientUsesBoundedPlatformProjection(t *testing.T) {
	fixture := models.ObservabilityIdentitySnapshot{
		ObservedAt:      time.Now().UTC(),
		Nodes:           []models.ObservabilityNodeIdentity{{NodeID: "10000000-0000-4000-8000-000000000001", Version: 2}},
		ModuleInstances: []models.ObservabilityModuleIdentity{{ModuleName: "manager", InstanceID: "worker-a", Role: "worker", NodeID: "10000000-0000-4000-8000-000000000001", LeaseExpiresAt: time.Now().Add(time.Minute).UTC()}},
	}
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	body := encode(fixture)
	status, requests := 200, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" || r.URL.Path != "/api/v1/system/runtime/observability-identities" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer platform-token" {
			t.Fatalf("request=%s %s headers=%v", r.Method, r.URL, r.Header)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("platform-token"), server.Client())
	snapshot, err := client.GetObservabilityIdentities(context.Background())
	if err != nil || len(snapshot.Nodes) != 1 || len(snapshot.ModuleInstances) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot, err := client.WithTenantID(7).GetObservabilityIdentities(context.Background()); err == nil || snapshot != nil || requests != 1 {
		t.Fatalf("tenant request=%+v %v requests=%d", snapshot, err, requests)
	}
	for _, invalid := range []string{"{}", `{"nodes":[],"module_instances":[]}`, strings.Replace(body, `"version":2`, `"version":0`, 1), strings.Replace(body, `"worker"`, `"unknown"`, 1), strings.Replace(body, `"worker-a"`, `""`, 1), body + "{}", body + strings.Repeat(" ", int(models.ObservabilityIdentityResponseLimit))} {
		body = invalid
		if snapshot, err := client.GetObservabilityIdentities(context.Background()); err == nil || snapshot != nil {
			t.Fatal("accepted malformed/oversized response")
		}
	}
	for _, code := range []int{401, 403, 503, 504} {
		status, body = code, `{"error_code":"observability_identity_unavailable","error":"safe error"}`
		if snapshot, err := client.GetObservabilityIdentities(context.Background()); snapshot != nil {
			t.Fatal("returned snapshot with error")
		} else if got, ok := SystemAPIStatusCode(err); !ok || got != code {
			t.Fatalf("status=%d error=%v", got, err)
		}
	}
}

func TestSystemObservabilityClientRejectsDuplicateForeignExpiredAndStaleIdentities(t *testing.T) {
	now := time.Now().UTC()
	node := models.ObservabilityNodeIdentity{NodeID: "10000000-0000-4000-8000-000000000001", Version: 1}
	instance := models.ObservabilityModuleIdentity{ModuleName: "manager", InstanceID: "worker-a", Role: "worker", NodeID: node.NodeID, LeaseExpiresAt: now.Add(time.Minute)}
	base := func() models.ObservabilityIdentitySnapshot {
		return models.ObservabilityIdentitySnapshot{ObservedAt: now, Nodes: []models.ObservabilityNodeIdentity{node}, ModuleInstances: []models.ObservabilityModuleIdentity{instance}}
	}
	for _, mutate := range []func(*models.ObservabilityIdentitySnapshot){
		func(s *models.ObservabilityIdentitySnapshot) { s.Nodes = append(s.Nodes, node) },
		func(s *models.ObservabilityIdentitySnapshot) { s.ModuleInstances = append(s.ModuleInstances, instance) },
		func(s *models.ObservabilityIdentitySnapshot) { s.ModuleInstances[0].NodeID = "foreign" },
		func(s *models.ObservabilityIdentitySnapshot) { s.ModuleInstances[0].LeaseExpiresAt = now },
		func(s *models.ObservabilityIdentitySnapshot) { s.ObservedAt = now.Add(-time.Minute) },
		func(s *models.ObservabilityIdentitySnapshot) { s.ObservedAt = now.Add(time.Minute) },
		func(s *models.ObservabilityIdentitySnapshot) {
			s.Nodes[0].NodeID = "00000000-0000-0000-0000-000000000000"
		},
		func(s *models.ObservabilityIdentitySnapshot) {
			s.Nodes = make([]models.ObservabilityNodeIdentity, models.ObservabilityIdentityNodeLimit+1)
		},
		func(s *models.ObservabilityIdentitySnapshot) {
			s.ModuleInstances = make([]models.ObservabilityModuleIdentity, models.ObservabilityIdentityInstanceLimit+1)
		},
	} {
		snapshot := base()
		mutate(&snapshot)
		if err := validateObservabilityIdentities(snapshot, now, now); err == nil {
			t.Fatalf("accepted invalid snapshot=%+v", snapshot)
		}
	}
}
