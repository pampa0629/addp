package service

import (
	"context"
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
			materializationCapability(commonExecution.TaskTypeMaterializationSeal, "物化封口", "校验通用写入执行及其 staging 物理结构并封口批次"),
			materializationCapability(commonExecution.TaskTypeMaterializationPublish, "物化发布", "原子发布同一编排 execution 中已准备的物化批次"),
			materializationCapability(commonExecution.TaskTypeMaterializationGroupPublish, "物化组发布", "在同一 PostgreSQL 事务内原子发布物化组的全部逻辑表"),
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
	createURL := "/modeling/logical-tables"
	editURL := "/modeling/logical-tables/:id"
	if taskType == commonExecution.TaskTypeMaterializationGroupPublish {
		createURL = "/modeling/materialization-groups"
		editURL = "/modeling/materialization-groups/:id"
	}
	return map[string]interface{}{
		"type": taskType, "display_name": displayName, "description": description,
		"definition_schema": map[string]interface{}{"type": "object"},
		"supports_schedule": false, "supports_cancel": false, "supports_inline_execution": false,
		"create_url": createURL, "edit_url": editURL, "deprecated": false,
	}
}

func (s *MaterializationService) ListTaskDefinitions(ctx context.Context, tenantID int64, taskType string, page, pageSize int) ([]MaterializationTaskDefinition, int, error) {
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
	groups := []models.MaterializationGroup{}
	groupedLogicalTableIDs := map[int64]struct{}{}
	if s.groupService != nil {
		groups, err = s.groupService.ListAll(ctx, tenantID)
		if err != nil {
			return nil, 0, err
		}
		for _, group := range groups {
			for _, member := range group.Members {
				groupedLogicalTableIDs[member.LogicalTableID] = struct{}{}
			}
		}
	}
	items := make([]MaterializationTaskDefinition, 0, len(tables)*2)
	for i := range tables {
		fields, fieldErr := s.logicalTableRepo.GetFields(tables[i].ID)
		if fieldErr != nil {
			return nil, 0, fieldErr
		}
		targetParent, hasParent := materializationString(tables[i].Materialization, "target_parent_locator")
		targetName, hasName := materializationString(tables[i].Materialization, "target_name")
		partitioned := materializationHasPartitioning(tables[i].Materialization)
		if len(fields) == 0 || validateMaterialization(&tables[i], fields) != nil ||
			!hasParent || targetParent == "" || !hasName || targetName == "" || partitioned {
			continue
		}
		for _, candidate := range materializationTaskTypes(taskType) {
			if candidate == commonExecution.TaskTypeMaterializationGroupPublish {
				continue
			}
			if candidate == commonExecution.TaskTypeMaterializationPublish {
				if _, grouped := groupedLogicalTableIDs[tables[i].ID]; grouped {
					continue
				}
			}
			items = append(items, materializationTaskDefinition(tables[i], candidate))
		}
	}
	if taskType == "" || taskType == commonExecution.TaskTypeMaterializationGroupPublish {
		for _, group := range groups {
			items = append(items, materializationGroupTaskDefinition(group))
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

func (s *MaterializationService) GetTaskDefinition(ctx context.Context, logicalTableID, tenantID int64, taskType string) (*MaterializationTaskDefinition, error) {
	if !isMaterializationTaskTypeOrEmpty(taskType) || strings.TrimSpace(taskType) == "" {
		return nil, apperrors.Validation("materialization_task_type_invalid", modeli18n.MsgMaterializationInvalid)
	}
	if taskType == commonExecution.TaskTypeMaterializationGroupPublish {
		if s.groupService == nil {
			return nil, apperrors.NotFound("materialization_group_not_found", modeli18n.MsgMaterializationNotFound)
		}
		group, err := s.groupService.Get(ctx, tenantID, logicalTableID)
		if err != nil {
			return nil, err
		}
		definition := materializationGroupTaskDefinition(*group)
		return &definition, nil
	}
	table, _, _, _, _, err := s.loadApprovedDefinition(logicalTableID, tenantID)
	if err != nil {
		return nil, err
	}
	definition := materializationTaskDefinition(*table, taskType)
	return &definition, nil
}

func materializationGroupTaskDefinition(group models.MaterializationGroup) MaterializationTaskDefinition {
	return MaterializationTaskDefinition{
		ID: group.ID, TenantID: group.TenantID, TaskType: commonExecution.TaskTypeMaterializationGroupPublish,
		Name:        group.Name + materializationTaskNameSuffix(commonExecution.TaskTypeMaterializationGroupPublish),
		Description: group.Description, ExecutionContract: materializationGroupExecutionContract(group),
	}
}

func materializationTaskDefinition(table models.LogicalTable, taskType string) MaterializationTaskDefinition {
	return MaterializationTaskDefinition{
		ID: table.ID, TenantID: table.TenantID, TaskType: taskType,
		Name: table.Name + materializationTaskNameSuffix(taskType), Description: table.Description,
		ExecutionContract: materializationExecutionContract(taskType),
	}
}

func materializationExecutionContract(taskType string) taskprovider.ExecutionContract {
	inputSchema := taskprovider.ClosedObjectSchema()
	inputDefaults := map[string]interface{}{}
	outputProperties := map[string]interface{}{"batch_id": map[string]interface{}{"type": "string"}}
	outputRequired := []interface{}{"batch_id"}
	if taskType == commonExecution.TaskTypeMaterializationPrepare {
		outputProperties["staging_locator"] = map[string]interface{}{"type": "string"}
		outputRequired = append(outputRequired, "staging_locator")
	}
	if taskType == commonExecution.TaskTypeMaterializationSeal {
		inputSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"batch_id":            map[string]interface{}{"type": "string", "minLength": float64(1)},
				"writer_execution_id": map[string]interface{}{"type": "string", "minLength": float64(1)},
				"target_locator":      map[string]interface{}{"type": "string", "minLength": float64(1)},
			},
			"required":             []interface{}{"batch_id", "writer_execution_id", "target_locator"},
			"additionalProperties": false,
		}
		outputProperties["staging_locator"] = map[string]interface{}{"type": "string"}
		outputProperties["schema_fingerprint"] = map[string]interface{}{"type": "string"}
		outputRequired = append(outputRequired, "staging_locator", "schema_fingerprint")
	}
	if taskType == commonExecution.TaskTypeMaterializationPublish {
		outputProperties["target_locator"] = map[string]interface{}{"type": "string"}
		outputRequired = append(outputRequired, "target_locator")
	}
	return taskprovider.ExecutionContract{
		InputSchema: inputSchema, InputDefaults: inputDefaults, InputUISchema: map[string]interface{}{},
		OutputSchema: map[string]interface{}{"type": "object", "properties": outputProperties, "required": outputRequired, "additionalProperties": false},
	}
}

func materializationGroupExecutionContract(group models.MaterializationGroup) taskprovider.ExecutionContract {
	return taskprovider.ExecutionContract{
		InputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{
				"expected_group_id":      map[string]interface{}{"type": "integer", "minimum": float64(1)},
				"expected_group_version": map[string]interface{}{"type": "integer", "minimum": float64(1)},
			}, "additionalProperties": false,
		},
		InputDefaults: map[string]interface{}{"expected_group_id": group.ID, "expected_group_version": group.Version},
		InputUISchema: map[string]interface{}{},
		OutputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{
				"batch_ids":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"target_locators": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			}, "required": []interface{}{"batch_ids", "target_locators"}, "additionalProperties": false,
		},
	}
}

func materializationTaskTypes(taskType string) []string {
	if taskType != "" {
		return []string{taskType}
	}
	return []string{commonExecution.TaskTypeMaterializationPrepare, commonExecution.TaskTypeMaterializationSeal, commonExecution.TaskTypeMaterializationPublish, commonExecution.TaskTypeMaterializationGroupPublish}
}

func isMaterializationTaskTypeOrEmpty(taskType string) bool {
	return taskType == "" || taskType == commonExecution.TaskTypeMaterializationPrepare || taskType == commonExecution.TaskTypeMaterializationSeal || taskType == commonExecution.TaskTypeMaterializationPublish || taskType == commonExecution.TaskTypeMaterializationGroupPublish
}

func materializationTaskNameSuffix(taskType string) string {
	if taskType == commonExecution.TaskTypeMaterializationPrepare {
		return " · 物化准备"
	}
	if taskType == commonExecution.TaskTypeMaterializationSeal {
		return " · 物化封口"
	}
	if taskType == commonExecution.TaskTypeMaterializationGroupPublish {
		return " · 物化组发布"
	}
	return " · 物化发布"
}
