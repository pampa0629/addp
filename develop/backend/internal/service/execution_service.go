package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

// ExecutionService dispatches all claimed Develop tasks through their owner domain executor.
// Query keeps its existing policy, authorization and result implementation.
type ExecutionService struct {
	executor *DevExecutor
	queue    *repository.ExecutionRepository
	query    *QueryExecutionService
}

func NewExecutionService(executor *DevExecutor, queue *repository.ExecutionRepository) (*ExecutionService, error) {
	query, err := NewQueryExecutionService(executor, queue)
	if err != nil {
		return nil, err
	}
	executor.executionQueue = queue
	return &ExecutionService{executor: executor, queue: queue, query: query}, nil
}

func (s *ExecutionService) Execute(ctx context.Context, item *commonExecution.TaskExecution, lease commonExecution.Lease) error {
	if item == nil || item.Module != commonExecution.ModuleDevelop || item.ExecutionID != lease.ExecutionID || item.TenantID != lease.TenantID || item.ExecutionBoundary != commonExecution.ExecutionBoundaryBounded {
		return fmt.Errorf("claimed Develop execution does not match its lease")
	}
	if item.TaskType == commonExecution.TaskTypeQuery {
		return s.query.Execute(ctx, item, lease)
	}
	if item.TaskType != commonExecution.TaskTypeWorkflow && item.TaskType != commonExecution.TaskTypeScript {
		return fmt.Errorf("unsupported Develop execution type")
	}
	startedAt := time.Now().UTC()
	if item.StartedAt != nil {
		startedAt = *item.StartedAt
	}
	task, err := devRuntimeTaskFromExecution(item)
	if err != nil {
		return s.fail(ctx, item, lease, startedAt, "snapshot_invalid", err)
	}
	ctx = commonExecution.ContextWithLease(ctx, lease)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(task.Timeout)*time.Second)
	defer cancel()
	var result, outputs commonModels.JSONMap
	var message string
	switch item.TaskType {
	case commonExecution.TaskTypeWorkflow:
		authorization, err := s.workflowAuthorization(runCtx, item, lease, task)
		if err != nil {
			return s.fail(ctx, item, lease, startedAt, "authorization_failed", err)
		}
		runCtx = context.WithValue(runCtx, workflowReceiptKey{}, workflowReceiptRecorder(func(ctx context.Context, runtimeID string) error {
			if err := s.queue.RecordWorkflowReceipt(ctx, lease, runtimeID); err != nil {
				return err
			}
			if item.Metadata == nil {
				item.Metadata = commonModels.JSONMap{}
			}
			item.Metadata["runtime_execution_id"] = runtimeID
			return nil
		}))
		result, outputs, message = s.executor.executeWorkflow(runCtx, task, item.ExecutionID, item.TenantID, authorization)
	case commonExecution.TaskTypeScript:
		// Notebook dispatch remains fail-closed until its queued execution permit is confirmed.
		return s.fail(ctx, item, lease, startedAt, "authorization_unavailable", fmt.Errorf("Notebook queued execution permit is unavailable"))
	}
	metadata := queryExecutionMetadata(result)
	for key, value := range item.Metadata {
		metadata[key] = value
	}
	if len(outputs) > 0 {
		metadata["outputs"] = outputs
	}
	status := commonExecution.ExecutionStatusSuccess
	fields := terminalQueryFields(startedAt, metadata, nil)
	if message != "" {
		status = commonExecution.ExecutionStatusFailed
		suffix := "execution_failed"
		if runCtx.Err() == context.DeadlineExceeded {
			status = commonExecution.ExecutionStatusTimeout
			suffix = "execution_timeout"
		}
		fields["error_details"] = executionFailureDetails(item.TaskType, suffix, message, result)
	} else if facts := developLineageFacts(task, outputs); facts != nil {
		metadata["lineage_facts"] = facts
	}
	if err := s.queue.CompleteWithLease(ctx, item, lease, status, time.Now().UTC(), fields); err != nil {
		return err
	}
	if metadata["lineage_facts"] != nil {
		s.executor.notifyExecutionLineage(ctx, uint(item.TenantID), item.ExecutionID)
	}
	return nil
}

func (s *ExecutionService) fail(ctx context.Context, item *commonExecution.TaskExecution, lease commonExecution.Lease, startedAt time.Time, suffix string, cause error) error {
	return s.queue.FailClaimed(ctx, item, lease, "develop."+item.TaskType+"."+suffix, cause.Error(), time.Now().UTC())
}

func (s *ExecutionService) workflowAuthorization(ctx context.Context, item *commonExecution.TaskExecution, lease commonExecution.Lease, task *models.DevTask) (*IssuedWorkflowExecutionAuthorization, error) {
	if item.ActorPrincipalID == nil || *item.ActorPrincipalID <= 0 || item.ActorTenantMembershipID == nil || *item.ActorTenantMembershipID <= 0 || item.IssuedAuthorizationVersion == nil || *item.IssuedAuthorizationVersion <= 0 {
		return nil, fmt.Errorf("workflow execution actor is incomplete")
	}
	switch item.Source {
	case commonExecution.ModuleDevelop:
		if item.ExecutionAuthorizationID == nil || *item.ExecutionAuthorizationID <= 0 || item.AuthorizationExpiresAt == nil || !item.AuthorizationExpiresAt.After(time.Now().UTC()) {
			return nil, fmt.Errorf("persisted workflow authorization is incomplete or expired")
		}
		plan, err := s.executor.buildWorkflowExecutionAuthorizationPlan(ctx, task, uint(item.TenantID))
		if err != nil {
			return nil, err
		}
		return &IssuedWorkflowExecutionAuthorization{AuthorizationID: *item.ExecutionAuthorizationID, EngineEffects: plan.engineEffects, Effects: workflowEffectUnion(plan.engineEffects), ActorPrincipalID: *item.ActorPrincipalID, ActorTenantMembershipID: *item.ActorTenantMembershipID, IssuedAuthorizationVersion: *item.IssuedAuthorizationVersion, ExpiresAt: item.AuthorizationExpiresAt.UTC()}, nil
	case commonExecution.ModuleOrchestrator:
		if item.ParentExecutionID == nil {
			return nil, fmt.Errorf("workflow child has no parent execution")
		}
		parentID, err := uuid.Parse(strings.TrimSpace(*item.ParentExecutionID))
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(item.ExecutionID)
		if err != nil {
			return nil, err
		}
		authorization, err := s.executor.prepareWorkflowExecutionAuthorizationFromExecution(ctx, task, uint(item.TenantID), parentID, executionID, lease)
		if err != nil {
			return nil, err
		}
		if authorization.ActorPrincipalID != *item.ActorPrincipalID || authorization.ActorTenantMembershipID != *item.ActorTenantMembershipID || authorization.IssuedAuthorizationVersion != *item.IssuedAuthorizationVersion {
			return nil, fmt.Errorf("workflow authorization actor does not match parent provenance")
		}
		if err := s.queue.AttachAuthorizationWithLease(ctx, lease, authorization.AuthorizationID, authorization.ExpiresAt); err != nil {
			return nil, err
		}
		return authorization, nil
	default:
		return nil, fmt.Errorf("unsupported workflow execution source")
	}
}

func devRuntimeTaskFromExecution(item *commonExecution.TaskExecution) (*models.DevTask, error) {
	content, ok := mapValue(item.ExecutionConfig["content"])
	if !ok {
		return nil, fmt.Errorf("Develop execution content snapshot is missing")
	}
	timeout, ok := positiveInt(item.ExecutionConfig["timeout"])
	if !ok {
		return nil, fmt.Errorf("Develop execution timeout snapshot is invalid")
	}
	config := models.DevTaskContent{}
	for key, value := range item.ExecutionConfig {
		config[key] = value
	}
	for _, key := range []string{"content", "inputs", "timeout", "runtime_parameters"} {
		delete(config, key)
	}
	parameters, _ := mapValue(item.ExecutionConfig["runtime_parameters"])
	task := &models.DevTask{TenantID: uint(item.TenantID), DevType: item.TaskType, Content: models.DevTaskContent(content), ExecutionConfig: config, Timeout: timeout, RuntimeParameters: parameters}
	if item.SourceTaskID != nil {
		id, err := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID)
		if err != nil {
			return nil, err
		}
		task.ID = id
	}
	if err := validateDevTaskContent(task.DevType, task.Content); err != nil {
		return nil, err
	}
	if err := validateDevTaskExecutionConfig(task.DevType, task.Content, task.ExecutionConfig); err != nil {
		return nil, err
	}
	return task, nil
}

type workflowReceiptKey struct{}
type workflowReceiptRecorder func(context.Context, string) error
