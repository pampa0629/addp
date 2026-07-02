package service

import (
	"context"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
)

func TestValidateStepTaskReferencesAcceptsDeclaredTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", taskCapabilitiesForTest("scan", false, `{"type":"object","additionalProperties":false}`)),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err != nil {
		t.Fatalf("ValidateStepTaskReferences() error = %v, want nil", err)
	}
}

func TestValidateStepTaskReferencesRejectsParametersDisallowedByExecutionSchema(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", taskCapabilitiesForTest("scan", false, `{"type":"object","additionalProperties":false}`)),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
		Parameters: map[string]interface{}{"force": true},
	}})

	if err == nil {
		t.Fatal("expected disallowed parameters to be rejected")
	}
	if !strings.Contains(err.Error(), "parameters.force is not allowed") {
		t.Fatalf("error = %q, want parameters.force is not allowed", err.Error())
	}
}

func TestValidateStepTaskReferencesAcceptsDeclaredExecutionSchemaParameters(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"develop": taskProviderForTest("develop", taskCapabilitiesForTest("query", false, `{"type":"object","properties":{"limit":{"type":"integer"}},"additionalProperties":false}`)),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "query", Name: "Query", Provider: "develop", TaskType: "query", TaskID: 1,
		Parameters: map[string]interface{}{"limit": 100},
	}})

	if err != nil {
		t.Fatalf("ValidateStepTaskReferences() error = %v, want nil", err)
	}
}

func TestValidateStepTaskReferencesValidatesParametersForRepeatedTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", taskCapabilitiesForTest("scan", false, `{"type":"object","additionalProperties":false}`)),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{
		{ID: "scan_a", Name: "Scan A", Provider: "meta", TaskType: "scan", TaskID: 1},
		{
			ID: "scan_b", Name: "Scan B", Provider: "meta", TaskType: "scan", TaskID: 2,
			Parameters: map[string]interface{}{"force": true},
		},
	})

	if err == nil {
		t.Fatal("expected second step parameters to be rejected")
	}
	if !strings.Contains(err.Error(), "steps[1]") || !strings.Contains(err.Error(), "parameters.force is not allowed") {
		t.Fatalf("error = %q, want steps[1] parameters.force rejection", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsUndeclaredTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"manager": taskProviderForTest("manager", taskCapabilitiesForTest("vector_tile_cache_generation", false, `{"type":"object","additionalProperties":false}`)),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "embedding", Name: "Embedding", Provider: "manager", TaskType: "embedding", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected undeclared task_type to be rejected")
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("error = %q, want not declared", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsDeprecatedTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"develop": taskProviderForTest("develop", taskCapabilitiesForTest("workflow", true, `{"type":"object","additionalProperties":false}`)),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "workflow", Name: "Workflow", Provider: "develop", TaskType: "workflow", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected deprecated task_type to be rejected")
	}
	if !strings.Contains(err.Error(), "deprecated") {
		t.Fatalf("error = %q, want deprecated", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsInvalidCapabilities(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{"schema_version":"legacy"}`),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected invalid capabilities to be rejected")
	}
	if !strings.Contains(err.Error(), "capabilities invalid") {
		t.Fatalf("error = %q, want capabilities invalid", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsMissingExecutionSchema(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_capabilities":[{
				"type":"scan",
				"display_name":"scan",
				"description":"scan task",
				"definition_schema":{"type":"object"},
				"supports_schedule":false,
				"supports_cancel":false,
				"supports_inline_execution":false,
				"create_url":"/meta/scan",
				"edit_url":"/meta/scan?task_id=:id",
				"deprecated":false
			}]
		}`),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected missing execution_schema to be rejected")
	}
	if !strings.Contains(err.Error(), "execution_schema is required") {
		t.Fatalf("error = %q, want execution_schema is required", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsDuplicateTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_capabilities":[
				{
					"type":"scan",
					"display_name":"scan",
					"description":"scan task",
					"definition_schema":{"type":"object"},
					"execution_schema":{"type":"object"},
					"supports_schedule":false,
					"supports_cancel":false,
					"supports_inline_execution":false,
					"create_url":"/meta/scan",
					"edit_url":"/meta/scan?task_id=:id",
					"deprecated":false
				},
				{
					"type":"scan",
					"display_name":"scan",
					"description":"scan task",
					"definition_schema":{"type":"object"},
					"execution_schema":{"type":"object"},
					"supports_schedule":false,
					"supports_cancel":false,
					"supports_inline_execution":false,
					"create_url":"/meta/scan",
					"edit_url":"/meta/scan?task_id=:id",
					"deprecated":false
				}
			]
		}`),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected duplicate task_type to be rejected")
	}
	if !strings.Contains(err.Error(), `duplicate task_type "scan"`) {
		t.Fatalf("error = %q, want duplicate task_type", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsInvalidTaskTypeName(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_capabilities":[{
				"type":"Scan",
				"display_name":"scan",
				"description":"scan task",
				"definition_schema":{"type":"object"},
				"execution_schema":{"type":"object"},
				"supports_schedule":false,
				"supports_cancel":false,
				"supports_inline_execution":false,
				"create_url":"/meta/scan",
				"edit_url":"/meta/scan?task_id=:id",
				"deprecated":false
			}]
		}`),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected invalid task_type name to be rejected")
	}
	if !strings.Contains(err.Error(), "type must match") {
		t.Fatalf("error = %q, want type must match", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsDeprecatedNonBoolean(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_capabilities":[{
				"type":"scan",
				"display_name":"scan",
				"description":"scan task",
				"definition_schema":{"type":"object"},
				"execution_schema":{"type":"object"},
				"supports_schedule":false,
				"supports_cancel":false,
				"supports_inline_execution":false,
				"create_url":"/meta/scan",
				"edit_url":"/meta/scan?task_id=:id",
				"deprecated":"false"
			}]
		}`),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected non-boolean deprecated to be rejected")
	}
	if !strings.Contains(err.Error(), "deprecated must be boolean") {
		t.Fatalf("error = %q, want deprecated must be boolean", err.Error())
	}
}

func TestValidateStepTaskReferencesRejectsExecutionSchemaTypeNotObject(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_capabilities":[{
				"type":"scan",
				"display_name":"scan",
				"description":"scan task",
				"definition_schema":{"type":"object"},
				"execution_schema":{"type":"array"},
				"supports_schedule":false,
				"supports_cancel":false,
				"supports_inline_execution":false,
				"create_url":"/meta/scan",
				"edit_url":"/meta/scan?task_id=:id",
				"deprecated":false
			}]
		}`),
	})

	err := registry.ValidateStepTaskReferences(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected execution_schema.type != object to be rejected")
	}
	if !strings.Contains(err.Error(), "execution_schema.type must be object") {
		t.Fatalf("error = %q, want execution_schema.type must be object", err.Error())
	}
}

func taskProviderRegistryForTest(providers map[string]*commonModels.TaskProvider) *TaskProviderRegistry {
	return &TaskProviderRegistry{
		providers:   providers,
		cacheTTL:    time.Hour,
		lastRefresh: time.Now(),
	}
}

func taskProviderForTest(moduleName string, capabilities string) *commonModels.TaskProvider {
	capabilitiesJSON := commonModels.JSONString(capabilities)
	return &commonModels.TaskProvider{
		ModuleName:   moduleName,
		BaseURL:      "http://example.invalid",
		Capabilities: &capabilitiesJSON,
		IsEnabled:    true,
	}
}

func taskCapabilitiesForTest(taskType string, deprecated bool, executionSchema string) string {
	return `{
		"schema_version":"task.capabilities/v1",
		"task_capabilities":[{
			"type":"` + taskType + `",
			"display_name":"` + taskType + `",
			"description":"` + taskType + ` task",
			"definition_schema":{"type":"object"},
			"execution_schema":` + executionSchema + `,
			"supports_schedule":false,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/` + taskType + `/tasks",
			"edit_url":"/` + taskType + `/tasks?task_id=:id",
			"deprecated":` + boolJSON(deprecated) + `
		}]
	}`
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
