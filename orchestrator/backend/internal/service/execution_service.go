package service

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	"gorm.io/gorm/clause"

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ExecutionService 统一执行服务（Orchestrator 模块）
type ExecutionService struct {
	db                *gorm.DB
	taskExecutionRepo *commonExecution.TaskExecutionRepository
}

type ExecutionActor struct {
	PrincipalID          int64
	TenantMembershipID   int64
	AuthorizationVersion int64
}

// NewExecutionService 创建执行服务
func NewExecutionService(
	db *gorm.DB,
) *ExecutionService {
	return &ExecutionService{
		db:                db,
		taskExecutionRepo: commonExecution.NewTaskExecutionRepository(db),
	}
}

// CreateExecutionWithContext 创建带标准 TaskProvider 上下文的编排执行记录。
func (s *ExecutionService) CreateExecutionWithContext(ctx context.Context, orchestrationID, tenantID uint, triggerType, source string, parentExecutionID *string, actor ExecutionActor) (*commonExecution.TaskExecution, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	var execution *commonExecution.TaskExecution
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var orch models.Orchestration
		query := tx.Where("id = ? AND tenant_id = ?", orchestrationID, tenantID)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&orch).Error; err != nil {
			return err
		}
		var err error
		execution, err = createOrchestrationExecution(ctx, tx, &orch, triggerType, source, parentExecutionID, actor)
		return err
	})
	return execution, err
}

func createOrchestrationExecution(ctx context.Context, tx *gorm.DB, orch *models.Orchestration, triggerType, source string, parent *string, actor ExecutionActor) (*commonExecution.TaskExecution, error) {
	normalized, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return nil, err
	}
	if source == "" {
		source = commonExecution.ModuleOrchestrator
	}
	if parent == nil && (actor.PrincipalID <= 0 || actor.TenantMembershipID <= 0 || actor.AuthorizationVersion <= 0) {
		return nil, fmt.Errorf("execution actor facts are required")
	}
	if parent != nil && source != commonExecution.ModuleOrchestrator {
		return nil, fmt.Errorf("orchestrator child execution source must be orchestrator")
	}
	config, err := freezeExecutionPlan(orch.Steps)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	triggered := int(actor.PrincipalID)
	name := orch.Name
	item := &commonExecution.TaskExecution{TenantID: int(orch.TenantID), ExecutionID: uuid.NewString(), Module: commonExecution.ModuleOrchestrator, TaskType: commonExecution.TaskTypeOrchestration,
		Source: source, SourceTaskID: commonExecution.NewSourceTaskIDFromUint(orch.ID), SourceTaskName: &name, ParentExecutionID: parent, Status: commonExecution.ExecutionStatusPending,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, TriggerType: normalized, TriggeredBy: &triggered, ActorPrincipalID: &actor.PrincipalID, ActorTenantMembershipID: &actor.TenantMembershipID, IssuedAuthorizationVersion: &actor.AuthorizationVersion,
		ExecutionConfig: config, Metadata: commonModels.JSONMap{}, CreatedAt: now, UpdatedAt: now}
	if parent != nil {
		if err := commonExecution.InheritOrchestratorActor(tx, item); err != nil {
			return nil, err
		}
	}
	if err := tx.WithContext(ctx).Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

// GetExecution 获取执行记录
func (s *ExecutionService) GetExecution(ctx context.Context, id, tenantID uint) (*commonExecution.TaskExecution, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	// 从统一表获取执行记录
	execution, err := s.taskExecutionRepo.GetByID(ctx, int64(id), int(tenantID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("execution not found")
		}
		return nil, err
	}

	// 验证是否为 orchestrator 模块的执行
	if execution.Module != commonExecution.ModuleOrchestrator {
		return nil, fmt.Errorf("execution not found or access denied")
	}

	return execution, nil
}

// GetExecutionByExecutionID 按全局 execution_id 获取 Orchestrator 执行记录。
func (s *ExecutionService) GetExecutionByExecutionID(ctx context.Context, executionID string, tenantID uint) (*commonExecution.TaskExecution, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("tenant_id is required")
	}
	execution, err := s.taskExecutionRepo.GetByExecutionID(ctx, executionID, int(tenantID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("execution not found")
		}
		return nil, err
	}
	if execution.Module != commonExecution.ModuleOrchestrator {
		return nil, fmt.Errorf("execution not found or access denied")
	}
	return execution, nil
}

// ListExecutions 列出执行记录
func (s *ExecutionService) ListExecutions(ctx context.Context, orchestrationID, tenantID uint, page, pageSize int) ([]*commonExecution.TaskExecution, int64, error) {
	sourceTaskID := commonExecution.NewSourceTaskIDFromUint(orchestrationID)
	filter := commonExecution.TaskExecutionFilter{
		TenantID:     int(tenantID),
		Module:       commonExecution.ModuleOrchestrator,
		TaskType:     commonExecution.TaskTypeOrchestration,
		SourceTaskID: sourceTaskID,
		Page:         page,
		PageSize:     pageSize,
	}

	return s.taskExecutionRepo.List(ctx, filter)
}

// ListAllExecutions 列出所有执行记录（跨编排）
func (s *ExecutionService) ListAllExecutions(ctx context.Context, tenantID uint, page, pageSize int) ([]*commonExecution.TaskExecution, int64, error) {
	filter := commonExecution.TaskExecutionFilter{
		TenantID: int(tenantID),
		Module:   commonExecution.ModuleOrchestrator,
		TaskType: commonExecution.TaskTypeOrchestration,
		Page:     page,
		PageSize: pageSize,
	}

	return s.taskExecutionRepo.List(ctx, filter)
}
