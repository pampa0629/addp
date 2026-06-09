package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/models"
)

func TestCheckAllProviderHealthChecksModuleAndTaskDiscovery(t *testing.T) {
	var gotInternalKey string
	var gotTenantID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/v1/meta/tasks":
			gotInternalKey = r.Header.Get("X-Internal-API-Key")
			gotTenantID = r.Header.Get("X-Tenant-ID")
			if r.URL.Query().Get("task_type") != "scan" {
				t.Fatalf("task_type query = %q, want scan", r.URL.Query().Get("task_type"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	caps := models.JSONString(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[
			{"type":"scan","deprecated":false},
			{"type":"legacy_scan","deprecated":true}
		]
	}`)
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, "internal-key")

	statuses, err := service.CheckAllProviderHealth(context.Background(), 7)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses len = %d, want 1", len(statuses))
	}
	status := statuses[0]
	if status.Status != "up" {
		t.Fatalf("provider status = %q, want up; message=%s", status.Status, status.Message)
	}
	if status.ModuleHealth == nil || status.ModuleHealth.Status != "up" {
		t.Fatalf("module health = %#v, want up", status.ModuleHealth)
	}
	if len(status.TaskDiscovery) != 1 {
		t.Fatalf("task discovery len = %d, want only non-deprecated task type", len(status.TaskDiscovery))
	}
	if status.TaskDiscovery[0].TaskType != "scan" || status.TaskDiscovery[0].Status != "up" {
		t.Fatalf("task discovery = %#v, want scan/up", status.TaskDiscovery[0])
	}
	if gotInternalKey != "internal-key" {
		t.Fatalf("X-Internal-API-Key = %q, want internal-key", gotInternalKey)
	}
	if gotTenantID != "7" {
		t.Fatalf("X-Tenant-ID = %q, want 7", gotTenantID)
	}
}

func TestCheckAllProviderHealthReportsInvalidCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected task discovery call for invalid capabilities: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	caps := models.JSONString(`{"schema_version":"legacy","task_types":[{"type":"scan"}]}`)
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, "")

	statuses, err := service.CheckAllProviderHealth(context.Background(), 0)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "degraded" {
		t.Fatalf("provider status = %q, want degraded", statuses[0].Status)
	}
	if statuses[0].Capabilities == nil || statuses[0].Capabilities.Status != "down" {
		t.Fatalf("capabilities = %#v, want down", statuses[0].Capabilities)
	}
	if len(statuses[0].TaskDiscovery) != 0 {
		t.Fatalf("task discovery len = %d, want 0", len(statuses[0].TaskDiscovery))
	}
}

type fakeTaskProviderLister struct {
	providers []*models.TaskProvider
	err       error
}

func (f fakeTaskProviderLister) ListTaskProviders() ([]*models.TaskProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.providers, nil
}
