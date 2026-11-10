package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestCheckAllProviderHealthUsesReadinessOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health/ready" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected probe: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	for _, tc := range []struct {
		name       string
		deprecated bool
		want       string
	}{
		{"active", false, "up"},
		{"all_deprecated", true, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caps := models.JSONString(monitorTaskCapabilitiesForTest(monitorTaskCapabilityForTest("scan", tc.deprecated)))
			svc := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{
				newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/task-provider/tasks", &caps),
			}})
			statuses, err := svc.CheckAllProviderHealth(t.Context())
			if err != nil || len(statuses) != 1 || statuses[0].Status != tc.want {
				t.Fatalf("provider statuses = %#v, err = %v, want %s", statuses, err, tc.want)
			}
			if len(statuses[0].Backends) != 1 || statuses[0].Backends[0].ModuleHealth.Status != "up" {
				t.Fatalf("backend readiness = %#v, want one ready instance", statuses[0].Backends)
			}
		})
	}
}

func TestCheckAllProviderHealthChecksEveryBackendAndAggregatesDegraded(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/ready":
			w.WriteHeader(http.StatusOK)
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
	provider := newHealthTaskProviderForTest("meta", "Meta", healthy.URL, true, true, "/api/v1/meta/task-provider/tasks", &caps)
	provider.Backends = append(provider.Backends, models.TaskProviderBackend{
		InstanceID: "backend-2", BaseURL: unhealthy.URL, LeaseExpiresAt: time.Now().Add(time.Hour),
	})
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{provider}})

	statuses, err := service.CheckAllProviderHealth(t.Context())
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
		newHealthTaskProviderForTest("meta", "Meta", "", true, false, "/api/v1/meta/task-provider/tasks", &caps),
	}})

	statuses, err := service.CheckAllProviderHealth(t.Context())
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
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/task-provider/tasks", &caps),
	}})

	statuses, err := service.CheckAllProviderHealth(t.Context())
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "degraded" {
		t.Fatalf("provider status = %q, want degraded", statuses[0].Status)
	}
	if statuses[0].Capabilities == nil || statuses[0].Capabilities.Status != "down" {
		t.Fatalf("capabilities = %#v, want down", statuses[0].Capabilities)
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
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/task-provider/tasks", &caps),
	}})

	statuses, err := service.CheckAllProviderHealth(t.Context())
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "degraded" {
		t.Fatalf("provider status = %q, want degraded", statuses[0].Status)
	}
	if statuses[0].Capabilities == nil || !strings.Contains(statuses[0].Capabilities.Message, "owner_runtime") {
		t.Fatalf("capabilities = %#v, want owner_runtime error", statuses[0].Capabilities)
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
		newHealthTaskProviderForTest("meta", "Meta", server.URL, true, true, "/api/v1/meta/task-provider/tasks", &caps),
	}})

	statuses, err := service.CheckAllProviderHealth(t.Context())
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
