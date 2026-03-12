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
func (s *ExecutionService) CreateExecution(ctx context.Context, orchestrationID, tenantID uint) (*commonModels.TaskExecution, error) {
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
		TaskType:       "orchestration",
		SourceTaskID:   &orchIDInt,
		SourceTaskName: &orchName,
		Status:         commonModels.ExecutionStatusPending,
		Progress:       0,
		TriggerType:    commonModels.TriggerTypeManual,
		Metadata:       make(commonModels.JSONMap),
		StartedAt:      &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.taskExecutionRepo.Create(ctx, execution); err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	s.logger.Info("execution created", "execution_id", execution.ExecutionID, "orchestration_id", orchestrationID)
	return execution, nil
}

// GetExecution 获取执行记录
func (s *ExecutionService) GetExecution(ctx context.Context, id, tenantID uint) (*commonModels.TaskExecution, error) {
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

	return execution, nil
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

	// 转换为 map，存入 metadata["step_results"]
	stepResultsMap := make(map[string]interface{})
	for k, v := range stepResults {
		stepResultsMap[k] = v
	}

	metadata := execution.Metadata
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	metadata["step_results"] = stepResultsMap

	// 计算进度（已完成步骤数 / 总步骤数 * 100）
	progress := 0
	if len(stepResults) > 0 {
		if execution.SourceTaskID != nil {
			orch, err := s.orchRepo.GetByID(uint(*execution.SourceTaskID))
			if err == nil && len(orch.Steps) > 0 {
				progress = int(float64(len(stepResults)) / float64(len(orch.Steps)) * 100)
			}
		}
	}

	return s.taskExecutionRepo.UpdateFields(ctx, execution.ExecutionID, execution.TenantID, map[string]interface{}{
		"metadata": metadata,
		"progress": progress,
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
		stepResultsMap := make(map[string]interface{})
		for k, v := range stepResults {
			stepResultsMap[k] = v
		}
		execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), 0)
		if err != nil {
			return err
		}
		metadata := execution.Metadata
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["step_results"] = stepResultsMap
		updates["metadata"] = metadata
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
func (s *ExecutionService) ListExecutions(ctx context.Context, orchestrationID, tenantID uint, page, pageSize int) ([]*commonModels.TaskExecution, int64, error) {
	orchIDInt := int(orchestrationID)
	filter := commonRepo.TaskExecutionFilter{
		TenantID:     int(tenantID),
		Module:       commonModels.ModuleOrchestrator,
		SourceTaskID: &orchIDInt,
		Page:         page,
		PageSize:     pageSize,
	}

	return s.taskExecutionRepo.List(ctx, filter)
}

// ListAllExecutions 列出所有执行记录（跨编排）
func (s *ExecutionService) ListAllExecutions(ctx context.Context, tenantID uint, page, pageSize int) ([]*commonModels.TaskExecution, int64, error) {
	filter := commonRepo.TaskExecutionFilter{
		TenantID: int(tenantID),
		Module:   commonModels.ModuleOrchestrator,
		Page:     page,
		PageSize: pageSize,
	}

	return s.taskExecutionRepo.List(ctx, filter)
}
