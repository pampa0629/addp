package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
)

func TestPlanAuthorizationUsesCurrentLeaseBoundary(t *testing.T) {
	parentExecutionID := "74d980cf-3ced-41ef-81fc-271f89249110"
	childExecutionID := "2aaeb79d-2bbd-47a2-a8d4-a607ce6d51a5"
	leaseToken := "5070c2fb-c22d-4ef9-aec2-b90ee2b09228"
	var authorizationRequest commonClient.IssueExecutionAuthorizationFromExecutionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/runtime/execution-authorizations":
			if err := json.NewDecoder(r.Body).Decode(&authorizationRequest); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenSource := qualityCatalogTokenSource("service-token")
	executor := NewCheckExecutor(commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client()), nil, 1)
	task := &models.QualityPlan{
		ID: 1, TenantID: 7, Version: 1,
	}
	execution := &commonExecution.TaskExecution{
		ExecutionID: childExecutionID, TenantID: 7, Module: commonExecution.ModuleQuality,
		Source: commonExecution.ModuleOrchestrator, TaskType: commonExecution.TaskTypeQualityPlan, Status: commonExecution.ExecutionStatusRunning,
		ParentExecutionID: &parentExecutionID,
	}
	lease := commonExecution.Lease{ExecutionID: childExecutionID, TenantID: 7, Attempt: 3, Token: leaseToken, Owner: "quality-worker"}
	config := &planExecutionConfig{
		TaskVersion:       1,
		ParentExecutionID: parentExecutionID,
		TableBindings:     []PlanTableBinding{{Alias: "participation", Locator: "addp://engine/12/path/public/table_3?type=table"}},
		Rules: PlanRuleDocument{
			SchemaVersion: planSchemaVersion,
			Rules: []PlanRule{{
				RuleKey: "00000000-0000-4000-8000-000000000001", Type: "not_null", Severity: "error",
				Params: json.RawMessage(`{"table":"participation","column":"person_id"}`),
			}},
		},
	}

	_, _ = executor.doPlan(context.Background(), task, execution, lease, config)
	if authorizationRequest.ParentExecutionID != parentExecutionID || authorizationRequest.ExecutionID != childExecutionID ||
		authorizationRequest.Audience != commonExecution.AudienceQuality || authorizationRequest.Attempt != lease.Attempt ||
		authorizationRequest.LeaseToken != lease.Token || len(authorizationRequest.Accesses) != 1 ||
		authorizationRequest.Accesses[0].EngineID != "12" || len(authorizationRequest.Accesses[0].Effects) != 1 ||
		authorizationRequest.Accesses[0].Effects[0] != "read" {
		t.Fatalf("authorization request = %#v", authorizationRequest)
	}
}
