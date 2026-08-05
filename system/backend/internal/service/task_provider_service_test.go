package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/system/internal/models"
)

func TestValidateTaskProviderAcceptsTaskCapabilitiesV2(t *testing.T) {
	if err := validateTaskProvider(validTaskProviderForTest(validTaskCapabilitiesForTest())); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}
}

func TestValidateTaskProviderAcceptsTopLevelPrivateExtension(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"task_capabilities":[{`,
		`"x_owner_features":{"supports_preview":true},"task_capabilities":[{`,
		1,
	))

	if err := validateTaskProvider(provider); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}
}

func TestValidateTaskProviderRejectsLegacyCapabilitiesVersion(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`task.capabilities/v2`,
		`task.capabilities/v1`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "task.capabilities/v2")
}

func TestValidateTaskProviderRejectsExecutionSchemaInTypeCapability(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"definition_schema":{"type":"object"}`,
		`"definition_schema":{"type":"object"},"execution_schema":{"type":"object"}`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "execution_schema is not allowed")
}

func TestValidateTaskProviderRejectsUnknownTopLevelField(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"task_capabilities":[{`,
		`"owner_features":{},"task_capabilities":[{`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "x_ private extension")
}

func TestValidateTaskProviderRejectsInvalidDefinitionSchema(t *testing.T) {
	tests := []struct {
		name        string
		old         string
		replacement string
		want        string
	}{
		{name: "non object", old: `"definition_schema":{"type":"object"}`, replacement: `"definition_schema":{"type":"array"}`, want: "definition_schema.type must be object"},
		{name: "unsupported keyword", old: `"definition_schema":{"type":"object"}`, replacement: `"definition_schema":{"type":"object","oneOf":[{"type":"object"}]}`, want: "oneOf is not supported"},
		{name: "unknown keyword", old: `"definition_schema":{"type":"object"}`, replacement: `"definition_schema":{"type":"object","patternProperties":{}}`, want: "patternProperties is not allowed"},
		{name: "invalid property", old: `"definition_schema":{"type":"object"}`, replacement: `"definition_schema":{"type":"object","properties":{"name":"string"}}`, want: "properties.name must be object schema"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := validTaskProviderForTest(strings.Replace(validTaskCapabilitiesForTest(), tt.old, tt.replacement, 1))
			assertTaskProviderValidationError(t, validateTaskProvider(provider), tt.want)
		})
	}
}

func TestValidateTaskProviderRejectsInlineExecution(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"supports_inline_execution":false`,
		`"supports_inline_execution":true`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "supports_inline_execution must be false")
}

func TestValidateTaskProviderRejectsInvalidStandardEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.TaskProvider)
		want   string
	}{
		{name: "legacy detail", mutate: func(p *models.TaskProvider) { p.TaskDetailEndpoint = "/api/v1/meta/provider/tasks/{task_type}/{id}" }, want: "not /provider/tasks"},
		{name: "missing task type", mutate: func(p *models.TaskProvider) { p.TaskDetailEndpoint = "/api/v1/meta/tasks/{id}" }, want: "{task_type}"},
		{name: "gin placeholder", mutate: func(p *models.TaskProvider) { p.TaskExecuteEndpoint = "/api/v1/meta/tasks/:task_type/:id/execute" }, want: "{task_type}"},
		{name: "nonstandard execute", mutate: func(p *models.TaskProvider) { p.TaskExecuteEndpoint = "/api/v1/meta/tasks/{task_type}/{id}/run" }, want: "/tasks/{task_type}/{id}/execute"},
		{name: "nonstandard status", mutate: func(p *models.TaskProvider) { p.TaskStatusEndpoint = "/api/v1/meta/runs/{execution_id}" }, want: "/executions/{execution_id}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := validTaskProviderForTest(validTaskCapabilitiesForTest())
			tt.mutate(provider)
			assertTaskProviderValidationError(t, validateTaskProvider(provider), tt.want)
		})
	}
}

func TestValidateTaskProviderRequiresCancelEndpointOnlyForCancelableTypes(t *testing.T) {
	cancelable := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"supports_cancel":false`,
		`"supports_cancel":true`,
		1,
	))
	assertTaskProviderValidationError(t, validateTaskProvider(cancelable), "task_cancel_endpoint")

	cancelable.TaskCancelEndpoint = "/api/v1/meta/executions/{execution_id}/cancel"
	if err := validateTaskProvider(cancelable); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}

	nonCancelable := validTaskProviderForTest(validTaskCapabilitiesForTest())
	nonCancelable.TaskCancelEndpoint = "/api/v1/meta/executions/{execution_id}/cancel"
	assertTaskProviderValidationError(t, validateTaskProvider(nonCancelable), "must be empty")
}

func validTaskProviderForTest(capabilities string) *models.TaskProvider {
	capabilitiesJSON := models.JSONString(capabilities)
	return &models.TaskProvider{
		ModuleName:          "meta",
		DisplayName:         "元数据",
		Description:         "元数据扫描任务",
		BaseURL:             "http://localhost:8082",
		TaskListEndpoint:    "/api/v1/meta/tasks",
		TaskDetailEndpoint:  "/api/v1/meta/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/meta/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/meta/executions/{execution_id}",
		Capabilities:        &capabilitiesJSON,
		IsEnabled:           true,
	}
}

func validTaskCapabilitiesForTest() string {
	return `{
		"schema_version":"task.capabilities/v2",
		"task_capabilities":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`
}

func assertTaskProviderValidationError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("validateTaskProvider() error = nil, want validation error containing %q", want)
	}
	var validationErr *TaskProviderValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("validateTaskProvider() error type = %T, want *TaskProviderValidationError", err)
	}
	if !strings.Contains(validationErr.Message, want) {
		t.Fatalf("validation error = %q, want containing %q", validationErr.Message, want)
	}
}
