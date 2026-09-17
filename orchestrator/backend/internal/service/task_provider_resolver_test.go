package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/orchestrator/internal/models"
)

func TestValidateStepParametersUsesDefaultsWithoutMutatingOverrides(t *testing.T) {
	contract := &taskprovider.ExecutionContract{
		InputSchema: map[string]interface{}{
			"type": "object", "additionalProperties": false,
			"properties": map[string]interface{}{"version": map[string]interface{}{"type": "integer", "minimum": 1}},
			"required":   []interface{}{"version"},
		},
		InputDefaults: map[string]interface{}{"version": 3},
	}
	for _, allowTemplates := range []bool{true, false} {
		for _, tc := range []struct {
			name       string
			parameters map[string]interface{}
			wantError  bool
		}{
			{"owner default", nil, false},
			{"explicit version", map[string]interface{}{"version": 4}, false},
			{"explicit zero", map[string]interface{}{"version": 0}, true},
			{"explicit null", map[string]interface{}{"version": nil}, true},
			{"wrong type", map[string]interface{}{"version": "3"}, true},
			{"unknown field", map[string]interface{}{"other": 3}, true},
			{"upstream reference", map[string]interface{}{"version": "{{source.outputs.version}}"}, !allowTemplates},
		} {
			t.Run(fmt.Sprintf("%s/templates=%t", tc.name, allowTemplates), func(t *testing.T) {
				before := fmt.Sprintf("%#v", tc.parameters)
				err := validateStepParametersByExecutionContract(models.Step{Parameters: tc.parameters}, contract, allowTemplates)
				if (err != nil) != tc.wantError {
					t.Fatalf("error = %v, want error %t", err, tc.wantError)
				}
				if fmt.Sprintf("%#v", tc.parameters) != before || contract.InputDefaults["version"] != 3 {
					t.Fatal("validation mutated inputs")
				}
			})
		}
	}
	contract.InputDefaults = nil
	if err := validateStepParametersByExecutionContract(models.Step{}, contract, true); err == nil {
		t.Fatal("missing required input without a default must fail")
	}
}

func TestValidateStepParametersDoesNotRepairExplicitObjectWithDefaults(t *testing.T) {
	contract := &taskprovider.ExecutionContract{
		InputSchema: map[string]interface{}{
			"type": "object", "additionalProperties": false,
			"properties": map[string]interface{}{"resource": map[string]interface{}{
				"type": "object", "properties": map[string]interface{}{"locator": map[string]interface{}{"type": "string"}},
				"required": []interface{}{"locator"}, "additionalProperties": false,
			}},
			"required": []interface{}{"resource"},
		},
		InputDefaults: map[string]interface{}{"resource": map[string]interface{}{"locator": "saved-table"}},
	}
	if err := validateStepParametersByExecutionContract(models.Step{}, contract, false); err != nil {
		t.Fatal(err)
	}
	step := models.Step{Parameters: map[string]interface{}{"resource": map[string]interface{}{}}}
	if err := validateStepParametersByExecutionContract(step, contract, false); err == nil {
		t.Fatal("explicit incomplete resource must fail")
	}
}

func TestValidateStepTaskReferencesUsesConcreteTaskExecutionContract(t *testing.T) {
	registry := taskProviderResolverForTest(t, map[uint]string{
		1: executionContractJSON(`{"type":"object","properties":{"limit":{"type":"integer"}},"additionalProperties":false}`),
	})
	err := registry.ValidateStepTaskReferences(context.Background(), 7, models.Steps{{
		ID: "query", Name: "Query", Provider: "develop", TaskType: "workflow", TaskID: 1,
		Parameters: map[string]interface{}{"limit": 100},
	}})
	if err != nil {
		t.Fatalf("ValidateStepTaskReferences() error = %v", err)
	}
}

func TestValidateStepTaskReferencesAcceptsRequiredOwnerDefault(t *testing.T) {
	registry := taskProviderResolverForTest(t, map[uint]string{
		1: `{"input_schema":{"type":"object","properties":{"version":{"type":"integer","minimum":1}},"required":["version"],"additionalProperties":false},"input_defaults":{"version":3},"input_ui_schema":{},"output_schema":{"type":"object","additionalProperties":false}}`,
	})
	steps := models.Steps{{ID: "model", Name: "Model", Provider: "develop", TaskType: "workflow", TaskID: 1}}
	if err := registry.ValidateStepTaskReferences(context.Background(), 7, steps); err != nil {
		t.Fatalf("owner default must satisfy required input: %v", err)
	}
	if steps[0].Parameters != nil {
		t.Fatal("validation must not freeze current owner defaults into the orchestration")
	}
}

func TestGetProviderRejectsOfflineDeclarationWithStableError(t *testing.T) {
	registry := &TaskProviderResolver{
		loadProvider: func(context.Context, string) (*commonModels.TaskProvider, error) {
			return &commonModels.TaskProvider{ModuleName: "develop", Enabled: true, Available: false}, nil
		},
	}

	_, err := registry.GetProvider(context.Background(), "develop")
	if !errors.Is(err, ErrTaskProviderUnavailable) {
		t.Fatalf("GetProvider() error = %v, want ErrTaskProviderUnavailable", err)
	}
}

func TestValidateStepTaskReferencesRejectsParameterOutsideConcreteContract(t *testing.T) {
	registry := taskProviderResolverForTest(t, map[uint]string{
		1: executionContractJSON(`{"type":"object","additionalProperties":false}`),
	})
	err := registry.ValidateStepTaskReferences(context.Background(), 7, models.Steps{{
		ID: "query", Name: "Query", Provider: "develop", TaskType: "workflow", TaskID: 1,
		Parameters: map[string]interface{}{"force": true},
	}})
	if err == nil || !strings.Contains(err.Error(), "parameters.force is not allowed") {
		t.Fatalf("error = %v, want force rejection", err)
	}
	var stepErr *StepTaskValidationError
	if !errors.As(err, &stepErr) || stepErr.Code != StepTaskParametersInvalid || stepErr.StepIndex != 0 {
		t.Fatalf("step error = %#v", stepErr)
	}
}

func TestValidateStepTaskReferencesSeparatesContractsForSameTaskType(t *testing.T) {
	registry := taskProviderResolverForTest(t, map[uint]string{
		1: executionContractJSON(`{"type":"object","properties":{"limit":{"type":"integer"}},"additionalProperties":false}`),
		2: executionContractJSON(`{"type":"object","properties":{"distance":{"type":"number"}},"additionalProperties":false}`),
	})
	err := registry.ValidateStepTaskReferences(context.Background(), 7, models.Steps{
		{ID: "first", Name: "First", Provider: "develop", TaskType: "workflow", TaskID: 1, Parameters: map[string]interface{}{"limit": 10}},
		{ID: "second", Name: "Second", Provider: "develop", TaskType: "workflow", TaskID: 2, Parameters: map[string]interface{}{"limit": 10}},
	})
	if err == nil || !strings.Contains(err.Error(), "steps[1]") || !strings.Contains(err.Error(), "parameters.limit") {
		t.Fatalf("error = %v, want task 2 contract rejection", err)
	}
}

func TestValidateStepTaskReferencesAcceptsOnlyDeclaredTypeCompatibleOutputs(t *testing.T) {
	registry := taskProviderResolverForTest(t, map[uint]string{
		1: `{
			"input_schema":{"type":"object","additionalProperties":false},
			"input_defaults":{},"input_ui_schema":{},
			"output_schema":{"type":"object","properties":{"save_3":{"type":"object","properties":{"resource":{"type":"object","properties":{"locator":{"type":"string"}},"additionalProperties":false}},"additionalProperties":false}},"additionalProperties":false}
		}`,
		2: executionContractJSON(`{"type":"object","properties":{"load_1":{"type":"object","properties":{"source_resource":{"type":"object","properties":{"locator":{"type":"string"}},"additionalProperties":false}},"additionalProperties":false}},"additionalProperties":false}`),
	})
	steps := models.Steps{
		{ID: "produce", Name: "Produce", Provider: "develop", TaskType: "workflow", TaskID: 1},
		{ID: "consume", Name: "Consume", Provider: "develop", TaskType: "workflow", TaskID: 2, DependsOn: []string{"produce"}, Parameters: map[string]interface{}{
			"load_1": map[string]interface{}{
				"source_resource": map[string]interface{}{"locator": "{{produce.outputs.save_3.resource.locator}}"},
			},
		}},
	}
	if err := registry.ValidateStepTaskReferences(context.Background(), 7, steps); err != nil {
		t.Fatalf("declared output binding error = %v", err)
	}

	steps[1].Parameters["load_1"].(map[string]interface{})["source_resource"].(map[string]interface{})["locator"] = "{{produce.metadata.result.produced_targets.0.locator}}"
	err := registry.ValidateStepTaskReferences(context.Background(), 7, steps)
	if err == nil || !strings.Contains(err.Error(), "declared output") {
		t.Fatalf("arbitrary output path error = %v", err)
	}
}

func TestGetTaskExecutionContractRequiresValidTaskDetailContract(t *testing.T) {
	registry := taskProviderResolverForTest(t, map[uint]string{
		1: `{"input_schema":{"type":"object"}}`,
	})
	provider, err := registry.GetProvider(context.Background(), "develop")
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.GetTaskExecutionContract(context.Background(), provider, "workflow", 1, 7)
	if err == nil || !strings.Contains(err.Error(), "execution_contract") {
		t.Fatalf("error = %v, want invalid execution_contract", err)
	}
}

func TestValidateStepTaskReferencesRejectsUndeclaredAndDeprecatedTaskTypesBeforeDetailCall(t *testing.T) {
	registry := taskProviderResolverForTest(t, map[uint]string{})
	provider, err := registry.GetProvider(context.Background(), "develop")
	if err != nil {
		t.Fatal(err)
	}
	provider.Capabilities = capabilityJSONForTest("query", false)
	err = registry.ValidateStepTaskReferences(context.Background(), 7, models.Steps{{
		ID: "workflow", Name: "Workflow", Provider: "develop", TaskType: "workflow", TaskID: 1,
	}})
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("undeclared error = %v", err)
	}

	provider.Capabilities = capabilityJSONForTest("workflow", true)
	err = registry.ValidateStepTaskReferences(context.Background(), 7, models.Steps{{
		ID: "workflow", Name: "Workflow", Provider: "develop", TaskType: "workflow", TaskID: 1,
	}})
	if err == nil || !strings.Contains(err.Error(), "deprecated") {
		t.Fatalf("deprecated error = %v", err)
	}
}

func taskProviderResolverForTest(t *testing.T, contracts map[uint]string) *TaskProviderResolver {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer tenant-token" {
			http.Error(response, "missing token", http.StatusUnauthorized)
			return
		}
		var taskID uint
		if _, err := fmt.Sscanf(request.URL.Path, "/tasks/workflow/%d", &taskID); err != nil {
			http.NotFound(response, request)
			return
		}
		contract, exists := contracts[taskID]
		if !exists {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(response, `{"id":%d,"task_type":"workflow","execution_contract":%s}`, taskID, contract)
	}))
	t.Cleanup(server.Close)
	tokenSource := registrationServiceTokens("tenant-token")
	systemClient := commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client())
	provider := &commonModels.TaskProvider{
		ModuleName: "develop", Backends: taskProviderBackendsForTest(server.URL), Available: true, Enabled: true,
		TaskProviderDeclaration: commonModels.TaskProviderDeclaration{
			TaskDetailEndpoint: "/tasks/{task_type}/{id}", Capabilities: capabilityJSONForTest("workflow", false),
		},
	}
	return &TaskProviderResolver{
		systemClient: systemClient, httpClient: server.Client(),
		loadProvider: func(context.Context, string) (*commonModels.TaskProvider, error) { return provider, nil },
	}
}

func taskProviderBackendsForTest(baseURLs ...string) []commonModels.TaskProviderBackend {
	backends := make([]commonModels.TaskProviderBackend, 0, len(baseURLs))
	for index, baseURL := range baseURLs {
		backends = append(backends, commonModels.TaskProviderBackend{
			InstanceID: fmt.Sprintf("backend-%d", index+1), BaseURL: baseURL, LeaseExpiresAt: time.Now().Add(time.Hour),
		})
	}
	return backends
}

func TestTaskProviderResolverRoundRobinsCurrentBackendPool(t *testing.T) {
	provider := &commonModels.TaskProvider{
		ModuleName: "meta", Available: true, Backends: taskProviderBackendsForTest("http://meta-a:8082", "http://meta-b:8082"),
	}
	resolver := &TaskProviderResolver{
		loadProvider: func(context.Context, string) (*commonModels.TaskProvider, error) { return provider, nil },
	}

	first, err := resolver.GetProvider(context.Background(), "meta")
	if err != nil {
		t.Fatal(err)
	}
	firstURL := first.ResolvedBaseURL
	second, err := resolver.GetProvider(context.Background(), "meta")
	if err != nil {
		t.Fatal(err)
	}
	if firstURL != "http://meta-a:8082" || second.ResolvedBaseURL != "http://meta-b:8082" {
		t.Fatalf("resolved URLs = %q, %q", firstURL, second.ResolvedBaseURL)
	}
}

func capabilityJSONForTest(taskType string, deprecated bool) *commonModels.JSONString {
	raw := commonModels.JSONString(fmt.Sprintf(`{
		"schema_version":"task.capabilities/v2",
		"task_capabilities":[{
			"type":%q,"display_name":%q,"description":"task",
			"definition_schema":{"type":"object"},
			"supports_schedule":false,"supports_cancel":false,"supports_inline_execution":false,
			"create_url":"/tasks/create","edit_url":"/tasks/edit?id=:id","deprecated":%t
		}]
	}`, taskType, taskType, deprecated))
	return &raw
}

func executionContractJSON(inputSchema string) string {
	return `{
		"input_schema":` + inputSchema + `,
		"input_defaults":{},
		"input_ui_schema":{},
		"output_schema":{"type":"object","additionalProperties":false}
	}`
}

func taskCapabilitiesForTest(taskType string, deprecated bool, _ string) string {
	return string(*capabilityJSONForTest(taskType, deprecated))
}
