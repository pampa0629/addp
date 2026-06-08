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
		TaskStatusEndpoint:  "/api/v1/meta/scan/runs/{execution_id}",
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
