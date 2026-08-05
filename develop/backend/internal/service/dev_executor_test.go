package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExecuteWithParamsFromParentExecutionKeepsFailedChildWhenAuthorizationFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"develop", "common"} {
		if err := db.Exec("ATTACH DATABASE ':memory:' AS " + schema).Error; err != nil {
			t.Fatalf("attach %s schema: %v", schema, err)
		}
	}
	for _, statement := range []string{
		`CREATE TABLE develop.dev_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			display_name TEXT, dev_type TEXT NOT NULL, content JSON NOT NULL, execution_config JSON,
			editor_layout JSON, timeout INTEGER, description TEXT, tags TEXT, created_by INTEGER,
			updated_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
			status TEXT, last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME
		)`,
		`CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL, task_type TEXT NOT NULL, source TEXT NOT NULL, source_task_id TEXT,
			source_task_name TEXT, parent_execution_id TEXT, status TEXT NOT NULL, progress INTEGER,
			current_step TEXT, trigger_type TEXT NOT NULL, triggered_by INTEGER,
			actor_principal_id INTEGER, actor_tenant_membership_id INTEGER, issued_authorization_version INTEGER,
			execution_authorization_id INTEGER, authorization_effects TEXT, authorization_expires_at DATETIME,
			execution_config JSON, error_details JSON, metadata JSON, execution_time_ms INTEGER,
			rows_affected INTEGER, records_read INTEGER, records_written INTEGER, bytes_read INTEGER,
			bytes_written INTEGER, started_at DATETIME, completed_at DATETIME, created_at DATETIME, updated_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create execution fixture table: %v", err)
		}
	}
	devTaskRepo := repository.NewDevTaskRepository(db)
	executionRepo := commonExecution.NewTaskExecutionRepository(db)
	devTask := &models.DevTask{
		TenantID: 7, Name: "scheduled query", DevType: commonExecution.TaskTypeQuery,
		Status: "active", Timeout: 60,
		Content:         models.DevTaskContent{"query_type": "sql", "query": "SELECT 1"},
		ExecutionConfig: models.DevTaskContent{"engine_id": float64(12)},
	}
	if err := devTaskRepo.Create(devTask); err != nil {
		t.Fatalf("create DevTask: %v", err)
	}
	parentExecutionID := uuid.New().String()
	principalID, membershipID, authorizationVersion := int64(31), int64(37), int64(5)
	parent := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: parentExecutionID, Module: commonExecution.ModuleOrchestrator,
		TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator,
		Status: commonExecution.ExecutionStatusRunning, TriggerType: commonExecution.TriggerTypeScheduled,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := executionRepo.Create(context.Background(), parent); err != nil {
		t.Fatalf("create parent execution: %v", err)
	}

	called := false
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		called = true
		if request.URL.Path != "/api/v1/system/runtime/execution-authorizations" ||
			request.Header.Get("Authorization") != "Bearer addp_at_develop" ||
			request.Header.Get("X-Internal-API-Key") != "" || request.Header.Get("X-Tenant-ID") != "" {
			t.Fatalf("invalid authorization request: path=%s headers=%#v", request.URL.Path, request.Header)
		}
		http.Error(w, `{"error":"denied"}`, http.StatusForbidden)
	}))
	defer system.Close()
	systemClient := commonClient.NewSystemServiceClient(
		system.URL,
		staticServiceTokenSource("addp_at_develop"),
		system.Client(),
	)
	executor := &DevExecutor{
		devTaskRepo: devTaskRepo, taskExecutionRepo: executionRepo,
		sqlEngine: NewSQLEngineService(&config.Config{}, systemClient, nil),
	}
	childExecutionID, err := executor.ExecuteWithParamsFromParentExecution(
		context.Background(), devTask.ID, nil, 7, commonExecution.TriggerTypeScheduled,
		commonExecution.ModuleOrchestrator, parentExecutionID, commonExecution.TaskTypeQuery,
	)
	if err == nil || childExecutionID == "" || !called {
		t.Fatalf("child execution id=%q error=%v called=%t", childExecutionID, err, called)
	}
	child, loadErr := executionRepo.GetByExecutionID(context.Background(), childExecutionID, 7)
	if loadErr != nil {
		t.Fatalf("load failed child execution: %v", loadErr)
	}
	if child.Status != commonExecution.ExecutionStatusFailed || child.CompletedAt == nil ||
		child.ParentExecutionID == nil || *child.ParentExecutionID != parentExecutionID ||
		child.ActorPrincipalID == nil || *child.ActorPrincipalID != principalID ||
		child.ActorTenantMembershipID == nil || *child.ActorTenantMembershipID != membershipID ||
		child.IssuedAuthorizationVersion == nil || *child.IssuedAuthorizationVersion != authorizationVersion ||
		child.ExecutionAuthorizationID != nil {
		t.Fatalf("failed child execution facts = %#v", child)
	}
}

func TestApplySQLExecutionAuthorizationFactsPersistsOnlyReferencesAndSummary(t *testing.T) {
	expiresAt := time.Date(2026, 7, 29, 10, 15, 0, 0, time.UTC)
	execution := &commonExecution.TaskExecution{ExecutionID: "execution-1"}
	applySQLExecutionAuthorizationFacts(execution, &IssuedSQLExecutionAuthorization{
		AuthorizationID: 71, Effect: SQLExecutionEffectRead, ActorPrincipalID: 11,
		ActorTenantMembershipID: 13, IssuedAuthorizationVersion: 17, ExpiresAt: expiresAt,
	})
	if execution.ExecutionAuthorizationID == nil || *execution.ExecutionAuthorizationID != 71 ||
		execution.ActorPrincipalID == nil || *execution.ActorPrincipalID != 11 ||
		execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID != 13 ||
		execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion != 17 ||
		len(execution.AuthorizationEffects) != 1 || execution.AuthorizationEffects[0] != "read" ||
		execution.AuthorizationExpiresAt == nil || !execution.AuthorizationExpiresAt.Equal(expiresAt) {
		t.Fatalf("execution authorization facts = %#v", execution)
	}
	encoded, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	for _, secretMarker := range []string{"addp_at_", "connection_info", "access_plan", "user_token", "service_token"} {
		if strings.Contains(string(encoded), secretMarker) {
			t.Fatalf("execution serialization leaked %q: %s", secretMarker, encoded)
		}
	}
}

func TestApplyWorkflowExecutionAuthorizationFactsPersistsOnlyReferencesAndEffects(t *testing.T) {
	expiresAt := time.Date(2026, 7, 29, 10, 15, 0, 0, time.UTC)
	execution := &commonExecution.TaskExecution{ExecutionID: "execution-2"}
	applyWorkflowExecutionAuthorizationFacts(execution, &IssuedWorkflowExecutionAuthorization{
		AuthorizationID: 81, ActorPrincipalID: 21, ActorTenantMembershipID: 23,
		IssuedAuthorizationVersion: 27, Effects: []string{"read", "write"}, ExpiresAt: expiresAt,
		EngineEffects: map[uint][]string{12: {"read"}, 13: {"write"}},
	})
	if execution.ExecutionAuthorizationID == nil || *execution.ExecutionAuthorizationID != 81 ||
		execution.ActorPrincipalID == nil || *execution.ActorPrincipalID != 21 ||
		execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID != 23 ||
		execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion != 27 ||
		!reflect.DeepEqual([]string(execution.AuthorizationEffects), []string{"read", "write"}) ||
		execution.AuthorizationExpiresAt == nil || !execution.AuthorizationExpiresAt.Equal(expiresAt) {
		t.Fatalf("execution authorization facts = %#v", execution)
	}
	encoded, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"connection_info", "access_plan", "engine_effects", "user_token", "service_token"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("execution serialization leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestExecuteWorkflowRejectsInvalidWorkflowDefinitionBeforeRuntime(t *testing.T) {
	executor := &DevExecutor{}
	result, errorMessage := executor.executeWorkflow(context.Background(), &models.DevTask{
		DevType: commonExecution.TaskTypeWorkflow,
		Content: models.DevTaskContent{
			"workflow_definition": map[string]interface{}{
				"tasks": []interface{}{
					map[string]interface{}{
						"id":       "task1",
						"operator": "load",
						"params":   map[string]interface{}{},
					},
				},
			},
		},
		ExecutionConfig: models.DevTaskContent{
			"engine_id": float64(7),
		},
	}, "execution-1", 1, nil)

	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if !strings.Contains(errorMessage, "depends_on") {
		t.Fatalf("errorMessage = %q, want depends_on validation error", errorMessage)
	}
}

func TestWorkflowProducedTargetScanOptionsUseRefGroupsForFileTargets(t *testing.T) {
	opts := workflowProducedTargetScanOptions(WorkflowProducedTarget{
		EngineID: 26,
		Type:     "file",
		Path:     []string{"supermap", "result.udbx"},
		Locator:  "addp://engine/26/path/supermap/result.udbx?type=file",
	})

	if opts.EngineID != 26 || opts.ScanDepth != "deep" || !opts.Force {
		t.Fatalf("scan options base fields = %#v", opts)
	}
	if len(opts.Targets) != 0 {
		t.Fatalf("file target should not use locator targets: %#v", opts.Targets)
	}
	if len(opts.RefGroups) != 1 || opts.RefGroups[0].Primary != "supermap/result.udbx" {
		t.Fatalf("ref_groups = %#v, want primary supermap/result.udbx", opts.RefGroups)
	}
}

func TestWorkflowExecutionOutputsAreAddressedByTaskID(t *testing.T) {
	outputs := workflowExecutionOutputs([]WorkflowProducedTarget{
		{TaskID: "save_3", Type: "table", Locator: "addp://engine/7/path/public/roads_buffered?type=table"},
	})
	taskOutput, ok := outputs["save_3"].(map[string]interface{})
	if !ok {
		t.Fatalf("outputs = %#v, want save_3 object", outputs)
	}
	resource, ok := taskOutput["resource"].(map[string]interface{})
	if !ok || resource["type"] != "table" || resource["locator"] == "" {
		t.Fatalf("resource output = %#v", taskOutput["resource"])
	}
}

func TestWorkflowProducedTargetScanOptionsUseTargetsForTableTargets(t *testing.T) {
	locator := "addp://engine/8/path/public/result?type=table"
	opts := workflowProducedTargetScanOptions(WorkflowProducedTarget{
		EngineID: 8,
		Type:     "table",
		Path:     []string{"public", "result"},
		Locator:  locator,
	})

	if !reflect.DeepEqual(opts.Targets, []string{locator}) {
		t.Fatalf("targets = %#v, want locator target", opts.Targets)
	}
	if len(opts.RefGroups) != 0 {
		t.Fatalf("table target should not use ref_groups: %#v", opts.RefGroups)
	}
}
