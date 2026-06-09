package service

import (
	"context"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
)

func TestValidateStepTaskTypesAcceptsDeclaredTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_types":[{"type":"scan","deprecated":false}]
		}`),
	})

	err := registry.ValidateStepTaskTypes(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err != nil {
		t.Fatalf("ValidateStepTaskTypes() error = %v, want nil", err)
	}
}

func TestValidateStepTaskTypesRejectsParametersDisallowedByExecutionSchema(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_types":[{
				"type":"scan",
				"deprecated":false,
				"execution_schema":{"type":"object","additionalProperties":false}
			}]
		}`),
	})

	err := registry.ValidateStepTaskTypes(context.Background(), models.Steps{{
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

func TestValidateStepTaskTypesAcceptsDeclaredExecutionSchemaParameters(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"develop": taskProviderForTest("develop", `{
			"schema_version":"task.capabilities/v1",
			"task_types":[{
				"type":"query",
				"deprecated":false,
				"execution_schema":{
					"type":"object",
					"properties":{"limit":{"type":"integer"}},
					"additionalProperties":false
				}
			}]
		}`),
	})

	err := registry.ValidateStepTaskTypes(context.Background(), models.Steps{{
		ID: "query", Name: "Query", Provider: "develop", TaskType: "query", TaskID: 1,
		Parameters: map[string]interface{}{"limit": 100},
	}})

	if err != nil {
		t.Fatalf("ValidateStepTaskTypes() error = %v, want nil", err)
	}
}

func TestValidateStepTaskTypesValidatesParametersForRepeatedTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{
			"schema_version":"task.capabilities/v1",
			"task_types":[{
				"type":"scan",
				"deprecated":false,
				"execution_schema":{"type":"object","additionalProperties":false}
			}]
		}`),
	})

	err := registry.ValidateStepTaskTypes(context.Background(), models.Steps{
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

func TestValidateStepTaskTypesRejectsUndeclaredTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"manager": taskProviderForTest("manager", `{
			"schema_version":"task.capabilities/v1",
			"task_types":[{"type":"mvt_generation","deprecated":false}]
		}`),
	})

	err := registry.ValidateStepTaskTypes(context.Background(), models.Steps{{
		ID: "embedding", Name: "Embedding", Provider: "manager", TaskType: "embedding", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected undeclared task_type to be rejected")
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("error = %q, want not declared", err.Error())
	}
}

func TestValidateStepTaskTypesRejectsDeprecatedTaskType(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"develop": taskProviderForTest("develop", `{
			"schema_version":"task.capabilities/v1",
			"task_types":[{"type":"workflow","deprecated":true}]
		}`),
	})

	err := registry.ValidateStepTaskTypes(context.Background(), models.Steps{{
		ID: "workflow", Name: "Workflow", Provider: "develop", TaskType: "workflow", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected deprecated task_type to be rejected")
	}
	if !strings.Contains(err.Error(), "deprecated") {
		t.Fatalf("error = %q, want deprecated", err.Error())
	}
}

func TestValidateStepTaskTypesRejectsInvalidCapabilities(t *testing.T) {
	registry := taskProviderRegistryForTest(map[string]*commonModels.TaskProvider{
		"meta": taskProviderForTest("meta", `{"schema_version":"legacy"}`),
	})

	err := registry.ValidateStepTaskTypes(context.Background(), models.Steps{{
		ID: "scan", Name: "Scan", Provider: "meta", TaskType: "scan", TaskID: 1,
	}})

	if err == nil {
		t.Fatal("expected invalid capabilities to be rejected")
	}
	if !strings.Contains(err.Error(), "capabilities invalid") {
		t.Fatalf("error = %q, want capabilities invalid", err.Error())
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
