package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type traceRequestResolver struct {
	calls               int
	owners              []string
	denied, unavailable bool
}

func (r *traceRequestResolver) Resolve(_ context.Context, owners []string, tenant int, principal int64, bearer string) ([]execution.ReadScope, error) {
	r.calls++
	r.owners = owners
	if bearer != "Bearer exact-user" {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	if r.unavailable {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	grants := []execution.ReadGrant{}
	if !r.denied {
		grants = []execution.ReadGrant{{TaskType: "orchestration", TaskHistory: true}, {TaskType: "sync", TaskHistory: true}}
	}
	return []execution.ReadScope{{Module: owners[0], TenantID: tenant, PrincipalID: principal, Grants: grants}}, nil
}
func TestExecutionTraceRouteReauthorizesAndRejectsSelectors(t *testing.T) {
	permissions := []string{"monitor.execution.read"}
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(authtest.NewTenantUserAuthContext("7", "9", permissions))
	}))
	defer system.Close()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	id := "a110b9a2-0000-4000-8000-000000000001"
	unsupported := "a110b9a2-0000-4000-8000-000000000002"
	task := "deleted-task"
	for _, row := range []execution.TaskExecution{{ExecutionID: id, Module: "orchestrator", TaskType: "orchestration"}, {ExecutionID: unsupported, Module: "transfer", TaskType: "sync"}} {
		row.TenantID = 7
		row.SourceTaskID = &task
		row.Status = "failed"
		row.TriggerType = "manual"
		row.ExecutionBoundary = execution.ExecutionBoundaryBounded
		row.Metadata = models.JSONMap{"secret": "private-payload"}
		row.ExecutionConfig = models.JSONMap{"secret": "private-config"}
		if err := repo.Create(t.Context(), &row); err != nil {
			t.Fatal(err)
		}
	}
	resolver := &traceRequestResolver{}
	query := service.NewExecutionQueryService(repo)
	query.SetReadResolver(resolver)
	traces := service.NewTraceService(nil, nil)
	router := SetupRouter(query, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, nil, nil, nil, nil, traces, nil, nil)
	get := func(path, body string, want int) string {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/monitor/executions/by-execution-id/"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer exact-user")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s: %d want %d %s", path, response.Code, want, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" && want != 403 {
			t.Fatal("missing no-store")
		}
		raw := response.Body.String()
		for _, secret := range []string{"private-", "execution_config", "metadata", "trace_id", "span_id", "instance_id", "parent_id", "links"} {
			if strings.Contains(raw, secret) {
				t.Fatalf("leaked %s: %s", secret, raw)
			}
		}
		return raw
	}
	raw := get(id+"/trace-stages", "", 200)
	var value service.ExecutionTraceStagesResponse
	if err := json.Unmarshal([]byte(raw), &value); err != nil || !value.Supported || len(value.Stages) != 2 || value.Stages[0].State != "not_recorded" {
		t.Fatal(raw, err)
	}
	if resolver.calls != 1 || len(resolver.owners) != 1 || resolver.owners[0] != "orchestrator" {
		t.Fatal("wrong Owner scope", resolver)
	}
	get(id+"/trace-stages", "", 200)
	if resolver.calls != 2 {
		t.Fatal("cached Owner decision")
	}
	resolver.denied = true
	get(id+"/trace-stages", "", 404)
	resolver.denied = false
	resolver.unavailable = true
	get(id+"/trace-stages", "", 503)
	resolver.unavailable = false
	raw = get(unsupported+"/trace-stages", "", 200)
	if !strings.Contains(raw, `"supported":false`) || !strings.Contains(raw, `"stages":[]`) {
		t.Fatal(raw)
	}
	before := resolver.calls
	for _, path := range []string{"not-uuid/trace-stages", strings.ToUpper(id) + "/trace-stages", id + "/trace-stages?trace_id=private", id + "/trace-stages?start=2026-01-01"} {
		get(path, "", 400)
	}
	get(id+"/trace-stages", `{}`, 400)
	get("a110b9a2-0000-4000-8000-000000000003/trace-stages", "", 404)
	if resolver.calls != before {
		t.Fatal("invalid/missing request contacted Owner")
	}
	permissions = []string{"monitor.statistics.read"}
	get(id+"/trace-stages", "", 403)
	if resolver.calls != before {
		t.Fatal("permission failure contacted Owner")
	}
}
