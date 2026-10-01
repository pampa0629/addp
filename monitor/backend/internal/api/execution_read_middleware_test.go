package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type requestScopeResolver struct{ revoked, unavailable bool }

func (r *requestScopeResolver) Resolve(_ context.Context, _ []string, tenant int, principal int64, bearer string) ([]execution.ReadScope, error) {
	if r.unavailable {
		return nil, service.ErrExecutionOwnerUnavailable
	}
	if r.revoked {
		return []execution.ReadScope{}, nil
	}
	return []execution.ReadScope{{TenantID: tenant, PrincipalID: principal, Module: "orchestrator", Grants: []execution.ReadGrant{{TaskType: "orchestration", TaskHistory: true}}}}, nil
}

func TestExecutionReadAPIRejectsHiddenChildrenRevocationAndUnavailableOwner(t *testing.T) {
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(monitorTenantAuthContext())
	}))
	defer system.Close()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	task, root := "deleted-task", "root"
	for _, row := range []execution.TaskExecution{
		{ExecutionID: root, TenantID: 7, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task, Status: "failed", TriggerType: "manual", ExecutionConfig: models.JSONMap{"secret": "payload-sentinel"}, ErrorDetails: models.JSONMap{"message": "password=payload-sentinel", "stack": "payload-sentinel"}},
		{ExecutionID: "hidden", TenantID: 7, Module: "transfer", TaskType: "sync", SourceTaskID: &task, ParentExecutionID: &root, Status: "success", TriggerType: "manual"},
		{ExecutionID: "other-tenant", TenantID: 8, Module: "orchestrator", TaskType: "orchestration", SourceTaskID: &task, Status: "success", TriggerType: "manual"},
	} {
		if err := repo.Create(context.Background(), &row); err != nil {
			t.Fatal(err)
		}
	}
	resolver := &requestScopeResolver{}
	query := service.NewExecutionQueryService(repo)
	query.SetReadResolver(resolver)
	router := SetupRouter(query, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"))
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer addp_at_monitor")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	response := get("/api/v1/monitor/executions/by-execution-id/root/tree")
	if response.Code != 200 || strings.Contains(response.Body.String(), "hidden") || strings.Contains(response.Body.String(), "payload-sentinel") || strings.Contains(response.Body.String(), "execution_config") {
		t.Fatalf("unsafe tree status=%d body=%s", response.Code, response.Body.String())
	}
	if response = get("/api/v1/monitor/executions/by-execution-id/hidden"); response.Code != 404 {
		t.Fatalf("hidden status=%d", response.Code)
	}
	resolver.revoked = true
	if response = get("/api/v1/monitor/executions/by-execution-id/root"); response.Code != 404 {
		t.Fatalf("revoked status=%d", response.Code)
	}
	resolver.unavailable = true
	if response = get("/api/v1/monitor/executions"); response.Code != 503 {
		t.Fatalf("unavailable status=%d", response.Code)
	}
}
