package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/models"
)

func TestCheckAllProviderHealthChecksModuleAndTaskDiscovery(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
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
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, staticServiceTokenProvider("service-token"))

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
	if gotAuthorization != "Bearer service-token" {
		t.Fatalf("Authorization = %q, want Bearer service-token", gotAuthorization)
	}
}

func TestCheckAllProviderHealthReportsLegacyTaskDiscoveryShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
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
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, staticServiceTokenProvider("service-token"))

	statuses, err := service.CheckAllProviderHealth(context.Background(), 0)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "down" {
		t.Fatalf("provider status = %q, want down", statuses[0].Status)
	}
	if len(statuses[0].TaskDiscovery) != 1 {
		t.Fatalf("task discovery len = %d, want 1", len(statuses[0].TaskDiscovery))
	}
	if statuses[0].TaskDiscovery[0].Status != "down" {
		t.Fatalf("task discovery status = %q, want down", statuses[0].TaskDiscovery[0].Status)
	}
	if !strings.Contains(statuses[0].TaskDiscovery[0].Message, "data") {
		t.Fatalf("task discovery message = %q, want non-standard data field", statuses[0].TaskDiscovery[0].Message)
	}
}

func TestCheckAllProviderHealthReportsTaskDiscoveryExtraFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
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
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, staticServiceTokenProvider("service-token"))

	statuses, err := service.CheckAllProviderHealth(context.Background(), 0)
	if err != nil {
		t.Fatalf("CheckAllProviderHealth() error = %v", err)
	}
	if statuses[0].Status != "down" {
		t.Fatalf("provider status = %q, want down", statuses[0].Status)
	}
	if len(statuses[0].TaskDiscovery) != 1 || statuses[0].TaskDiscovery[0].Status != "down" {
		t.Fatalf("task discovery = %#v, want one down check", statuses[0].TaskDiscovery)
	}
	if !strings.Contains(statuses[0].TaskDiscovery[0].Message, "total_pages") {
		t.Fatalf("task discovery message = %q, want total_pages error", statuses[0].TaskDiscovery[0].Message)
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

	caps := models.JSONString(`{"schema_version":"legacy","task_capabilities":[{"type":"scan"}]}`)
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, staticServiceTokenProvider("service-token"))

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

func TestCheckAllProviderHealthReportsUnknownCapabilityFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
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
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, staticServiceTokenProvider("service-token"))

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
	if len(statuses[0].TaskDiscovery) != 0 {
		t.Fatalf("task discovery len = %d, want 0", len(statuses[0].TaskDiscovery))
	}
}

func TestCheckAllProviderHealthReportsNonBooleanDeprecated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
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
	service := NewHealthCheckService(fakeTaskProviderLister{providers: []*models.TaskProvider{{
		ModuleName:       "meta",
		DisplayName:      "Meta",
		BaseURL:          server.URL,
		TaskListEndpoint: "/api/v1/meta/tasks",
		Capabilities:     &caps,
	}}}, staticServiceTokenProvider("service-token"))

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
