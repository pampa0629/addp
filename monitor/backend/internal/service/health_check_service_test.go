package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestCheckAllProviderHealthChecksModuleAndTaskDiscovery(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/ready":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/v1/meta/tasks":
			gotAuthorization = r.Header.Get("Authorization")
			if r.URL.Query().Get("task_type") != "scan" {
				t.Fatalf("task_type query = %q, want scan", r.URL.Query().Get("task_type"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"page_size":100}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	caps := models.JSONString(monitorTaskCapabilitiesForTest(
		monitorTaskCapabilityForTest("scan", false),
		monitorTaskCapabilityForTest("legacy_scan", true),
	))
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/tasks", &caps),
	}}, staticServiceTokenProvider("service-token"))

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
	if len(status.Backends) != 1 || status.Backends[0].ModuleHealth == nil || status.Backends[0].ModuleHealth.Status != "up" {
		t.Fatalf("Backend health = %#v, want one up instance", status.Backends)
	}
	if len(status.Backends[0].TaskDiscovery) != 1 {
		t.Fatalf("task discovery len = %d, want only non-deprecated task type", len(status.Backends[0].TaskDiscovery))
	}
	if status.Backends[0].TaskDiscovery[0].TaskType != "scan" || status.Backends[0].TaskDiscovery[0].Status != "up" {
		t.Fatalf("task discovery = %#v, want scan/up", status.Backends[0].TaskDiscovery[0])
	}
	if gotAuthorization != "Bearer service-token" {
		t.Fatalf("Authorization = %q, want Bearer service-token", gotAuthorization)
	}
}

func TestCheckAllProviderHealthChecksEveryBackendAndAggregatesDegraded(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/ready":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/meta/tasks":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"page_size":100}`))
		default:
			t.Fatalf("unexpected healthy Backend path: %s", r.URL.Path)
		}
	}))
	defer healthy.Close()

	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Fatalf("unhealthy Backend should only receive /health/ready, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthy.Close()

	caps := models.JSONString(monitorTaskCapabilitiesForTest(monitorTaskCapabilityForTest("scan", false)))
	provider := newHealthTaskProviderForTest("meta", "Meta", healthy.URL, true, true, "/api/v1/meta/tasks", &caps)
	provider.Backends = append(provider.Backends, models.TaskProviderBackend{
		InstanceID: "backend-2", BaseURL: unhealthy.URL, LeaseExpiresAt: time.Now().Add(time.Hour),
	})
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{provider}}, staticServiceTokenProvider("service-token"))

	statuses, err := service.CheckAllProviderHealth(context.Background(), 7)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if len(statuses) != 1 || statuses[0].Status != "degraded" {
		t.Fatalf("provider statuses = %#v, want one degraded Provider", statuses)
	}
	if len(statuses[0].Backends) != 2 {
		t.Fatalf("Backend count = %d, want 2", len(statuses[0].Backends))
	}
	if statuses[0].Backends[0].Status != "up" || statuses[0].Backends[1].Status != "down" {
		t.Fatalf("Backend statuses = %#v, want up/down", statuses[0].Backends)
	}
}

func TestCheckAllProviderHealthKeepsOfflineDeclarationWithoutProbing(t *testing.T) {
	caps := models.JSONString(monitorTaskCapabilitiesForTest(monitorTaskCapabilityForTest("scan", false)))
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
		newHealthTaskProviderForTest("meta", "Meta", "", true, false, "/api/v1/meta/tasks", &caps),
	}}, nil)

	statuses, err := service.CheckAllProviderHealth(context.Background(), 7)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if len(statuses) != 1 || statuses[0].Available || statuses[0].Status != "down" {
		t.Fatalf("offline status = %#v", statuses)
	}
	if !strings.Contains(statuses[0].Message, "Backend lease") {
		t.Fatalf("provider message = %q, want missing Backend lease", statuses[0].Message)
	}
	if len(statuses[0].Backends) != 0 {
		t.Fatalf("Backends = %#v, want no probes", statuses[0].Backends)
	}
}

func TestCheckAllProviderHealthReportsLegacyTaskDiscoveryShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/ready":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/meta/tasks":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	caps := models.JSONString(monitorTaskCapabilitiesForTest(monitorTaskCapabilityForTest("scan", false)))
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/tasks", &caps),
	}}, staticServiceTokenProvider("service-token"))

	statuses, err := service.CheckAllProviderHealth(context.Background(), 0)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "down" {
		t.Fatalf("provider status = %q, want down", statuses[0].Status)
	}
	checks := statuses[0].Backends[0].TaskDiscovery
	if len(checks) != 1 {
		t.Fatalf("task discovery len = %d, want 1", len(checks))
	}
	if checks[0].Status != "down" {
		t.Fatalf("task discovery status = %q, want down", checks[0].Status)
	}
	if !strings.Contains(checks[0].Message, "data") {
		t.Fatalf("task discovery message = %q, want non-standard data field", checks[0].Message)
	}
}

func TestCheckAllProviderHealthReportsTaskDiscoveryExtraFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/ready":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/meta/tasks":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"page_size":100,"total_pages":0}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	caps := models.JSONString(monitorTaskCapabilitiesForTest(monitorTaskCapabilityForTest("scan", false)))
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/tasks", &caps),
	}}, staticServiceTokenProvider("service-token"))

	statuses, err := service.CheckAllProviderHealth(context.Background(), 0)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "down" {
		t.Fatalf("provider status = %q, want down", statuses[0].Status)
	}
	checks := statuses[0].Backends[0].TaskDiscovery
	if len(checks) != 1 || checks[0].Status != "down" {
		t.Fatalf("task discovery = %#v, want one down check", checks)
	}
	if !strings.Contains(checks[0].Message, "total_pages") {
		t.Fatalf("task discovery message = %q, want total_pages error", checks[0].Message)
	}
}

func TestCheckAllProviderHealthReportsInvalidCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Fatalf("unexpected task discovery call for invalid capabilities: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	caps := models.JSONString(`{"schema_version":"legacy","task_capabilities":[{"type":"scan"}]}`)
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/tasks", &caps),
	}}, staticServiceTokenProvider("service-token"))

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
	if len(statuses[0].Backends[0].TaskDiscovery) != 0 {
		t.Fatalf("task discovery len = %d, want 0", len(statuses[0].Backends[0].TaskDiscovery))
	}
}

func TestCheckAllProviderHealthReportsUnknownCapabilityFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Fatalf("unexpected task discovery call for invalid capabilities: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	caps := models.JSONString(`{
		"schema_version":"task.capabilities/v2",
		"task_capabilities":[{
			"type":"scan",
			"display_name":"scan",
			"description":"scan task",
			"definition_schema":{"type":"object"},
			"supports_schedule":false,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false,
			"owner_runtime":"legacy"
		}]
	}`)
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/tasks", &caps),
	}}, staticServiceTokenProvider("service-token"))

	statuses, err := service.CheckAllProviderHealth(context.Background(), 0)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "degraded" {
		t.Fatalf("provider status = %q, want degraded", statuses[0].Status)
	}
	if statuses[0].Capabilities == nil || !strings.Contains(statuses[0].Capabilities.Message, "owner_runtime") {
		t.Fatalf("capabilities = %#v, want owner_runtime error", statuses[0].Capabilities)
	}
	if len(statuses[0].Backends[0].TaskDiscovery) != 0 {
		t.Fatalf("task discovery len = %d, want 0", len(statuses[0].Backends[0].TaskDiscovery))
	}
}

func TestCheckAllProviderHealthReportsNonBooleanDeprecated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			t.Fatalf("unexpected task discovery call for invalid capabilities: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	caps := models.JSONString(`{
		"schema_version":"task.capabilities/v2",
		"task_capabilities":[{
			"type":"scan",
			"display_name":"scan",
			"description":"scan task",
			"definition_schema":{"type":"object"},
			"supports_schedule":false,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":"false"
		}]
	}`)
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/tasks", &caps),
	}}, staticServiceTokenProvider("service-token"))

	statuses, err := service.CheckAllProviderHealth(context.Background(), 0)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "degraded" {
		t.Fatalf("provider status = %q, want degraded", statuses[0].Status)
	}
	if statuses[0].Capabilities == nil || !strings.Contains(statuses[0].Capabilities.Message, "deprecated must be boolean") {
		t.Fatalf("capabilities = %#v, want deprecated boolean error", statuses[0].Capabilities)
	}
}

type fakeTaskProviderLister struct {
	providers []*models.TaskProvider
	err       error
}

func newHealthTaskProviderForTest(
	moduleName string,
	displayName string,
	baseURL string,
	enabled bool,
	available bool,
	taskListEndpoint string,
	capabilities *models.JSONString,
) *models.TaskProvider {
	provider := &models.TaskProvider{
		ModuleName: moduleName, Enabled: enabled, Available: available,
		TaskProviderDeclaration: models.TaskProviderDeclaration{
			DisplayName: displayName, TaskListEndpoint: taskListEndpoint, Capabilities: capabilities,
		},
	}
	if baseURL != "" {
		provider.Backends = []models.TaskProviderBackend{{
			InstanceID: "backend-1", BaseURL: baseURL, LeaseExpiresAt: time.Now().Add(time.Hour),
		}}
	}
	if !available {
		provider.UnavailableReason = "no_valid_backend"
	}
	return provider
}

type staticServiceTokenProvider string

func (provider staticServiceTokenProvider) Token(context.Context, uint) (string, error) {
	return string(provider), nil
}

func (f fakeTaskProviderLister) ListTaskProviders() ([]*models.TaskProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.providers, nil
}

func monitorTaskCapabilitiesForTest(items ...string) string {
	return `{"schema_version":"task.capabilities/v2","task_capabilities":[` + strings.Join(items, ",") + `]}`
}

func monitorTaskCapabilityForTest(taskType string, deprecated bool) string {
	deprecatedJSON := "false"
	if deprecated {
		deprecatedJSON = "true"
	}
	return `{
		"type":"` + taskType + `",
		"display_name":"` + taskType + `",
		"description":"` + taskType + ` task",
		"definition_schema":{"type":"object"},
		"supports_schedule":false,
		"supports_cancel":false,
		"supports_inline_execution":false,
		"create_url":"/monitor/` + taskType + `",
		"edit_url":"/monitor/` + taskType + `?task_id=:id",
		"deprecated":` + deprecatedJSON + `
	}`
}
