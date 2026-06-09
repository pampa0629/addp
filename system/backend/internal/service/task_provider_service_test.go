package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/system/internal/models"
)

func TestValidateTaskProviderAcceptsTaskCapabilitiesV1(t *testing.T) {
	provider := validTaskProviderForTest(validTaskCapabilitiesForTest())

	if err := validateTaskProvider(provider); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}
}

func TestValidateTaskProviderAcceptsTopLevelPrivateExtension(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"task_types":[{`,
		`"x_owner_features":{"supports_preview":true},"task_types":[{`,
		1,
	))

	if err := validateTaskProvider(provider); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}
}

func TestValidateTaskProviderRejectsUnknownTopLevelField(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"task_types":[{`,
		`"owner_features":{"supports_preview":true},"task_types":[{`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "x_ private extension")
}

func TestValidateTaskProviderRejectsMissingSchemaVersion(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"task_types":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"execution_schema":{"type":"object"},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`)

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "capabilities.schema_version")
}

func TestValidateTaskProviderRejectsMissingTaskTypes(t *testing.T) {
	provider := validTaskProviderForTest(`{"schema_version":"task.capabilities/v1"}`)

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "capabilities.task_types")
}

func TestValidateTaskProviderRejectsMissingTaskTypeSchema(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[{
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
	}`)

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "execution_schema")
}

func TestValidateTaskProviderRejectsInvalidTaskTypeName(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"type":"scan"`,
		`"type":"Scan-Task"`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "must match")
}

func TestValidateTaskProviderRejectsNonObjectTaskTypeSchema(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"execution_schema":{"type":"object"}`,
		`"execution_schema":[]`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "execution_schema must be an object schema")
}

func TestValidateTaskProviderRejectsNonObjectSchemaType(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"definition_schema":{"type":"object"}`,
		`"definition_schema":{"type":"array"}`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "definition_schema.type must be object")
}

func TestValidateTaskProviderAcceptsSupportedTaskTypeSchemaSubset(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{
				"type":"object",
				"title":"扫描任务定义",
				"description":"Meta 扫描任务公开字段摘要",
				"properties":{
					"name":{"type":"string","title":"任务名称","minLength":1,"maxLength":128},
					"scan_depth":{"type":"string","enum":["basic","deep"],"default":"basic"},
					"targets":{
						"type":"array",
						"items":{"type":"string"},
						"minItems":1
					}
				},
				"required":["name"],
				"additionalProperties":true
			},
			"execution_schema":{
				"type":"object",
				"properties":{
					"force":{"type":"boolean","default":false},
					"sample_size":{"type":"integer","minimum":1,"maximum":1000}
				},
				"additionalProperties":false
			},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`)

	if err := validateTaskProvider(provider); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}
}

func TestValidateTaskProviderAcceptsDevelopStyleOpenExecutionSchema(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[{
			"type":"workflow",
			"display_name":"工作流任务",
			"description":"执行 Develop 工作流任务",
			"definition_schema":{
				"type":"object",
				"title":"Develop 任务定义公开摘要",
				"properties":{
					"name":{"type":"string","title":"任务名称"},
					"task_type":{"type":"string","enum":["workflow"],"default":"workflow"},
					"content":{
						"type":"object",
						"properties":{
							"workflow_definition":{"type":"object","additionalProperties":true},
							"inputs":{"type":"object","additionalProperties":true}
						},
						"required":["workflow_definition"],
						"additionalProperties":true
					},
					"execution_config":{"type":"object","additionalProperties":true}
				},
				"required":["name","task_type"],
				"additionalProperties":true
			},
			"execution_schema":{
				"type":"object",
				"title":"Develop 执行参数",
				"description":"由具体任务定义中的 parameter_schema/default_parameters 决定",
				"additionalProperties":true
			},
			"supports_schedule":false,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/develop/workflow?action=create",
			"edit_url":"/develop/workflow?action=edit&id=:id",
			"deprecated":false
		}]
	}`)

	if err := validateTaskProvider(provider); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}
}

func TestValidateTaskProviderRejectsUnsupportedTaskTypeSchemaKeywords(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"execution_schema":{"type":"object"}`,
		`"execution_schema":{"type":"object","oneOf":[{"type":"object"}]}`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "oneOf is not supported")
}

func TestValidateTaskProviderRejectsUnknownTaskTypeSchemaKeywords(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"execution_schema":{"type":"object"}`,
		`"execution_schema":{"type":"object","patternProperties":{}}`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "patternProperties is not allowed")
}

func TestValidateTaskProviderRejectsUnsupportedTaskTypeSchemaType(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"execution_schema":{"type":"object"}`,
		`"execution_schema":{"type":"object","properties":{"payload":{"type":"function"}}}`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), `type "function" is not supported`)
}

func TestValidateTaskProviderRejectsInvalidTaskTypeSchemaRequired(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"definition_schema":{"type":"object"}`,
		`"definition_schema":{"type":"object","required":["name","name"]}`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), `required contains duplicate field "name"`)
}

func TestValidateTaskProviderRejectsInvalidTaskTypeSchemaProperties(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"definition_schema":{"type":"object"}`,
		`"definition_schema":{"type":"object","properties":{"name":"string"}}`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "properties.name must be object schema")
}

func TestValidateTaskProviderRejectsInlineExecutionInV1(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"supports_inline_execution":false`,
		`"supports_inline_execution":true`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "supports_inline_execution must be false")
}

func TestValidateTaskProviderRejectsMissingTaskTypeCreateURL(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"execution_schema":{"type":"object"},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`)

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "create_url")
}

func TestValidateTaskProviderRejectsDuplicateTaskType(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[
			{
				"type":"scan",
				"display_name":"扫描任务",
				"description":"执行元数据扫描",
				"definition_schema":{"type":"object"},
				"execution_schema":{"type":"object"},
				"supports_schedule":true,
				"supports_cancel":false,
				"supports_inline_execution":false,
				"create_url":"/meta/scan",
				"edit_url":"/meta/scan?task_id=:id",
				"deprecated":false
			},
			{
				"type":"scan",
				"display_name":"扫描任务",
				"description":"执行元数据扫描",
				"definition_schema":{"type":"object"},
				"execution_schema":{"type":"object"},
				"supports_schedule":true,
				"supports_cancel":false,
				"supports_inline_execution":false,
				"create_url":"/meta/scan",
				"edit_url":"/meta/scan?task_id=:id",
				"deprecated":false
			}
		]
	}`)

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "duplicate task_type")
}

func TestValidateTaskProviderRejectsAbsoluteTaskTypeURL(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"execution_schema":{"type":"object"},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"http://localhost:5175/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`)

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "Console route")
}

func TestValidateTaskProviderRejectsProtocolRelativeTaskTypeURL(t *testing.T) {
	provider := validTaskProviderForTest(`{
		"schema_version":"task.capabilities/v1",
		"task_types":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"execution_schema":{"type":"object"},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"//localhost:5175/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`)

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "Console route")
}

func TestValidateTaskProviderRejectsLegacyProviderTasksEndpoint(t *testing.T) {
	provider := validTaskProviderForTest(validTaskCapabilitiesForTest())
	provider.TaskDetailEndpoint = "/api/v1/transfer/provider/tasks/{task_type}/{id}"

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "not /provider/tasks")
}

func TestValidateTaskProviderRejectsDetailEndpointWithoutTaskType(t *testing.T) {
	provider := validTaskProviderForTest(validTaskCapabilitiesForTest())
	provider.TaskDetailEndpoint = "/api/v1/meta/tasks/{id}"

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "{task_type}")
}

func TestValidateTaskProviderRejectsStatusEndpointWithoutExecutionID(t *testing.T) {
	provider := validTaskProviderForTest(validTaskCapabilitiesForTest())
	provider.TaskStatusEndpoint = "/api/v1/meta/scan/runs/{id}"

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "{execution_id}")
}

func TestValidateTaskProviderRejectsNonStandardStatusEndpoint(t *testing.T) {
	provider := validTaskProviderForTest(validTaskCapabilitiesForTest())
	provider.TaskStatusEndpoint = "/api/v1/meta/scan/runs/{execution_id}"

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "/executions/{execution_id}")
}

func TestValidateTaskProviderRejectsNonStandardExecuteEndpoint(t *testing.T) {
	provider := validTaskProviderForTest(validTaskCapabilitiesForTest())
	provider.TaskExecuteEndpoint = "/api/v1/meta/tasks/{task_type}/{id}/run"

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "/tasks/{task_type}/{id}/execute")
}

func TestValidateTaskProviderRejectsCancelableTaskWithoutCancelEndpoint(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"supports_cancel":false`,
		`"supports_cancel":true`,
		1,
	))

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "task_cancel_endpoint")
}

func TestValidateTaskProviderAcceptsCancelableTaskWithCancelEndpoint(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"supports_cancel":false`,
		`"supports_cancel":true`,
		1,
	))
	provider.TaskCancelEndpoint = "/api/v1/meta/executions/{execution_id}/cancel"

	if err := validateTaskProvider(provider); err != nil {
		t.Fatalf("validateTaskProvider() error = %v, want nil", err)
	}
}

func TestValidateTaskProviderRejectsCancelEndpointWithoutCancelableTask(t *testing.T) {
	provider := validTaskProviderForTest(validTaskCapabilitiesForTest())
	provider.TaskCancelEndpoint = "/api/v1/meta/executions/{execution_id}/cancel"

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "must be empty")
}

func TestValidateTaskProviderRejectsNonStandardCancelEndpoint(t *testing.T) {
	provider := validTaskProviderForTest(strings.Replace(
		validTaskCapabilitiesForTest(),
		`"supports_cancel":false`,
		`"supports_cancel":true`,
		1,
	))
	provider.TaskCancelEndpoint = "/api/v1/meta/scan/runs/{execution_id}/cancel"

	assertTaskProviderValidationError(t, validateTaskProvider(provider), "/executions/{execution_id}/cancel")
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
		"schema_version":"task.capabilities/v1",
		"task_types":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"execution_schema":{"type":"object"},
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
