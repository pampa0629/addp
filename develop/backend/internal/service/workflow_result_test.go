package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/taskprovider"
	"github.com/addp/develop/backend/internal/protection"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type workflowResultGate struct {
	denied bool
	reads  int
}

func (g *workflowResultGate) BeginCatalogPath(_ context.Context, _ uint, _ plugin.EnginePlugin, _ plugin.EngineCatalogPath) (func(), error) {
	g.reads++
	if g.denied {
		return nil, protection.ErrRequired
	}
	return func() {}, nil
}

func TestWorkflowProfessionalResultRechecksOwnerSourcesAndProtectionWithoutHistoryRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	actor, member, version, by := int64(9), int64(1), int64(1), 9
	item := &execution.TaskExecution{ExecutionID: "own-workflow", TenantID: 7, Module: "develop", TaskType: "workflow", Source: "develop", Status: "success", Attempt: 1, TriggerType: "manual", TriggeredBy: &by, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version,
		Metadata: map[string]interface{}{"result": map[string]interface{}{"final_result": map[string]interface{}{"runtime_type": "dataframe", "row_count": 2}}}}
	if err = repo.Create(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	paths := []plugin.EngineCatalogPath{plugin.FileItemPath(12, "a.gpkg"), plugin.FileItemPath(12, "b.gpkg")}
	denied, calls := false, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/system/engine-access/read-checks/develop-workflow-result" || r.Header.Get("Authorization") != "Bearer addp_at_current" {
			t.Error("wrong source check identity or route")
		}
		var request struct {
			Targets []plugin.EngineCatalogPath `json:"targets"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || !reflect.DeepEqual(request.Targets, paths) {
			t.Error("not the complete actual source set")
		}
		if denied {
			w.WriteHeader(403)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"observed_at": time.Now()})
	}))
	defer server.Close()
	gate := &workflowResultGate{}
	e := &DevExecutor{taskExecutionRepo: repo, workflowResults: newWorkflowResultCache(), workflowEngine: &WorkflowEngineService{systemService: client.NewSystemServiceClient(server.URL, nil, server.Client()), protectionGate: gate}}
	lease := execution.Lease{ExecutionID: item.ExecutionID, Attempt: 1, Token: "original-lease"}
	provider := fileInputsProvider(fileInputsData(t))
	sources := []workflowResultSource{{Provider: provider, Target: paths[0]}, {Provider: provider, Target: paths[1]}}
	table := &plugin.WorkflowTableResult{SchemaVersion: "addp.workflow.table-result/v1", Columns: []string{"instance_id", "area"}, Rows: []map[string]interface{}{{"instance_id": 1, "area": 42.375}, {"instance_id": 2, "area": 9.125}}, RowCount: 2}
	if err = e.workflowResults.put(item, lease, time.Now().Add(time.Minute), sources, table); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.workflowResults.remove(item.ExecutionID, lease.Token) })
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute"})
	ctx := WithExecutionAuthContext(t.Context(), facts)
	response, err := e.GetQueryResult(ctx, item.ExecutionID, 7, "addp_at_current")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response.Metadata)
	if !strings.Contains(string(raw), "42.375") || gate.reads != 2 || calls != 1 {
		t.Fatal("missing authorized complete result", string(raw), gate.reads, calls)
	}
	history, err := e.GetExecution(ctx, item.ExecutionID, 7)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(history)
	if strings.Contains(string(raw), "42.375") || strings.Contains(string(raw), "professional_result_expires_at") {
		t.Fatal("rows or temporary delivery leaked to history")
	}
	raw, _ = json.Marshal(taskprovider.NewExecutionStatusResponse(item))
	if strings.Contains(string(raw), "42.375") {
		t.Fatal("rows leaked to orchestrator")
	}
	for _, scenario := range []string{"other-user", "other-tenant", "revoked-function", "changed-version", "changed-membership"} {
		t.Run(scenario, func(t *testing.T) {
			current := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute"})
			tenant := uint(7)
			switch scenario {
			case "other-user":
				current.Principal.ID = "10"
			case "other-tenant":
				current = authtest.NewTenantUserAuthContext("8", "9", []string{"develop.task.read", "develop.data_read.execute"})
				tenant = 8
			case "revoked-function":
				current = authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read"})
			case "changed-version":
				current.Authorization.AuthorizationVersion = "2"
			case "changed-membership":
				member := "2"
				current.Context.TenantMembershipID = &member
			}
			before := calls
			if value, err := e.GetQueryResult(WithExecutionAuthContext(t.Context(), current), item.ExecutionID, tenant, "addp_at_current"); err == nil || value != nil || calls != before {
				t.Fatal("unauthorized result or source request", scenario, err)
			}
		})
	}
	denied = true
	if value, err := e.GetQueryResult(ctx, item.ExecutionID, 7, "addp_at_current"); err == nil || value != nil {
		t.Fatal("revoked source disclosed rows")
	}
	denied = false
	gate.denied = true
	if value, err := e.GetQueryResult(ctx, item.ExecutionID, 7, "addp_at_current"); err == nil || value != nil {
		t.Fatal("changed protection disclosed rows")
	}
	gate.denied = false
	e.workflowResults.remove(item.ExecutionID, lease.Token)
	if value, err := e.GetQueryResult(ctx, item.ExecutionID, 7, "addp_at_current"); !errors.Is(err, ErrWorkflowResultUnavailable) || value != nil {
		t.Fatal("missing result used history or recomputed", err)
	}
}

func TestWorkflowResultCacheExpiresWithoutExtendingReadsOrRemovingNewGeneration(t *testing.T) {
	cache := newWorkflowResultCache()
	actor := int64(9)
	item := &execution.TaskExecution{ExecutionID: "a", TenantID: 7, ActorPrincipalID: &actor}
	lease := execution.Lease{Attempt: 1, Token: "generation"}
	table := &plugin.WorkflowTableResult{SchemaVersion: "addp.workflow.table-result/v1", Columns: []string{}, Rows: []map[string]interface{}{}, RowCount: 0}
	if err := cache.put(item, lease, time.Now().Add(40*time.Millisecond), []workflowResultSource{{Target: plugin.FileItemPath(1, "a")}}, table); err != nil {
		t.Fatal(err)
	}
	entry := cache.get("a", 1, 7, 9)
	if entry == nil || cache.get("a", 2, 7, 9) != nil || cache.get("a", 1, 8, 9) != nil || cache.get("a", 1, 7, 10) != nil {
		t.Fatal("cache binding missing")
	}
	cache.remove("a", "old")
	if cache.get("a", 1, 7, 9) != entry {
		t.Fatal("old generation removed current result")
	}
	time.Sleep(time.Until(entry.expires) + 20*time.Millisecond)
	if cache.get("a", 1, 7, 9) != nil {
		t.Fatal("expired rows retained")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) != 0 {
		t.Fatal("timer did not release result")
	}
}
