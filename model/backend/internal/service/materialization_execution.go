package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	execution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const MaterializationTaskType = "logical_table_materialization"
const materializationRunTimeout = 45 * time.Second
const materializationLeaseDuration = 90 * time.Second

type MaterializationTask struct {
	ID                int64                          `json:"id"`
	TenantID          int64                          `json:"tenant_id"`
	Name              string                         `json:"name"`
	Description       string                         `json:"description"`
	TaskType          string                         `json:"task_type"`
	Status            string                         `json:"status"`
	ExecutionContract taskprovider.ExecutionContract `json:"execution_contract"`
}

func materializationTask(table models.LogicalTable) MaterializationTask {
	return MaterializationTask{ID: table.ID, TenantID: table.TenantID, Name: table.Name, Description: table.Description, TaskType: MaterializationTaskType, Status: "idle", ExecutionContract: taskprovider.ExecutionContract{
		InputSchema:   map[string]interface{}{"type": "object", "properties": map[string]interface{}{"version": map[string]interface{}{"type": "integer", "minimum": 1}}, "required": []interface{}{"version"}, "additionalProperties": false},
		InputDefaults: map[string]interface{}{"version": table.Version}, InputUISchema: map[string]interface{}{"version": map[string]interface{}{"order": 0}},
		OutputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"execution_id": map[string]interface{}{"type": "string"}, "target_locator": map[string]interface{}{"type": "string", "format": "resource-locator"}}, "required": []interface{}{"execution_id", "target_locator"}, "additionalProperties": false},
	}}
}

func (s *MaterializationService) ListMaterializationTasks(ctx context.Context, tenantID int64, page, size int) ([]MaterializationTask, int64, error) {
	var tables []models.LogicalTable
	query := s.logicalTableRepo.DB().WithContext(ctx).Model(&models.LogicalTable{}).Where("tenant_id = ? AND status = ? AND materialization->>'target_name' <> '' AND materialization->>'target_parent_locator' <> ''", tenantID, "approved")
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&tables).Error; err != nil {
		return nil, 0, err
	}
	items := make([]MaterializationTask, 0, len(tables))
	for _, t := range tables {
		items = append(items, materializationTask(t))
	}
	return items, total, nil
}
func (s *MaterializationService) MaterializationTaskDetail(id, tenantID int64) (MaterializationTask, error) {
	table, _, _, _, _, err := s.loadApprovedDefinition(id, tenantID)
	if err != nil {
		return MaterializationTask{}, err
	}
	return materializationTask(*table), nil
}

// EnqueueMaterialization is the sole command path for manual and orchestrated creation.
func (s *MaterializationService) EnqueueMaterialization(ctx context.Context, id, tenantID, version int64, token, parentID, trigger string) (*execution.TaskExecution, error) {
	table, _, locator, _, _, err := s.loadApprovedDefinition(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err = requireVersion(table.Version, version); err != nil {
		return nil, err
	}
	if s.authorizationIssuer == nil || s.systemClient == nil {
		return nil, apperrors.Unavailable("materialization_authorization_unavailable", modeli18n.MsgMaterializationInvalid)
	}
	source := execution.ModuleModel
	if parentID != "" {
		source = execution.ModuleOrchestrator
		if _, err = execution.NormalizeOrchestratorChildContext(source, parentID); err != nil {
			return nil, err
		}
	}
	trigger, err = execution.NormalizeTriggerType(trigger)
	if err != nil {
		return nil, err
	}
	item := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenantID), Module: execution.ModuleModel, TaskType: MaterializationTaskType, Source: source, SourceTaskID: execution.NewSourceTaskIDFromInt(int(id)), SourceTaskName: &table.Name, Status: execution.ExecutionStatusPending, TriggerType: trigger, ExecutionBoundary: execution.ExecutionBoundaryBounded, MaxAttempts: 1, ExecutionConfig: commonModels.JSONMap{"logical_table_id": id, "version": version, "materialization": table.Materialization}, Metadata: commonModels.JSONMap{}, ErrorDetails: commonModels.JSONMap{}}
	if parentID != "" {
		item.ParentExecutionID = &parentID
	}
	db := s.logicalTableRepo.DB()
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, e := repository.LockLogicalTable(tx, id, tenantID)
		if e != nil {
			return e
		}
		if e = requireVersion(locked.Version, version); e != nil {
			return e
		}
		if locked.Status != "approved" {
			return apperrors.Conflict("materialization_table_not_approved", modeli18n.MsgMaterializationConflict)
		}
		var active int64
		if e = tx.Model(&execution.TaskExecution{}).Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?", tenantID, execution.ModuleModel, MaterializationTaskType, *item.SourceTaskID, []string{"pending", "running"}).Count(&active).Error; e != nil {
			return e
		}
		if active > 0 {
			return apperrors.Conflict("materialization_execution_active", modeli18n.MsgMaterializationConflict)
		}
		if parentID != "" {
			if e = execution.InheritOrchestratorActor(tx, item); e != nil {
				return apperrors.Forbidden("materialization_parent_invalid", modeli18n.MsgMaterializationConflict)
			}
		}
		return tx.Create(item).Error
	})
	if err != nil {
		return nil, err
	}
	accesses := []commonClient.ExecutionEngineAccessScope{{EngineID: strconv.FormatUint(uint64(locator.EngineID), 10), Effects: []string{"read", "ddl"}}}
	var issued *commonClient.IssuedExecutionAuthorization
	if parentID == "" {
		issued, err = s.authorizationIssuer.Issue(ctx, token, commonClient.IssueExecutionAuthorizationRequest{Audience: execution.AudienceModel, ExecutionID: item.ExecutionID, Accesses: accesses, ExpiresIn: materializationAuthorizationTTL})
	} else {
		issued, err = s.systemClient.WithTenantID(uint(tenantID)).IssueExecutionAuthorizationFromExecution(ctx, commonClient.IssueExecutionAuthorizationFromExecutionRequest{ParentExecutionID: parentID, Audience: execution.AudienceModel, ExecutionID: item.ExecutionID, Accesses: accesses, ExpiresIn: materializationAuthorizationTTL})
	}
	var fields map[string]interface{}
	if err == nil {
		fields, err = commonClient.TaskExecutionAuthorizationFields(issued)
	}
	if err == nil {
		fields["triggered_by"] = fields["actor_principal_id"]
		result := db.WithContext(ctx).Model(&execution.TaskExecution{}).Where("execution_id = ? AND tenant_id = ? AND status = ?", item.ExecutionID, tenantID, "pending").Updates(fields)
		err = result.Error
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("execution no longer pending")
		}
	}
	if err != nil {
		now := time.Now().UTC()
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		db.WithContext(persist).Model(&execution.TaskExecution{}).Where("execution_id = ? AND status = ?", item.ExecutionID, "pending").Updates(map[string]interface{}{"status": "failed", "completed_at": now, "error_details": commonModels.JSONMap{"code": "materialization_authorization_failed", "message_key": modeli18n.MsgMaterializationInvalid}})
		return nil, materializedTargetAuthorizationError(err)
	}
	return item, nil
}

// StartMaterializationRunner runs a supervised bounded queue in the Model backend.
func (s *MaterializationService) StartMaterializationRunner(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		owner := "model-materialization-" + uuid.NewString()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			s.recoverMaterializationExecutions(ctx)
			var item *execution.TaskExecution
			var lease *execution.Lease
			err := s.logicalTableRepo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var e error
				item, lease, e = execution.ClaimNext(ctx, tx, execution.ClaimOptions{Module: execution.ModuleModel, TaskType: MaterializationTaskType, WorkerID: owner, LeaseDuration: materializationLeaseDuration, RequireAuthorization: true})
				return e
			})
			if err != nil {
				log.Printf("model materialization claim: %v", err)
				continue
			}
			if item == nil {
				continue
			}
			s.runMaterialization(ctx, item, *lease)
		}
	}()
	return done
}

func (s *MaterializationService) runMaterialization(ctx context.Context, item *execution.TaskExecution, lease execution.Lease) {
	runCtx, cancel := context.WithTimeout(ctx, materializationRunTimeout)
	defer cancel()
	var config struct {
		LogicalTableID int64 `json:"logical_table_id"`
		Version        int64 `json:"version"`
	}
	raw, _ := json.Marshal(item.ExecutionConfig)
	err := json.Unmarshal(raw, &config)
	locator := ""
	if err == nil && item.ExecutionAuthorizationID != nil {
		locator, err = s.createMaterializedTarget(runCtx, config.LogicalTableID, int64(item.TenantID), config.Version, strconv.FormatInt(*item.ExecutionAuthorizationID, 10), item.ExecutionID)
	} else if err == nil {
		err = fmt.Errorf("execution authorization missing")
	}
	status := "success"
	fields := map[string]interface{}{"progress": 100, "metadata": commonModels.JSONMap{"outputs": commonModels.JSONMap{"execution_id": item.ExecutionID, "target_locator": locator}}}
	if err != nil {
		status = "failed"
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			status = "timeout"
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			status = "cancelled"
		}
		code, message := "materialization_execution_failed", modeli18n.MsgMaterializationInvalid
		if domain, ok := apperrors.As(err); ok {
			code, message = domain.Code, domain.MessageID
		}
		fields = map[string]interface{}{"error_details": commonModels.JSONMap{"code": code, "message_key": message}}
		log.Printf("model materialization %s failed: %v", item.ExecutionID, err)
	}
	if item.StartedAt != nil {
		fields["execution_time_ms"] = time.Since(*item.StartedAt).Milliseconds()
	}
	completeCtx, completeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer completeCancel()
	if err = execution.CompleteWithLease(completeCtx, s.logicalTableRepo.DB(), lease, status, time.Now().UTC(), fields); err != nil {
		log.Printf("model materialization completion %s: %v", item.ExecutionID, err)
	}
}
func (s *MaterializationService) recoverMaterializationExecutions(ctx context.Context) {
	db := s.logicalTableRepo.DB()
	now := time.Now().UTC()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, e := execution.FindExpiredForUpdate(ctx, tx, execution.ExpiredOptions{Module: execution.ModuleModel, TaskType: MaterializationTaskType, Now: now, Limit: 50})
		if e != nil {
			return e
		}
		for _, item := range items {
			lease, e := execution.LeaseFromExecution(item)
			if e != nil {
				return e
			}
			duration := int64(0)
			if item.StartedAt != nil {
				duration = now.Sub(*item.StartedAt).Milliseconds()
			}
			if e = execution.FailExpired(ctx, tx, lease, now, map[string]interface{}{"execution_time_ms": duration, "error_details": commonModels.JSONMap{"code": "materialization_lease_expired", "message_key": modeli18n.MsgMaterializationConflict}}); e != nil {
				return e
			}
		}
		return tx.Model(&execution.TaskExecution{}).Where("module = ? AND task_type = ? AND status = ? AND execution_authorization_id IS NULL AND created_at < ?", execution.ModuleModel, MaterializationTaskType, "pending", now.Add(-2*time.Minute)).Updates(map[string]interface{}{"status": "failed", "completed_at": now, "error_details": commonModels.JSONMap{"code": "materialization_authorization_incomplete", "message_key": modeli18n.MsgMaterializationInvalid}}).Error
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("model materialization recovery: %v", err)
	}
}

func ModelTaskProviderDeclaration() *commonModels.TaskProviderDeclaration {
	raw, _ := json.Marshal(map[string]interface{}{"schema_version": "task.capabilities/v2", "task_capabilities": []map[string]interface{}{{"type": MaterializationTaskType, "display_name": "逻辑表建表", "description": "创建或校验已审批逻辑表的目标物理表，保留已有数据", "definition_schema": map[string]interface{}{"type": "object"}, "supports_schedule": false, "supports_cancel": false, "supports_inline_execution": false, "create_url": "/modeling/logical-tables", "edit_url": "/modeling/logical-tables/:id?tab=physical-target", "deprecated": false}}})
	capabilities := commonModels.JSONString(raw)
	return &commonModels.TaskProviderDeclaration{DisplayName: "数据建模", Description: "已审批逻辑表的目标表创建与结构校验", TaskListEndpoint: "/api/v1/model/task-provider/tasks", TaskDetailEndpoint: "/api/v1/model/task-provider/tasks/{task_type}/{id}", TaskExecuteEndpoint: "/api/v1/model/task-provider/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/api/v1/model/task-provider/executions/{execution_id}", Capabilities: &capabilities}
}
