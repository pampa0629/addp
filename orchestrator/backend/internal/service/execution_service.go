package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/orchestrator/internal/models"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ExecutionService 统一执行服务（Orchestrator 模块）
type ExecutionService struct {
	taskExecutionRepo *commonRepo.TaskExecutionRepository
	orchRepo          *repository.OrchestrationRepository
	logger            *slog.Logger
}

// NewExecutionService 创建执行服务
func NewExecutionService(
	db *gorm.DB,
	orchRepo *repository.OrchestrationRepository,
) *ExecutionService {
	return &ExecutionService{
		taskExecutionRepo: commonRepo.NewTaskExecutionRepository(db),
		orchRepo:          orchRepo,
		logger:            logger.With("component", "execution_service"),
	}
}

// CreateExecution 创建执行记录
func (s *ExecutionService) CreateExecution(ctx context.Context, orchestrationID, tenantID uint) (*models.Execution, error) {
	// 获取编排信息
	orch, err := s.orchRepo.GetByID(orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("orchestration not found: %w", err)
	}

	// 创建统一执行记录
	now := time.Now()
	orchIDInt := int(orchestrationID)
	orchName := orch.Name

	execution := &commonModels.TaskExecution{
		TenantID:       int(tenantID),
		ExecutionID:    uuid.New().String(),
		Module:         commonModels.ModuleOrchestrator,
		ExecutionType:  "orchestration",
		SourceTaskID:   &orchIDInt,
		SourceTaskName: &orchName,
		Status:         commonModels.ExecutionStatusPending,
		Progress:       0,
		TriggerType:    commonModels.TriggerTypeManual,
		StepResults:    make(commonModels.JSONMap),
		StartedAt:      &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	s.logger.Info("execution created", "execution_id", execution.ExecutionID, "orchestration_id", orchestrationID)
	return s.convertToOrchestrationExecution(execution), nil
}

// GetExecution 获取执行记录
func (s *ExecutionService) GetExecution(ctx context.Context, id, tenantID uint) (*models.Execution, error) {
	// 从统一表获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), int(tenantID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("execution not found")
		}
		return nil, err
	}

	// 验证是否为 orchestrator 模块的执行
	if execution.Module != commonModels.ModuleOrchestrator {
		return nil, fmt.Errorf("execution not found or access denied")
	}

	return s.convertToOrchestrationExecution(execution), nil
}

// UpdateStatus 更新执行状态
func (s *ExecutionService) UpdateStatus(ctx context.Context, id uint, status string) error {
	// 获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}

	updates := map[string]interface{}{
		"status": status,
	}

	// 如果状态是运行中，更新 started_at
	if status == "running" {
		now := time.Now()
		updates["started_at"] = now
	}

	// 如果状态是完成或失败，更新 completed_at
	if status == "completed" || status == "failed" {
		now := time.Now()
		updates["completed_at"] = now
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, updates)
}

// UpdateCurrentStep 更新当前步骤
func (s *ExecutionService) UpdateCurrentStep(ctx context.Context, id uint, currentStep string) error {
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, map[string]interface{}{
		"current_step": currentStep,
	})
}

// UpdateStepResults 更新步骤结果
func (s *ExecutionService) UpdateStepResults(ctx context.Context, id uint, stepResults models.StepResults) error {
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}

	// 转换为 JSONMap
	stepResultsMap := make(commonModels.JSONMap)
	for k, v := range stepResults {
		stepResultsMap[k] = v
	}

	// 计算进度（已完成步骤数 / 总步骤数 * 100）
	progress := 0
	if len(stepResults) > 0 {
		// 从 source_task_id 获取编排信息以计算总步骤数
		if execution.SourceTaskID != nil {
			orch, err := s.orchRepo.GetByID(uint(*execution.SourceTaskID))
			if err == nil && len(orch.Steps) > 0 {
				progress = int(float64(len(stepResults)) / float64(len(orch.Steps)) * 100)
			}
		}
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, map[string]interface{}{
		"step_results": stepResultsMap,
		"progress":     progress,
	})
}

// FinishExecution 完成执行（成功或失败）
func (s *ExecutionService) FinishExecution(ctx context.Context, id uint, status, errorMsg string, stepResults models.StepResults) error {
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
	if err != nil {
		return err
	}

	now := time.Now()
	updates := map[string]interface{}{
		"status":       status,
		"completed_at": now,
		"progress":     100,
	}

	// 更新步骤结果
	if stepResults != nil {
		stepResultsMap := make(commonModels.JSONMap)
		for k, v := range stepResults {
			stepResultsMap[k] = v
		}
		updates["step_results"] = stepResultsMap
	}

	// 更新错误信息
	if errorMsg != "" {
		updates["error_details"] = commonModels.JSONMap{
			"message": errorMsg,
		}
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, updates)
}

// ListExecutions 列出执行记录
func (s *ExecutionService) ListExecutions(ctx context.Context, orchestrationID, tenantID uint, page, pageSize int) ([]*models.Execution, int64, error) {
	orchIDInt := int(orchestrationID)
	filter := commonRepo.TaskExecutionFilter{
		TenantID:     int(tenantID),
		Module:       commonModels.ModuleOrchestrator,
		SourceTaskID: &orchIDInt,
		Page:         page,
		PageSize:     pageSize,
	}

	executions, total, err := s.taskExecutionRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 转换为 Orchestrator 执行记录
	result := make([]*models.Execution, len(executions))
	for i, exec := range executions {
		result[i] = s.convertToOrchestrationExecution(exec)
	}

	return result, total, nil
}

// ListAllExecutions 列出所有执行记录（跨编排）
func (s *ExecutionService) ListAllExecutions(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Execution, int64, error) {
	filter := commonRepo.TaskExecutionFilter{
		TenantID: int(tenantID),
		Module:   commonModels.ModuleOrchestrator,
		Page:     page,
		PageSize: pageSize,
	}

	executions, total, err := s.taskExecutionRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// 转换为 Orchestrator 执行记录
	result := make([]*models.Execution, len(executions))
	for i, exec := range executions {
		result[i] = s.convertToOrchestrationExecution(exec)
	}

	return result, total, nil
}

// convertToOrchestrationExecution 转换为 Orchestrator 执行记录（API 兼容层）
func (s *ExecutionService) convertToOrchestrationExecution(exec *commonModels.TaskExecution) *models.Execution {
	if exec == nil {
		return nil
	}

	orchExec := &models.Execution{
		ID:       uint(exec.ID),
		TenantID: uint(exec.TenantID),
		Status:   exec.Status,
	}

	// 转换 source_task_id 为 orchestration_id
	if exec.SourceTaskID != nil {
		orchExec.OrchestrationID = uint(*exec.SourceTaskID)
	}

	// 转换 current_step
	if exec.CurrentStep != nil {
		orchExec.CurrentStep = *exec.CurrentStep
	}

	// 转换 step_results
	if exec.StepResults != nil {
		stepResults := make(models.StepResults)
		for k, v := range exec.StepResults {
			// 尝试转换为 StepResult
			if stepResult, ok := v.(map[string]interface{}); ok {
				result := models.StepResult{}
				if status, ok := stepResult["status"].(string); ok {
					result.Status = status
				}
				if resultData, ok := stepResult["result"].(map[string]interface{}); ok {
					result.Result = resultData
				}
				if errMsg, ok := stepResult["error"].(string); ok {
					result.Error = errMsg
				}
				// 时间字段转换（简化处理）
				stepResults[k] = result
			}
		}
		orchExec.StepResults = stepResults
	}

	// 转换错误信息
	if exec.ErrorDetails != nil {
		if msg, ok := exec.ErrorDetails["message"].(string); ok {
			orchExec.ErrorMessage = msg
		}
	}

	// 转换时间字段
	if exec.StartedAt != nil {
		orchExec.StartedAt = exec.StartedAt
	}
	if exec.CompletedAt != nil {
		orchExec.CompletedAt = exec.CompletedAt
	}
	orchExec.CreatedAt = exec.CreatedAt

	return orchExec
}
