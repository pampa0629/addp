package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type runtimeMetricsAPIRepository struct {
	tenantID int
	module   string
}

func (r *runtimeMetricsAPIRepository) List(
	ctx context.Context,
	tenantID int,
	module string,
	_, _ time.Time,
) ([]repository.ExecutionRuntimeMetricRow, error) {
	if scopes, ok := execution.ReadScopesFromContext(ctx); !ok || len(scopes) != 1 || scopes[0].TenantID != tenantID {
		return nil, service.ErrExecutionOwnerUnavailable
	}
	r.tenantID = tenantID
	r.module = module
	return []repository.ExecutionRuntimeMetricRow{{
		Module: "quality", TaskType: "quality_plan", ExecutionBoundary: "bounded",
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
	router := SetupRouter(runtimeMetricsReadQuery(t), statisticsService, nil, nil, nil, nil, nil, nil, nil, systemServer.URL, nil, nil, modulelifecycle.NewStandalone("monitor"))

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
	router := SetupRouter(runtimeMetricsReadQuery(t), statisticsService, nil, nil, nil, nil, nil, nil, nil, systemServer.URL, nil, nil, modulelifecycle.NewStandalone("monitor"))

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

type metricsReadResolver struct{}

func (metricsReadResolver) Resolve(_ context.Context, _ []string, tenant int, principal int64, _ string) ([]execution.ReadScope, error) {
	return []execution.ReadScope{{TenantID: tenant, PrincipalID: principal, Module: "quality", Grants: []execution.ReadGrant{{TaskType: "quality_plan", TaskHistory: true}}}}, nil
}
func runtimeMetricsReadQuery(t *testing.T) *service.ExecutionQueryService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	task := "metrics-task"
	repo := execution.NewTaskExecutionRepository(db)
	if err := repo.Create(context.Background(), &execution.TaskExecution{ExecutionID: "metrics-execution", TenantID: 7, Module: "quality", TaskType: "quality_plan", SourceTaskID: &task, Status: "success", TriggerType: "manual"}); err != nil {
		t.Fatal(err)
	}
	query := service.NewExecutionQueryService(repo)
	query.SetReadResolver(metricsReadResolver{})
	return query
}
