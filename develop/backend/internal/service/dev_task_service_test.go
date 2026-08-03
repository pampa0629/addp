package service

import (
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
)

func TestValidateDevTaskContentAcceptsCanonicalQuery(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeQuery, map[string]interface{}{
		"query":      "SELECT 1",
		"query_type": "sql",
	})
	if err != nil {
		t.Fatalf("expected canonical query content to pass, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsLegacyQuerySQL(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeQuery, map[string]interface{}{
		"sql":        "SELECT 1",
		"query_type": "sql",
	})
	if err == nil {
		t.Fatal("expected legacy content.sql to be rejected")
	}
	if !strings.Contains(err.Error(), "content.query") {
		t.Fatalf("expected content.query error, got %v", err)
	}
}

func TestValidateDevTaskExecutionConfigAcceptsQueryEngineID(t *testing.T) {
	err := validateDevTaskExecutionConfig(
		commonExecution.TaskTypeQuery,
		map[string]interface{}{
			"query":      "SELECT 1",
			"query_type": "sql",
		},
		map[string]interface{}{
			"engine_id": 1,
		},
	)
	if err != nil {
		t.Fatalf("expected SQL query engine_id config to pass, got %v", err)
	}
}

func TestValidateDevTaskExecutionConfigRejectsMissingQueryEngineID(t *testing.T) {
	err := validateDevTaskExecutionConfig(
		commonExecution.TaskTypeQuery,
		map[string]interface{}{
			"query":      "SELECT 1",
			"query_type": "sql",
		},
		map[string]interface{}{},
	)
	if err == nil {
		t.Fatal("expected SQL query without engine_id to be rejected")
	}
	if !strings.Contains(err.Error(), "execution_config.engine_id") {
		t.Fatalf("expected engine_id error, got %v", err)
	}
}

func TestValidateDevTaskExecutionConfigUsesEngineIDForDuckDBRuntime(t *testing.T) {
	err := validateDevTaskExecutionConfig(
		commonExecution.TaskTypeQuery,
		map[string]interface{}{
			"query":      "SELECT 1",
			"query_type": "sql",
		},
		map[string]interface{}{"engine_id": 1},
	)
	if err != nil {
		t.Fatalf("expected runtime engine_id to pass, got %v", err)
	}
}

func TestValidateDevTaskExecutionConfigRequiresScriptEngineID(t *testing.T) {
	err := validateDevTaskExecutionConfig(
		commonExecution.TaskTypeScript,
		map[string]interface{}{"notebook_path": "analysis.ipynb"},
		map[string]interface{}{},
	)
	if err == nil || !strings.Contains(err.Error(), "execution_config.engine_id") {
		t.Fatalf("expected script engine_id error, got %v", err)
	}
}

func TestValidateDevTaskExecutionConfigAcceptsScriptEngineID(t *testing.T) {
	err := validateDevTaskExecutionConfig(
		commonExecution.TaskTypeScript,
		map[string]interface{}{"notebook_path": "analysis.ipynb"},
		map[string]interface{}{"engine_id": 10},
	)
	if err != nil {
		t.Fatalf("expected script engine_id config to pass, got %v", err)
	}
}

func TestValidateDevTaskContentAcceptsCanonicalWorkflow(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":         "load_1",
					"operator":   "load",
					"params":     map[string]interface{}{},
					"depends_on": []interface{}{},
				},
				map[string]interface{}{
					"id":       "save_1",
					"operator": "save",
					"params": map[string]interface{}{
						"target_name": "result",
					},
					"depends_on": []interface{}{"load_1"},
				},
			},
		},
		"inputs": map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("expected canonical workflow content to pass, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsWorkflowWithoutTasks(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"nodes": []interface{}{},
			"edges": []interface{}{},
		},
	})
	if err == nil {
		t.Fatal("expected workflow without tasks to be rejected")
	}
	if !strings.Contains(err.Error(), "workflow_definition.tasks") {
		t.Fatalf("expected tasks error, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsWorkflowTaskWithoutParams(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":         "task1",
					"operator":   "load",
					"depends_on": []interface{}{},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected workflow task without params to be rejected")
	}
	if !strings.Contains(err.Error(), "tasks[0].params") {
		t.Fatalf("expected params error, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsWorkflowTaskWithoutDependsOn(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":       "task1",
					"operator": "load",
					"params":   map[string]interface{}{},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected workflow task without depends_on to be rejected")
	}
	if !strings.Contains(err.Error(), "tasks[0].depends_on") {
		t.Fatalf("expected depends_on error, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsWorkflowTaskWithInvalidDependsOn(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":         "task1",
					"operator":   "load",
					"params":     map[string]interface{}{},
					"depends_on": "task0",
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected workflow task with non-array depends_on to be rejected")
	}
	if !strings.Contains(err.Error(), "depends_on") || !strings.Contains(err.Error(), "字符串数组") {
		t.Fatalf("expected depends_on array error, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsWorkflowUnknownDependency(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":         "task1",
					"operator":   "load",
					"params":     map[string]interface{}{},
					"depends_on": []interface{}{"missing"},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected workflow task with unknown dependency to be rejected")
	}
	if !strings.Contains(err.Error(), "依赖不存在的任务") {
		t.Fatalf("expected missing dependency error, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsWorkflowDependencyCycle(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"id":         "task1",
					"operator":   "load",
					"params":     map[string]interface{}{},
					"depends_on": []interface{}{"task2"},
				},
				map[string]interface{}{
					"id":         "task2",
					"operator":   "save",
					"params":     map[string]interface{}{},
					"depends_on": []interface{}{"task1"},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected workflow dependency cycle to be rejected")
	}
	if !strings.Contains(err.Error(), "循环依赖") {
		t.Fatalf("expected cycle dependency error, got %v", err)
	}
}

func TestDevTaskExecutionRecordConfigUsesDuckDBRuntimeEngineID(t *testing.T) {
	config := devTaskExecutionRecordConfig(
		&models.DevTask{
			DevType: commonExecution.TaskTypeQuery,
			Content: models.DevTaskContent{
				"query":      "SELECT 1",
				"query_type": "sql",
			},
			ExecutionConfig: models.DevTaskContent{"engine_id": 9},
		},
		map[string]interface{}{},
	)

	engineID, ok := config["engine_id"].(*uint)
	if !ok || engineID == nil || *engineID != 9 {
		t.Fatalf("engine_id = %#v, want *uint(9)", config["engine_id"])
	}
}

func TestDevTaskExecutionRecordConfigUsesEngineIDForNormalQuery(t *testing.T) {
	config := devTaskExecutionRecordConfig(
		&models.DevTask{
			DevType: commonExecution.TaskTypeQuery,
			Content: models.DevTaskContent{
				"query":      "SELECT 1",
				"query_type": "sql",
			},
			ExecutionConfig: models.DevTaskContent{
				"engine_id": 7,
			},
		},
		map[string]interface{}{},
	)

	engineID, ok := config["engine_id"].(*uint)
	if !ok || engineID == nil || *engineID != 7 {
		t.Fatalf("engine_id = %#v, want *uint(7)", config["engine_id"])
	}
	if _, ok := config["query_mode"]; ok {
		t.Fatalf("normal query execution record must not include query_mode: %#v", config)
	}
}

func TestValidateDevTaskContentRejectsLegacyWorkflowDef(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_def": map[string]interface{}{
			"nodes": []interface{}{},
			"edges": []interface{}{},
		},
	})
	if err == nil {
		t.Fatal("expected legacy content.workflow_def to be rejected")
	}
	if !strings.Contains(err.Error(), "content.workflow_definition") {
		t.Fatalf("expected content.workflow_definition error, got %v", err)
	}
}

func TestValidateExpectedDevTaskTypeAllowsInternalExecution(t *testing.T) {
	err := validateExpectedDevTaskType(commonExecution.TaskTypeWorkflow, "")
	if err != nil {
		t.Fatalf("expected empty expected task type to pass, got %v", err)
	}
}

func TestValidateExpectedDevTaskTypeRejectsProviderMismatch(t *testing.T) {
	err := validateExpectedDevTaskType(commonExecution.TaskTypeWorkflow, commonExecution.TaskTypeQuery)
	if err == nil {
		t.Fatal("expected task_type/dev_type mismatch to be rejected")
	}
	if !strings.Contains(err.Error(), "task_type=query") || !strings.Contains(err.Error(), "dev_type=workflow") {
		t.Fatalf("unexpected mismatch error: %v", err)
	}
}

func TestWorkflowTaskDefinitionSchemaRequiresCanonicalTasks(t *testing.T) {
	schema := workflowTaskDefinitionSchema()
	properties := schema["properties"].(map[string]interface{})
	content := properties["content"].(map[string]interface{})
	contentProperties := content["properties"].(map[string]interface{})
	workflowDefinition := contentProperties["workflow_definition"].(map[string]interface{})

	required := workflowDefinition["required"].([]interface{})
	if !containsStringItem(required, "tasks") {
		t.Fatalf("workflow_definition required = %#v, want tasks", required)
	}

	workflowProperties := workflowDefinition["properties"].(map[string]interface{})
	tasks := workflowProperties["tasks"].(map[string]interface{})
	taskItem := tasks["items"].(map[string]interface{})
	taskRequired := taskItem["required"].([]interface{})
	for _, field := range []string{"id", "operator", "params", "depends_on"} {
		if !containsStringItem(taskRequired, field) {
			t.Fatalf("task required = %#v, want %s", taskRequired, field)
		}
	}
}

func containsStringItem(items []interface{}, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
