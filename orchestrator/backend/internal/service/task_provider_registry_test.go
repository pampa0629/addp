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
