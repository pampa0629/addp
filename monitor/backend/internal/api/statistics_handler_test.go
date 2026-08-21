package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/service"
)

type runtimeMetricsAPIRepository struct {
	tenantID int
	module   string
}

func (r *runtimeMetricsAPIRepository) List(
	_ context.Context,
	tenantID int,
	module string,
	_, _ time.Time,
) ([]repository.ExecutionRuntimeMetricRow, error) {
	r.tenantID = tenantID
	r.module = module
	return []repository.ExecutionRuntimeMetricRow{{
		Module: "quality", TaskType: "check", ExecutionBoundary: "bounded",
		CreatedCount: 4, CompletedCount: 2, SuccessCount: 1, FailedCount: 1,
		PendingCount: 1, RunningCount: 1,
	}}, nil
}

func TestExecutionRuntimeMetricsRouteUsesCanonicalTenantContext(t *testing.T) {
	authContext := monitorTenantAuthContext()
	authContext.Authorization.RoleAssignments[0].Permissions = []string{"monitor.statistics.read"}
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(authContext); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer systemServer.Close()

	repository := &runtimeMetricsAPIRepository{}
	statisticsService := service.NewStatisticsServiceWithRuntimeMetrics(nil, repository)
	router := SetupRouter(nil, statisticsService, nil, nil, nil, nil, nil, nil, nil, systemServer.URL, nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/executions/runtime-metrics?duration=24h&module=quality", nil)
	request.Header.Set("Authorization", "Bearer addp_at_monitor")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if repository.tenantID != 7 || repository.module != "quality" {
		t.Fatalf("repository filter = tenant %d module %q", repository.tenantID, repository.module)
	}
	var payload service.ExecutionRuntimeMetricsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Duration != "24h" || len(payload.Groups) != 1 || payload.Groups[0].FailureRate != 50 {
		t.Fatalf("response = %#v", payload)
	}
}

func TestExecutionRuntimeMetricsRouteRejectsInvalidDurationInRequestedLanguage(t *testing.T) {
	authContext := monitorTenantAuthContext()
	authContext.Authorization.RoleAssignments[0].Permissions = []string{"monitor.statistics.read"}
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(authContext); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer systemServer.Close()

	statisticsService := service.NewStatisticsServiceWithRuntimeMetrics(nil, &runtimeMetricsAPIRepository{})
	router := SetupRouter(nil, statisticsService, nil, nil, nil, nil, nil, nil, nil, systemServer.URL, nil, nil)

	tests := []struct {
		language string
		message  string
	}{
		{language: "zh-cn", message: "统计窗口无效，仅支持 24h、7d 或 30d"},
		{language: "en", message: "Invalid observation window; supported values are 24h, 7d, and 30d"},
	}
	for _, test := range tests {
		t.Run(test.language, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/executions/runtime-metrics?duration=1h", nil)
			request.Header.Set("Authorization", "Bearer addp_at_monitor")
			request.Header.Set("Accept-Language", test.language)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", response.Code, response.Body.String())
			}
			var payload ErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Error != test.message {
				t.Fatalf("error = %q, want %q", payload.Error, test.message)
			}
		})
	}
}
