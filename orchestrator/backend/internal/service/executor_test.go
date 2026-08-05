package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/orchestrator/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestResolveTemplateReferences 测试参数模板解析
func TestResolveTemplateReferences(t *testing.T) {
	executor := &Executor{}

	// 模拟步骤结果
	stepResults := models.StepResults{
		"sql_extract": {
			Status: "success",
			Result: map[string]interface{}{
				"outputs": map[string]interface{}{
					"result_table": "temp_table_123",
					"row_count":    100,
					"geojson": map[string]interface{}{
						"type": "FeatureCollection",
						"features": []interface{}{
							map[string]interface{}{
								"type": "Feature",
								"geometry": map[string]interface{}{
									"type":        "Point",
									"coordinates": []interface{}{116.404, 39.915},
								},
							},
						},
					},
				},
			},
		},
		"spatial_analysis": {
			Status: "success",
			Result: map[string]interface{}{
				"outputs": map[string]interface{}{
					"geojson": map[string]interface{}{
						"type": "FeatureCollection",
						"features": []interface{}{
							map[string]interface{}{
								"type": "Feature",
								"geometry": map[string]interface{}{
									"type":        "Polygon",
									"coordinates": []interface{}{},
								},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		params   map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "Simple field reference",
			params: map[string]interface{}{
				"table_name": "{{sql_extract.outputs.result_table}}",
			},
			expected: map[string]interface{}{
				"table_name": "temp_table_123",
			},
		},
		{
			name: "Nested field reference",
			params: map[string]interface{}{
				"input_geojson": "{{sql_extract.outputs.geojson}}",
			},
			expected: map[string]interface{}{
				"input_geojson": map[string]interface{}{
					"type": "FeatureCollection",
					"features": []interface{}{
						map[string]interface{}{
							"type": "Feature",
							"geometry": map[string]interface{}{
								"type":        "Point",
								"coordinates": []interface{}{116.404, 39.915},
							},
						},
					},
				},
			},
		},
		{
			name: "Multiple references",
			params: map[string]interface{}{
				"source_table":   "{{sql_extract.outputs.result_table}}",
				"result_geojson": "{{spatial_analysis.outputs.geojson}}",
				"row_count":      "{{sql_extract.outputs.row_count}}",
			},
			expected: map[string]interface{}{
				"source_table": "temp_table_123",
				"result_geojson": map[string]interface{}{
					"type": "FeatureCollection",
					"features": []interface{}{
						map[string]interface{}{
							"type": "Feature",
							"geometry": map[string]interface{}{
								"type":        "Polygon",
								"coordinates": []interface{}{},
							},
						},
					},
				},
				"row_count": 100,
			},
		},
		{
			name: "Mixed with static values",
			params: map[string]interface{}{
				"dynamic_table": "{{sql_extract.outputs.result_table}}",
				"static_value":  "fixed_string",
				"numeric_value": 42,
			},
			expected: map[string]interface{}{
				"dynamic_table": "temp_table_123",
				"static_value":  "fixed_string",
				"numeric_value": 42,
			},
		},
		{
			name: "Nested map with references",
			params: map[string]interface{}{
				"config": map[string]interface{}{
					"table":  "{{sql_extract.outputs.result_table}}",
					"format": "geojson",
					"data":   "{{sql_extract.outputs.geojson}}",
				},
			},
			expected: map[string]interface{}{
				"config": map[string]interface{}{
					"table":  "temp_table_123",
					"format": "geojson",
					"data": map[string]interface{}{
						"type": "FeatureCollection",
						"features": []interface{}{
							map[string]interface{}{
								"type": "Feature",
								"geometry": map[string]interface{}{
									"type":        "Point",
									"coordinates": []interface{}{116.404, 39.915},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Array with references",
			params: map[string]interface{}{
				"tables": []interface{}{
					"{{sql_extract.outputs.result_table}}",
					"static_table",
				},
			},
			expected: map[string]interface{}{
				"tables": []interface{}{
					"temp_table_123",
					"static_table",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := executor.resolveTemplateReferences(tt.params, stepResults)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, resolved)
		})
	}
}

func TestResolveTemplateReferencesRejectsMissingPath(t *testing.T) {
	executor := &Executor{}
	stepResults := models.StepResults{
		"sql_extract": {
			Status: "success",
			Result: map[string]interface{}{
				"outputs": map[string]interface{}{"result_table": "temp_table_123"},
			},
		},
	}

	tests := []struct {
		name       string
		params     map[string]interface{}
		wantErrMsg string
	}{
		{
			name: "Non-existent step reference",
			params: map[string]interface{}{
				"value": "{{nonexistent_step.outputs.field}}",
			},
			wantErrMsg: `referenced step "nonexistent_step" has no result`,
		},
		{
			name: "Non-existent field reference",
			params: map[string]interface{}{
				"value": "{{sql_extract.outputs.nonexistent_field}}",
			},
			wantErrMsg: `path "sql_extract.outputs.nonexistent_field" is missing`,
		},
		{
			name: "Nested missing field reference",
			params: map[string]interface{}{
				"config": map[string]interface{}{
					"value": "{{sql_extract.outputs.missing}}",
				},
			},
			wantErrMsg: `parameters.config: value: path "sql_extract.outputs.missing" is missing`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.resolveTemplateReferences(tt.params, stepResults)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

func TestExecuteWithTaskProviderUsesTenantServiceBearer(t *testing.T) {
	executeAuthorization := ""
	statusAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/quality/tasks/check/42/execute":
			executeAuthorization = r.Header.Get("Authorization")
			if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
				t.Fatal("legacy internal headers must not be sent")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"execution_id": "child-exec"})
		case "/api/v1/quality/executions/child-exec":
			statusAuthorization = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"execution_id": "child-exec",
				"status":       "success",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executor := &Executor{
		taskProviderRegistry: &TaskProviderRegistry{
			providers: map[string]*commonModels.TaskProvider{
				"quality": {
					ModuleName:          "quality",
					BaseURL:             server.URL,
					TaskExecuteEndpoint: "/api/v1/quality/tasks/{task_type}/{id}/execute",
					TaskStatusEndpoint:  "/api/v1/quality/executions/{execution_id}",
					Capabilities:        jsonStringPtr(taskCapabilitiesForTest("check", false, `{"type":"object","additionalProperties":false}`)),
				},
			},
			cacheTTL:              time.Hour,
			lastRefresh:           time.Now(),
			loadExecutionContract: executionContractLoaderForTest(`{"type":"object","additionalProperties":false}`),
		},
		serviceTokens: commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "addp_at_orchestrator", nil
		}),
	}

	result, err := executor.executeWithTaskProvider(
		context.Background(),
		&models.Step{Provider: "quality", TaskType: "check", TaskID: 42, Timeout: 10},
		map[string]interface{}{},
		time.Now(),
		"parent-exec",
		"manual",
		7,
	)

	assert.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "Bearer addp_at_orchestrator", executeAuthorization)
	assert.Equal(t, "Bearer addp_at_orchestrator", statusAuthorization)
}

func TestExecuteWithTaskProviderForwardsScheduledExistingResultActionWithoutOwnerScheduleFields(t *testing.T) {
	var executePayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/manager/tasks/vector_tile_cache_generation/42/execute":
			if err := json.NewDecoder(r.Body).Decode(&executePayload); err != nil {
				t.Fatalf("decode execute payload: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"execution_id": "child-exec"})
		case "/api/v1/manager/executions/child-exec":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"execution_id": "child-exec",
				"status":       "success",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	executor := &Executor{
		taskProviderRegistry: &TaskProviderRegistry{
			providers: map[string]*commonModels.TaskProvider{
				"manager": {
					ModuleName:          "manager",
					BaseURL:             server.URL,
					TaskExecuteEndpoint: "/api/v1/manager/tasks/{task_type}/{id}/execute",
					TaskStatusEndpoint:  "/api/v1/manager/executions/{execution_id}",
					Capabilities: jsonStringPtr(taskCapabilitiesForTest(
						"vector_tile_cache_generation",
						false,
						`{"type":"object","properties":{"existing_result_action":{"type":"string","enum":["overwrite"]}},"additionalProperties":false}`,
					)),
				},
			},
			cacheTTL:              time.Hour,
			lastRefresh:           time.Now(),
			loadExecutionContract: executionContractLoaderForTest(`{"type":"object","properties":{"existing_result_action":{"type":"string","enum":["overwrite"]}},"additionalProperties":false}`),
		},
		serviceTokens: commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
			return "addp_at_orchestrator", nil
		}),
	}

	result, err := executor.executeWithTaskProvider(
		context.Background(),
		&models.Step{Provider: "manager", TaskType: "vector_tile_cache_generation", TaskID: 42, Timeout: 10},
		map[string]interface{}{"existing_result_action": "overwrite"},
		time.Now(),
		"parent-exec",
		"scheduled",
		7,
	)

	assert.NoError(t, err)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "scheduled", executePayload["trigger_type"])
	assert.Equal(t, "orchestrator", executePayload["source"])
	assert.Equal(t, "parent-exec", executePayload["parent_execution_id"])
	assert.Equal(t, map[string]interface{}{"existing_result_action": "overwrite"}, executePayload["parameters"])
	assert.NotContains(t, executePayload, "schedule")
	assert.NotContains(t, executePayload, "enabled")
	assert.NotContains(t, executePayload, "next_run_at")
}

func TestExecuteWithTaskProviderRejectsDeprecatedTaskTypeBeforeHTTPCall(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.NotFound(w, r)
	}))
	defer server.Close()

	executor := &Executor{
		taskProviderRegistry: &TaskProviderRegistry{
			providers: map[string]*commonModels.TaskProvider{
				"develop": {
					ModuleName:          "develop",
					BaseURL:             server.URL,
					TaskExecuteEndpoint: "/api/v1/develop/tasks/{task_type}/{id}/execute",
					TaskStatusEndpoint:  "/api/v1/develop/executions/{execution_id}",
					Capabilities:        jsonStringPtr(taskCapabilitiesForTest("workflow", true, `{"type":"object","additionalProperties":false}`)),
				},
			},
			cacheTTL:              time.Hour,
			lastRefresh:           time.Now(),
			loadExecutionContract: executionContractLoaderForTest(`{"type":"object","additionalProperties":false}`),
		},
	}

	result, err := executor.executeWithTaskProvider(
		context.Background(),
		&models.Step{Provider: "develop", TaskType: "workflow", TaskID: 42, Timeout: 10},
		map[string]interface{}{},
		time.Now(),
		"parent-exec",
		"manual",
		7,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deprecated")
	assert.Equal(t, "failed", result.Status)
	assert.False(t, called)
}

func TestExecuteWithTaskProviderRejectsDisallowedParametersBeforeHTTPCall(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.NotFound(w, r)
	}))
	defer server.Close()

	executor := &Executor{
		taskProviderRegistry: &TaskProviderRegistry{
			providers: map[string]*commonModels.TaskProvider{
				"meta": {
					ModuleName:          "meta",
					BaseURL:             server.URL,
					TaskExecuteEndpoint: "/api/v1/meta/tasks/{task_type}/{id}/execute",
					TaskStatusEndpoint:  "/api/v1/meta/executions/{execution_id}",
					Capabilities:        jsonStringPtr(taskCapabilitiesForTest("scan", false, `{"type":"object","additionalProperties":false}`)),
				},
			},
			cacheTTL:              time.Hour,
			lastRefresh:           time.Now(),
			loadExecutionContract: executionContractLoaderForTest(`{"type":"object","additionalProperties":false}`),
		},
	}

	result, err := executor.executeWithTaskProvider(
		context.Background(),
		&models.Step{Provider: "meta", TaskType: "scan", TaskID: 42, Timeout: 10},
		map[string]interface{}{"force": true},
		time.Now(),
		"parent-exec",
		"manual",
		7,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parameters.force is not allowed")
	assert.Equal(t, "failed", result.Status)
	assert.False(t, called)
}

func TestExecuteWithTaskProviderStrictlyValidatesResolvedParametersBeforeHTTPCall(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.NotFound(w, r)
	}))
	defer server.Close()

	executionSchema := `{
		"type":"object",
		"properties":{"limit":{"type":"integer","minimum":1,"maximum":1000}},
		"additionalProperties":false
	}`
	executor := &Executor{
		taskProviderRegistry: &TaskProviderRegistry{
			providers: map[string]*commonModels.TaskProvider{
				"develop": {
					ModuleName:          "develop",
					BaseURL:             server.URL,
					TaskExecuteEndpoint: "/api/v1/develop/tasks/{task_type}/{id}/execute",
					TaskStatusEndpoint:  "/api/v1/develop/executions/{execution_id}",
					Capabilities:        jsonStringPtr(taskCapabilitiesForTest("query", false, executionSchema)),
				},
			},
			cacheTTL:              time.Hour,
			lastRefresh:           time.Now(),
			loadExecutionContract: executionContractLoaderForTest(executionSchema),
		},
	}

	result, err := executor.executeWithTaskProvider(
		context.Background(),
		&models.Step{Provider: "develop", TaskType: "query", TaskID: 42, Timeout: 10},
		map[string]interface{}{"limit": "100"},
		time.Now(),
		"parent-exec",
		"manual",
		7,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parameters.limit must be integer")
	assert.Equal(t, "failed", result.Status)
	assert.False(t, called)
}

func executionContractLoaderForTest(inputSchemaJSON string) func(context.Context, *commonModels.TaskProvider, string, uint, uint) (*taskprovider.ExecutionContract, error) {
	return func(context.Context, *commonModels.TaskProvider, string, uint, uint) (*taskprovider.ExecutionContract, error) {
		var inputSchema map[string]interface{}
		if err := json.Unmarshal([]byte(inputSchemaJSON), &inputSchema); err != nil {
			return nil, err
		}
		return taskprovider.ParseExecutionContract(map[string]interface{}{
			"input_schema":    inputSchema,
			"input_defaults":  map[string]interface{}{},
			"input_ui_schema": map[string]interface{}{},
			"output_schema":   taskprovider.ClosedObjectSchema(),
		})
	}
}

func TestExtractProviderExecutionID(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]interface{}
		want string
	}{
		{
			name: "top level execution id",
			raw:  map[string]interface{}{"execution_id": "exec-top"},
			want: "exec-top",
		},
		{
			name: "missing execution id",
			raw:  map[string]interface{}{"status": "success"},
			want: "",
		},
		{
			name: "legacy id is rejected",
			raw:  map[string]interface{}{"id": "legacy-exec", "status": "running"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractProviderExecutionID(tt.raw))
		})
	}
}

func TestReplaceTaskProviderEndpointOnlyUsesStandardPlaceholders(t *testing.T) {
	got := replaceTaskProviderEndpoint(
		"/api/v1/meta/tasks/{task_type}/{id}/execute",
		"scan",
		"42",
		"",
	)
	assert.Equal(t, "/api/v1/meta/tasks/scan/42/execute", got)

	legacy := replaceTaskProviderEndpoint(
		"/api/v1/meta/tasks/:task_type/:id/execute",
		"scan",
		"42",
		"",
	)
	assert.Equal(t, "/api/v1/meta/tasks/:task_type/:id/execute", legacy)
}

// TestSplitPath 测试路径分割
func TestSplitPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name:     "Simple path",
			path:     "step.result.field",
			expected: []string{"step", "result", "field"},
		},
		{
			name:     "Nested path",
			path:     "step.result.nested.field.value",
			expected: []string{"step", "result", "nested", "field", "value"},
		},
		{
			name:     "Single element",
			path:     "step",
			expected: []string{"step"},
		},
		{
			name:     "Empty path",
			path:     "",
			expected: []string{},
		},
		{
			name:     "Path with consecutive dots",
			path:     "step..result",
			expected: []string{"step", "result"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveStringTemplate 测试字符串模板解析
func TestResolveStringTemplate(t *testing.T) {
	executor := &Executor{}

	stepResults := models.StepResults{
		"step1": {
			Status: "success",
			Result: map[string]interface{}{
				"outputs": map[string]interface{}{
					"table": "my_table",
					"nested": map[string]interface{}{
						"value": 123,
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		template string
		expected interface{}
		wantErr  string
	}{
		{
			name:     "Valid template",
			template: "{{step1.outputs.table}}",
			expected: "my_table",
		},
		{
			name:     "Nested template",
			template: "{{step1.outputs.nested.value}}",
			expected: 123,
		},
		{
			name:     "Not a template",
			template: "plain_string",
			expected: "plain_string",
		},
		{
			name:     "Invalid template format",
			template: "{{incomplete",
			expected: "{{incomplete",
		},
		{
			name:     "Legacy arbitrary result path",
			template: "{{step1.table}}",
			wantErr:  "must reference a declared output",
		},
		{
			name:     "Non-existent step",
			template: "{{nonexistent.outputs.field}}",
			wantErr:  `referenced step "nonexistent" has no result`,
		},
		{
			name:     "Empty template",
			template: "{{}}",
			wantErr:  "must reference a declared output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.resolveStringTemplate(tt.template, stepResults)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProviderExecutionErrorMessage(t *testing.T) {
	got := providerExecutionErrorMessage(map[string]interface{}{
		"status": "failed",
		"error_details": map[string]interface{}{
			"message": "boom",
		},
	})

	assert.Equal(t, "boom", got)
}

func TestTopologicalSortExecutesDependenciesBeforeDependents(t *testing.T) {
	graph := buildDAG(models.Steps{
		{ID: "query"},
		{ID: "orch1", DependsOn: []string{"query"}},
		{ID: "orch2", DependsOn: []string{"orch1"}},
	})

	got, err := topologicalSort(graph)

	assert.NoError(t, err)
	assert.Equal(t, []string{"query", "orch1", "orch2"}, got)
}

func jsonStringPtr(value string) *commonModels.JSONString {
	jsonString := commonModels.JSONString(value)
	return &jsonString
}
