package service

import (
	"encoding/json"
	"fmt"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
)

type MaterializationTaskDefinition struct {
	ID                int64
	TenantID          int64
	TaskType          string
	Name              string
	Description       string
	ExecutionContract taskprovider.ExecutionContract
}

func ModelTaskProviderDeclaration() (*commonModels.TaskProviderDeclaration, error) {
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{
			materializationCapability(commonExecution.TaskTypeMaterializationPrepare, "物化准备", "创建由 Model 管控的逻辑表 staging 物理表"),
			materializationCapability(commonExecution.TaskTypeMaterializationPublish, "物化发布", "原子发布同一编排 execution 中已准备的物化批次"),
		},
	}
	raw, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal Model TaskProvider capabilities: %w", err)
	}
	encoded := commonModels.JSONString(raw)
	return &commonModels.TaskProviderDeclaration{
		DisplayName:         "数据建模",
		Description:         "已审批逻辑表的受控物化任务",
		TaskListEndpoint:    "/api/v1/model/task-provider/tasks",
		TaskDetailEndpoint:  "/api/v1/model/task-provider/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/model/task-provider/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/model/task-provider/executions/{execution_id}",
		Capabilities:        &encoded,
	}, nil
}

func materializationCapability(taskType, displayName, description string) map[string]interface{} {
	return map[string]interface{}{
		"type": taskType, "display_name": displayName, "description": description,
		"definition_schema": map[string]interface{}{"type": "object"},
		"supports_schedule": false, "supports_cancel": false, "supports_inline_execution": false,
		"create_url": "/modeling/logical-tables", "edit_url": "/modeling/logical-tables/:id", "deprecated": false,
	}
}

func (s *MaterializationService) ListTaskDefinitions(tenantID int64, taskType string, page, pageSize int) ([]MaterializationTaskDefinition, int, error) {
	if !isMaterializationTaskTypeOrEmpty(taskType) {
		return nil, 0, apperrors.Validation("materialization_task_type_invalid", modeli18n.MsgMaterializationInvalid)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	tables, err := s.logicalTableRepo.ListApproved(tenantID)
	if err != nil {
		return nil, 0, err
	}
	items := make([]MaterializationTaskDefinition, 0, len(tables)*2)
	for i := range tables {
		fields, fieldErr := s.logicalTableRepo.GetFields(tables[i].ID)
		if fieldErr != nil {
			return nil, 0, fieldErr
		}
		targetParent, hasParent := materializationString(tables[i].Materialization, "target_parent_locator")
		targetName, hasName := materializationString(tables[i].Materialization, "target_name")
		_, partitioned := tables[i].Materialization["partition_by"]
		if len(fields) == 0 || validateMaterialization(&tables[i], fields) != nil ||
			!hasParent || targetParent == "" || !hasName || targetName == "" || partitioned {
			continue
		}
		for _, candidate := range materializationTaskTypes(taskType) {
			items = append(items, materializationTaskDefinition(tables[i], candidate))
		}
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		return []MaterializationTaskDefinition{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return items[start:end], total, nil
}

func (s *MaterializationService) GetTaskDefinition(logicalTableID, tenantID int64, taskType string) (*MaterializationTaskDefinition, error) {
	if !isMaterializationTaskTypeOrEmpty(taskType) || strings.TrimSpace(taskType) == "" {
		return nil, apperrors.Validation("materialization_task_type_invalid", modeli18n.MsgMaterializationInvalid)
	}
	table, _, _, _, _, err := s.loadApprovedDefinition(logicalTableID, tenantID)
	if err != nil {
		return nil, err
	}
	definition := materializationTaskDefinition(*table, taskType)
	return &definition, nil
}

func materializationTaskDefinition(table models.LogicalTable, taskType string) MaterializationTaskDefinition {
	return MaterializationTaskDefinition{
		ID: table.ID, TenantID: table.TenantID, TaskType: taskType,
		Name: table.Name + materializationTaskNameSuffix(taskType), Description: table.Description,
		ExecutionContract: materializationExecutionContract(taskType),
	}
}

func materializationExecutionContract(taskType string) taskprovider.ExecutionContract {
	properties := map[string]interface{}{"batch_id": map[string]interface{}{"type": "string"}}
	required := []interface{}{"batch_id"}
	if taskType == commonExecution.TaskTypeMaterializationPublish {
		properties["target_locator"] = map[string]interface{}{"type": "string"}
		required = append(required, "target_locator")
	}
	return taskprovider.ExecutionContract{
		InputSchema: taskprovider.ClosedObjectSchema(), InputDefaults: map[string]interface{}{}, InputUISchema: map[string]interface{}{},
		OutputSchema: map[string]interface{}{"type": "object", "properties": properties, "required": required, "additionalProperties": false},
	}
}

func materializationTaskTypes(taskType string) []string {
	if taskType != "" {
		return []string{taskType}
	}
	return []string{commonExecution.TaskTypeMaterializationPrepare, commonExecution.TaskTypeMaterializationPublish}
}

func isMaterializationTaskTypeOrEmpty(taskType string) bool {
	return taskType == "" || taskType == commonExecution.TaskTypeMaterializationPrepare || taskType == commonExecution.TaskTypeMaterializationPublish
}

func materializationTaskNameSuffix(taskType string) string {
	if taskType == commonExecution.TaskTypeMaterializationPrepare {
		return " · 物化准备"
	}
	return " · 物化发布"
}
