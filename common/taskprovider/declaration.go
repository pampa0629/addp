package taskprovider

import (
	"strings"

	"github.com/addp/common/models"
)

// ValidateDeclaration validates the module-level TaskProvider declaration.
func ValidateDeclaration(declaration *models.TaskProviderDeclaration) error {
	if declaration == nil {
		return nil
	}
	if strings.TrimSpace(declaration.DisplayName) == "" {
		return validationError("task_provider.display_name is required")
	}
	if strings.TrimSpace(declaration.Description) == "" {
		return validationError("task_provider.description is required")
	}
	endpoints := map[string]string{
		"task_list_endpoint":    strings.TrimSpace(declaration.TaskListEndpoint),
		"task_detail_endpoint":  strings.TrimSpace(declaration.TaskDetailEndpoint),
		"task_execute_endpoint": strings.TrimSpace(declaration.TaskExecuteEndpoint),
		"task_status_endpoint":  strings.TrimSpace(declaration.TaskStatusEndpoint),
	}
	for field, endpoint := range endpoints {
		if endpoint == "" || !strings.HasPrefix(endpoint, "/") {
			return validationError("task_provider.%s must start with /", field)
		}
		if strings.Contains(endpoint, "/provider/tasks") {
			return validationError("task_provider.%s must use the standard task endpoint", field)
		}
	}
	expectedSuffixes := map[string]string{
		"task_list_endpoint":    "/tasks",
		"task_detail_endpoint":  "/tasks/{task_type}/{id}",
		"task_execute_endpoint": "/tasks/{task_type}/{id}/execute",
		"task_status_endpoint":  "/executions/{execution_id}",
	}
	for field, suffix := range expectedSuffixes {
		if !strings.HasSuffix(endpoints[field], suffix) {
			return validationError("task_provider.%s must use standard endpoint suffix %s", field, suffix)
		}
	}
	if declaration.Capabilities == nil {
		return validationError("task_provider.capabilities is required")
	}
	capabilities, err := ParseCapabilities(string(*declaration.Capabilities))
	if err != nil {
		return err
	}
	cancelEndpoint := strings.TrimSpace(declaration.TaskCancelEndpoint)
	if capabilities.HasCancelableTaskType() && cancelEndpoint == "" {
		return validationError("task_provider.task_cancel_endpoint is required when any task_type supports cancel")
	}
	if !capabilities.HasCancelableTaskType() && cancelEndpoint != "" {
		return validationError("task_provider.task_cancel_endpoint must be empty when no task_type supports cancel")
	}
	if cancelEndpoint != "" {
		const suffix = "/executions/{execution_id}/cancel"
		if !strings.HasPrefix(cancelEndpoint, "/") || !strings.HasSuffix(cancelEndpoint, suffix) {
			return validationError("task_provider.task_cancel_endpoint must use standard endpoint suffix %s", suffix)
		}
	}
	return nil
}
