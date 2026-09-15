package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
)

func TestExecutorPublishesCompletedStepBeforeStartingNext(t *testing.T) {
	db := newOrchestratorExecutionServiceTestDB(t)
	steps := models.Steps{
		{ID: "first", Name: "First", Provider: "quality", TaskType: "check", TaskID: 41},
		{ID: "second", Name: "Second", Provider: "quality", TaskType: "check", TaskID: 42, DependsOn: []string{"first"}},
	}
	encoded, _ := json.Marshal(steps)
	if err := db.Exec(`INSERT INTO orchestrator.orchestrations (id, tenant_id, name, steps, enabled, schedule) VALUES (11, 7, 'progress', ?, false, '')`, encoded).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewOrchestrationRepository(db)
	service := NewExecutionService(db, repo)
	execution, err := service.CreateExecutionWithContext(context.Background(), 11, 7, "manual", "orchestrator", nil,
		ExecutionActor{PrincipalID: 9, TenantMembershipID: 19, AuthorizationVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	var observed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/42/execute") {
			state, err := service.GetExecution(context.Background(), uint(execution.ID), 7)
			if err != nil {
				t.Error(err)
				http.Error(w, "read failure", 500)
				return
			}
			results, ok := state.Metadata["step_results"].(map[string]interface{})
			if !ok || results["first"] == nil {
				t.Error("completed first step not visible before second starts")
			} else if result, ok := results["first"].(map[string]interface{}); !ok || result["status"] != "success" {
				t.Errorf("first result = %#v", results["first"])
			}
			if state.Status != commonExecution.ExecutionStatusRunning || (state.CurrentStep == nil || *state.CurrentStep != "second") || state.Progress != 50 {
				t.Errorf("intermediate state status=%s current=%v progress=%d", state.Status, state.CurrentStep, state.Progress)
			}
			observed.Store(true)
		}
		if strings.HasSuffix(r.URL.Path, "/execute") {
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"execution_id":"child-exec"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"execution_id":"child-exec","status":"success"}`)
		}
	}))
	defer server.Close()
	executor := &Executor{executionService: service, orchRepo: repo,
		taskProviderResolver: taskProviderResolverWithProvider(&commonModels.TaskProvider{
			ModuleName: "quality", Backends: taskProviderBackendsForTest(server.URL), Available: true,
			TaskProviderDeclaration: commonModels.TaskProviderDeclaration{
				TaskExecuteEndpoint: "/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/executions/{execution_id}",
				Capabilities: jsonStringPtr(taskCapabilitiesForTest("check", false, `{"type":"object","additionalProperties":false}`)),
			},
		}, `{"type":"object","additionalProperties":false}`),
		serviceTokens: commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil }),
	}
	if err := executor.executeSync(context.Background(), uint(execution.ID)); err != nil {
		t.Fatal(err)
	}
	if !observed.Load() {
		t.Fatal("second step never observed")
	}
}
