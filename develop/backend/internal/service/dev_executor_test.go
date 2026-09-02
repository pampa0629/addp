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
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDevelopLineageFactsUsesWorkflowDefinitionResourcesAndOutputs(t *testing.T) {
	task := &models.DevTask{
		DevType: commonExecution.TaskTypeWorkflow,
		Content: map[string]interface{}{
			"inputs": map[string]interface{}{},
			"workflow_definition": map[string]interface{}{
				"tasks": []interface{}{
					map[string]interface{}{
						"id":       "load_1",
						"operator": "load",
						"params": map[string]interface{}{
							"locator": "addp://engine/1/path/public/source?type=table&item_id=11",
						},
					},
				},
			},
		},
	}
	outputs := commonModels.JSONMap{"save_3": map[string]interface{}{"resource": map[string]interface{}{"locator": "addp://engine/1/path/public/target?type=table", "write_mode": "replace"}}}
	facts := developLineageFacts(task, outputs)
	if facts == nil || len(facts.Inputs) != 1 || len(facts.Outputs) != 1 {
		t.Fatalf("facts = %#v", facts)
	}
	if facts.Inputs[0].Locator != "addp://engine/1/path/public/source?type=table&item_id=11" {
		t.Fatalf("input locator = %q", facts.Inputs[0].Locator)
	}
	if facts.Outputs[0].Locator != "addp://engine/1/path/public/target?type=table" {
		t.Fatalf("output locator = %q", facts.Outputs[0].Locator)
	}
	if facts.Outputs[0].WriteMode != "replace" {
		t.Fatalf("output write_mode = %q, want replace", facts.Outputs[0].WriteMode)
	}
	if facts.SchemaVersion != commonExecution.LineageFactsSchemaVersion || facts.Operations[0].Operator != "develop" {
		t.Fatalf("facts = %#v", facts)
	}
}

func TestExecuteWithParamsFromParentExecutionQueuesQueryWithoutBackendAuthorization(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatalf("attach develop schema: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE develop.dev_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
			display_name TEXT, dev_type TEXT NOT NULL, content JSON NOT NULL, execution_config JSON,
			editor_layout JSON, timeout INTEGER, description TEXT, tags TEXT, created_by INTEGER,
			updated_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
			status TEXT, last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME
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
	if err != nil || childExecutionID == "" || called {
		t.Fatalf("child execution id=%q error=%v called=%t", childExecutionID, err, called)
	}
	child, loadErr := executionRepo.GetByExecutionID(context.Background(), childExecutionID, 7)
	if loadErr != nil {
		t.Fatalf("load failed child execution: %v", loadErr)
	}
	if child.Status != commonExecution.ExecutionStatusPending || child.CompletedAt != nil ||
		child.ParentExecutionID == nil || *child.ParentExecutionID != parentExecutionID ||
		child.ActorPrincipalID == nil || *child.ActorPrincipalID != principalID ||
		child.ActorTenantMembershipID == nil || *child.ActorTenantMembershipID != membershipID ||
		child.IssuedAuthorizationVersion == nil || *child.IssuedAuthorizationVersion != authorizationVersion ||
		child.ExecutionAuthorizationID != nil {
		t.Fatalf("queued child execution facts = %#v", child)
	}
	if _, ok := child.ExecutionConfig["content"]; !ok {
		t.Fatalf("queued child did not freeze content: %#v", child.ExecutionConfig)
	}
}

func TestApplySQLExecutionAuthorizationFactsPersistsOnlyReferences(t *testing.T) {
	expiresAt := time.Date(2026, 7, 29, 10, 15, 0, 0, time.UTC)
	execution := &commonExecution.TaskExecution{ExecutionID: "execution-1"}
	applySQLExecutionAuthorizationFacts(execution, &IssuedSQLExecutionAuthorization{
		AuthorizationID: 71, Effects: []SQLExecutionEffect{SQLExecutionEffectRead}, ActorPrincipalID: 11,
		ActorTenantMembershipID: 13, IssuedAuthorizationVersion: 17, ExpiresAt: expiresAt,
	})
	if execution.ExecutionAuthorizationID == nil || *execution.ExecutionAuthorizationID != 71 ||
		execution.ActorPrincipalID == nil || *execution.ActorPrincipalID != 11 ||
		execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID != 13 ||
		execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion != 17 ||
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

func TestCompileRelationPreviewQueryUsesEffectiveTableBinding(t *testing.T) {
	compiled, err := compileRelationPreviewQuery(&models.DevTask{
		DevType: commonExecution.TaskTypeQuery,
		Content: models.DevTaskContent{
			"query": "SELECT * FROM source", "query_type": "sql",
			"query_parameters": []interface{}{map[string]interface{}{"name": "source", "type": "relation"}},
		},
		ExecutionConfig: models.DevTaskContent{"engine_id": 12},
	}, map[string]interface{}{
		"source": map[string]interface{}{"locator": "addp://engine/12/path/public/source?type=table"},
	}, "postgresql")
	if err != nil {
		t.Fatal(err)
	}
	if got := compiled.Content["query"]; got != `SELECT * FROM "public"."source"` {
		t.Fatalf("compiled query = %q", got)
	}
}

func TestApplyWorkflowExecutionAuthorizationFactsPersistsOnlyReferences(t *testing.T) {
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
	result, outputs, errorMessage := executor.executeWorkflow(context.Background(), &models.DevTask{
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
	if outputs != nil {
		t.Fatalf("outputs = %#v, want nil", outputs)
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
		{TaskID: "save_3", Type: "table", Locator: "addp://engine/7/path/public/roads_buffered?type=table", WriteMode: "replace"},
	})
	taskOutput, ok := outputs["save_3"].(map[string]interface{})
	if !ok {
		t.Fatalf("outputs = %#v, want save_3 object", outputs)
	}
	resource, ok := taskOutput["resource"].(map[string]interface{})
	if !ok || resource["type"] != "table" || resource["locator"] == "" || resource["write_mode"] != "replace" {
		t.Fatalf("resource output = %#v", taskOutput["resource"])
	}
}

func TestWorkflowFinalResultForExecutionKeepsSmallStructuredResult(t *testing.T) {
	result, ok := workflowFinalResultForExecution(`[{"area":42}]`)
	if !ok {
		t.Fatal("workflowFinalResultForExecution() ok = false, want true")
	}
	rows, ok := result.([]interface{})
	if !ok || len(rows) != 1 || rows[0].(map[string]interface{})["area"] != float64(42) {
		t.Fatalf("result = %#v, want one row with area 42", result)
	}
}

func TestWorkflowFinalResultForExecutionSkipsLargeResult(t *testing.T) {
	result, ok := workflowFinalResultForExecution(strings.Repeat("x", workflowExecutionResultPreviewLimitBytes+1))
	if ok || result != nil {
		t.Fatalf("workflowFinalResultForExecution() = (%#v, %v), want (nil, false)", result, ok)
	}
}

func TestWorkflowProducedTargetCarriesCanonicalWriteMode(t *testing.T) {
	params := map[string]interface{}{
		"target_parent_locator": "addp://engine/7/path/public?type=schema",
		"target_name":           "roads_buffered",
		"mode":                  "overwrite",
	}
	targets, err := deriveWorkflowResourceParams(params, workflowPythonSaveAdapterSpec())
	if err != nil {
		t.Fatalf("deriveWorkflowResourceParams() error = %v", err)
	}
	if len(targets) != 1 || targets[0].WriteMode != "replace" {
		t.Fatalf("targets = %#v, want replace write mode", targets)
	}
}

func TestNotifyExecutionLineageFailureDoesNotMutateSuccessfulExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	executionID := uuid.New().String()
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, status, progress, trigger_type, metadata, created_at, updated_at)
		VALUES (7, ?, 'develop', 'workflow', 'develop', 'success', 100, 'manual', '{"lineage_facts":{}}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, executionID).Error; err != nil {
		t.Fatalf("insert execution: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"temporary failure"}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()
	executor := &DevExecutor{
		taskExecutionRepo: commonExecution.NewTaskExecutionRepository(db),
		metaClient:        commonClient.NewMetaClient(server.URL, staticServiceTokenSource("addp_at_develop")),
	}

	executor.notifyExecutionLineage(context.Background(), 7, executionID)

	execution, err := executor.taskExecutionRepo.GetByExecutionID(context.Background(), executionID, 7)
	if err != nil {
		t.Fatalf("load execution: %v", err)
	}
	if execution.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %q, want success", execution.Status)
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

func TestQueryResultAppliesPreviewLimitAndTruncation(t *testing.T) {
	executor := &DevExecutor{queryResultLimit: 2}
	result, errorMessage, rowsAffected := executor.queryResult(
		[]string{"id"},
		[]map[string]interface{}{{"id": 1}, {"id": 2}, {"id": 3}},
		3,
		SQLExecutionEffectRead,
		"table",
		nil,
	)
	if errorMessage != "" || rowsAffected != nil {
		t.Fatalf("error = %q, rowsAffected = %v", errorMessage, rowsAffected)
	}
	if result["rows_count"] != 2 || result["result_limit"] != 2 || result["truncated"] != true {
		t.Fatalf("result limit metadata = %#v", result)
	}
	if _, exists := result["rows_affected"]; exists {
		t.Fatalf("read preview must not expose rows_affected: %#v", result)
	}
	summary, ok := result["summary"].(map[string]interface{})
	if !ok {
		t.Fatalf("summary = %#v", result["summary"])
	}
	rows, ok := summary["preview_rows"].([]map[string]interface{})
	if !ok || len(rows) != 2 {
		t.Fatalf("preview_rows = %#v", summary["preview_rows"])
	}
}

func TestQueryResultPreservesRowsAffectedForWrite(t *testing.T) {
	executor := &DevExecutor{queryResultLimit: 2}
	result, errorMessage, rowsAffected := executor.queryResult(
		nil,
		nil,
		7,
		SQLExecutionEffectWrite,
		"table",
		nil,
	)
	if errorMessage != "" || rowsAffected == nil || *rowsAffected != 7 {
		t.Fatalf("error = %q, rowsAffected = %v", errorMessage, rowsAffected)
	}
	if result["rows_affected"] != int64(7) {
		t.Fatalf("write result rows_affected = %#v", result["rows_affected"])
	}
}

func TestGraphResultCapabilityAndPreviewLimit(t *testing.T) {
	capabilitiesJSON, err := plugin.MarshalEngineCapabilities(plugin.NewGraphCapabilities("neo4j"))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := commonModels.JSONString(capabilitiesJSON)
	engine := &commonModels.Engine{Capabilities: &capabilities}
	if !engineSupportsQueryResultKind(engine, "graph") {
		t.Fatal("graph capability was not detected")
	}

	graph, truncated := truncateGraphData(&plugin.GraphData{
		Nodes: []plugin.GraphNode{
			{ElementId: "1"}, {ElementId: "2"}, {ElementId: "3"},
		},
		Relationships: []plugin.GraphRelationship{
			{ElementId: "r1", StartNodeId: "1", EndNodeId: "2"},
			{ElementId: "r2", StartNodeId: "2", EndNodeId: "3"},
		},
	}, 2)
	if !truncated || len(graph.Nodes) != 2 || len(graph.Relationships) != 1 {
		t.Fatalf("graph = %#v, truncated = %v", graph, truncated)
	}
}

func TestExecutionStatusForQueryTimeout(t *testing.T) {
	if got := executionStatusForError("查询执行失败: context deadline exceeded"); got != commonExecution.ExecutionStatusTimeout {
		t.Fatalf("status = %q, want timeout", got)
	}
	if got := executionStatusForError("查询执行失败: connection refused"); got != commonExecution.ExecutionStatusFailed {
		t.Fatalf("status = %q, want failed", got)
	}
}
