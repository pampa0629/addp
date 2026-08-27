package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
)

func TestMaterializationGateAuthorizationUsesCurrentLeaseBoundary(t *testing.T) {
	parentExecutionID := "74d980cf-3ced-41ef-81fc-271f89249110"
	childExecutionID := "2aaeb79d-2bbd-47a2-a8d4-a607ce6d51a5"
	leaseToken := "5070c2fb-c22d-4ef9-aec2-b90ee2b09228"
	var authorizationRequest commonClient.IssueExecutionAuthorizationFromExecutionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/model/materialization-groups/9":
			_ = json.NewEncoder(w).Encode(commonClient.MaterializationGroup{
				ID: 9, Code: "outdoor_group", Name: "Outdoor group", Version: 2,
				Members: []commonClient.MaterializationGroupMember{{LogicalTableID: 3, Position: 0}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/model/materialization-read-contexts":
			_ = json.NewEncoder(w).Encode(commonClient.MaterializationReadContext{
				SchemaVersion: "model.materialization-read-context/v1",
				Items: []commonClient.MaterializationReadItem{{
					LogicalTableID: 3, BatchID: "batch-3", EngineID: 12,
					StagingLocator:    "addp://engine/12/path/public/outdoor_staging?type=table",
					Columns:           []commonClient.MaterializationReadColumn{{Name: "person_id", DataType: "text"}},
					SchemaFingerprint: strings.Repeat("a", 64),
				}},
			})
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
	executor := NewCheckExecutor(commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client()), nil, nil, nil, time.Minute, 1)
	executor.ConfigureMaterializationGate(commonClient.NewModelClient(server.URL, tokenSource, server.Client()), nil)
	task := &models.MaterializationGateTask{
		ID: 1, TenantID: 7, Version: 1, MaterializationGroupID: 9, MaterializationGroupVersion: 2,
	}
	execution := &commonExecution.TaskExecution{
		ExecutionID: childExecutionID, TenantID: 7, Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeMaterializationGate, Status: commonExecution.ExecutionStatusRunning,
		ParentExecutionID: &parentExecutionID,
	}
	lease := commonExecution.Lease{ExecutionID: childExecutionID, TenantID: 7, Attempt: 3, Token: leaseToken, Owner: "quality-worker"}
	config := &materializationGateExecutionConfig{
		TaskVersion: 1, MaterializationGroupID: 9, MaterializationGroupVersion: 2,
		ParentExecutionID: parentExecutionID,
		TableBindings:     []MaterializationGateTableBinding{{Alias: "participation", LogicalTableID: 3}},
		Assertions: MaterializationGateAssertionDocument{
			SchemaVersion: materializationGateSchemaVersion,
			Assertions: []MaterializationGateAssertion{{
				AssertionKey: "00000000-0000-4000-8000-000000000001", Type: "not_null", Severity: "error",
				Params: json.RawMessage(`{"table":"participation","column":"person_id"}`),
			}},
		},
	}

	_, _ = executor.doMaterializationGate(context.Background(), task, execution, lease, config)
	if authorizationRequest.ParentExecutionID != parentExecutionID || authorizationRequest.ExecutionID != childExecutionID ||
		authorizationRequest.Audience != commonExecution.AudienceQuality || authorizationRequest.Attempt != lease.Attempt ||
		authorizationRequest.LeaseToken != lease.Token || len(authorizationRequest.Accesses) != 1 ||
		authorizationRequest.Accesses[0].EngineID != "12" || len(authorizationRequest.Accesses[0].Effects) != 1 ||
		authorizationRequest.Accesses[0].Effects[0] != "read" {
		t.Fatalf("authorization request = %#v", authorizationRequest)
	}
}
