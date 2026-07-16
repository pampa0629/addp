package repository

import (
	"context"
	"fmt"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BuildRepository struct {
	db *gorm.DB
}

type BuildExecutionClaimMode string

const (
	BuildExecutionClaimRun   BuildExecutionClaimMode = "run"
	BuildExecutionClaimRerun BuildExecutionClaimMode = "rerun"
)

func NewBuildRepository(db *gorm.DB) *BuildRepository {
	return &BuildRepository{db: db}
}

// ============ BuildTask ============

func (r *BuildRepository) ListTasks(graphID, tenantID uint) ([]models.BuildTask, error) {
	var tasks []models.BuildTask
	err := r.db.Where("graph_id = ? AND tenant_id = ?", graphID, tenantID).
		Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (r *BuildRepository) ListAllTasks(tenantID uint) ([]models.BuildTask, error) {
	var tasks []models.BuildTask
	err := r.db.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (r *BuildRepository) GetTask(id, tenantID uint) (*models.BuildTask, error) {
	var task models.BuildTask
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error
	return &task, err
}

func (r *BuildRepository) CreateTask(task *models.BuildTask) error {
	return r.db.Create(task).Error
}

func (r *BuildRepository) UpdateTask(task *models.BuildTask) error {
	return r.db.Save(task).Error
}

func (r *BuildRepository) DeleteTask(id, tenantID uint) error {
	// 先删子表（review_items、materials），再删任务本身
	if err := r.db.Where("task_id = ? AND tenant_id = ?", id, tenantID).Delete(&models.ReviewItem{}).Error; err != nil {
		return err
	}
	if err := r.db.Where("task_id = ? AND tenant_id = ?", id, tenantID).Delete(&models.BuildMaterial{}).Error; err != nil {
		return err
	}
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.BuildTask{}).Error
}

func (r *BuildRepository) ClaimExecution(ctx context.Context, taskID, graphID, tenantID uint, execution *commonExecution.TaskExecution, mode BuildExecutionClaimMode) (*models.BuildTask, []models.BuildMaterial, error) {
	var task models.BuildTask
	var materials []models.BuildMaterial
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
			return err
		}
		if task.GraphID != graphID {
			return fmt.Errorf("build task %d does not belong to graph %d", taskID, graphID)
		}

		sourceTaskID := commonExecution.NewSourceTaskIDFromUint(taskID)
		var activeCount int64
		if err := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
				tenantID, commonExecution.ModuleGraph, commonExecution.TaskTypeKGBuild, *sourceTaskID,
				[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return fmt.Errorf("%w: graph build task %d already has an active execution", commonAPI.ErrConflict, taskID)
		}
		if task.Status == models.BuildStatusRunning || (task.Status == models.BuildStatusPending && task.ExecutionID != "") {
			return fmt.Errorf("%w: graph build task %d is active", commonAPI.ErrConflict, taskID)
		}

		switch mode {
		case BuildExecutionClaimRun:
		case BuildExecutionClaimRerun:
			if task.Status != models.BuildStatusSuccess && task.Status != models.BuildStatusFailed &&
				task.Status != models.BuildStatusTimeout && task.Status != models.BuildStatusCancelled {
				return fmt.Errorf("graph build task %d cannot rerun from status %s", taskID, task.Status)
			}
			if err := tx.Model(&models.BuildMaterial{}).
				Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
				Updates(map[string]interface{}{
					"status": models.BuildMaterialStatusPending, "processed_chunks": 0, "total_chunks": 0,
					"error_message": "", "processed_at": nil,
				}).Error; err != nil {
				return err
			}
			if err := tx.Where("task_id = ? AND tenant_id = ? AND status = ?", taskID, tenantID, models.ReviewStatusPending).
				Delete(&models.ReviewItem{}).Error; err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported graph build execution claim mode %q", mode)
		}

		if err := tx.Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
			Order("created_at ASC").Find(&materials).Error; err != nil {
			return err
		}
		if len(materials) == 0 {
			return fmt.Errorf("graph build task %d has no materials", taskID)
		}

		execution.SourceTaskID = sourceTaskID
		execution.SourceTaskName = &task.Name
		execution.ExecutionConfig = commonModels.JSONMap{
			"graph_id": graphID, "confidence_threshold": task.ConfidenceThreshold, "material_count": len(materials),
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		taskFields := map[string]interface{}{
			"execution_id": execution.ExecutionID, "status": models.BuildStatusPending,
			"started_at": nil, "completed_at": nil, "error_message": "",
		}
		if mode == BuildExecutionClaimRerun {
			taskFields["stats"] = []byte("{}")
		}
		if err := tx.Model(&task).Updates(taskFields).Error; err != nil {
			return err
		}
		task.ExecutionID = execution.ExecutionID
		task.Status = models.BuildStatusPending
		task.StartedAt = nil
		task.CompletedAt = nil
		task.ErrorMessage = ""
		if mode == BuildExecutionClaimRerun {
			task.Stats = []byte("{}")
		}
		return nil
	})
	return &task, materials, err
}

func (r *BuildRepository) StartExecution(ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("execution_id = ? AND tenant_id = ? AND status = ?", executionID, tenantID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": commonExecution.ExecutionStatusRunning, "started_at": startedAt, "updated_at": startedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: graph execution %s is not pending", commonAPI.ErrConflict, executionID)
		}
		result = tx.Model(&models.BuildTask{}).
			Where("id = ? AND tenant_id = ? AND execution_id = ? AND status = ?", taskID, tenantID, executionID, models.BuildStatusPending).
			Updates(map[string]interface{}{"status": models.BuildStatusRunning, "started_at": startedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: graph build task %d is not pending for execution %s", commonAPI.ErrConflict, taskID, executionID)
		}
		return nil
	})
}

func (r *BuildRepository) FinishExecution(ctx context.Context, taskID, tenantID uint, executionID string, taskFields, executionFields map[string]interface{}) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("execution_id = ? AND tenant_id = ? AND status IN ?", executionID, tenantID,
				[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
			Updates(executionFields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: graph execution %s is not active", commonAPI.ErrConflict, executionID)
		}
		result = tx.Model(&models.BuildTask{}).
			Where("id = ? AND tenant_id = ? AND execution_id = ? AND status IN ?", taskID, tenantID, executionID,
				[]string{models.BuildStatusPending, models.BuildStatusRunning}).
			Updates(taskFields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: graph build task %d is not active for execution %s", commonAPI.ErrConflict, taskID, executionID)
		}
		return nil
	})
}

// ============ BuildMaterial ============

func (r *BuildRepository) ListMaterials(taskID, tenantID uint) ([]models.BuildMaterial, error) {
	var materials []models.BuildMaterial
	err := r.db.Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
		Order("created_at ASC").Find(&materials).Error
	return materials, err
}

func (r *BuildRepository) GetMaterial(id, tenantID uint) (*models.BuildMaterial, error) {
	var mat models.BuildMaterial
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&mat).Error
	return &mat, err
}

func (r *BuildRepository) CreateMaterial(mat *models.BuildMaterial) error {
	return r.db.Create(mat).Error
}

func (r *BuildRepository) UpdateMaterial(mat *models.BuildMaterial) error {
	return r.db.Save(mat).Error
}

func (r *BuildRepository) DeleteMaterial(id, tenantID uint) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.BuildMaterial{}).Error
}

// ============ ReviewItem ============

type ReviewFilter struct {
	GraphID  uint
	TenantID uint
	TaskID   uint   // 0 表示不过滤
	ItemType string // "" 表示不过滤
	Status   string // "" 表示不过滤（默认返回 pending）
	Page     int
	PageSize int
}

func (r *BuildRepository) ListReviewItems(filter ReviewFilter) ([]models.ReviewItem, int64, error) {
	query := r.db.Where("graph_id = ? AND tenant_id = ?", filter.GraphID, filter.TenantID)
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if filter.ItemType != "" {
		query = query.Where("item_type = ?", filter.ItemType)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	} else {
		query = query.Where("status = ?", models.ReviewStatusPending)
	}

	var total int64
	query.Model(&models.ReviewItem{}).Count(&total)

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var items []models.ReviewItem
	err := query.Order("confidence ASC, created_at ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func (r *BuildRepository) GetReviewItem(id, tenantID uint) (*models.ReviewItem, error) {
	var item models.ReviewItem
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	return &item, err
}

func (r *BuildRepository) CreateReviewItem(item *models.ReviewItem) error {
	return r.db.Create(item).Error
}

func (r *BuildRepository) UpdateReviewItem(item *models.ReviewItem) error {
	return r.db.Save(item).Error
}

func (r *BuildRepository) CountPendingReview(graphID, tenantID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.ReviewItem{}).
		Where("graph_id = ? AND tenant_id = ? AND status = ?", graphID, tenantID, models.ReviewStatusPending).
		Count(&count).Error
	return count, err
}
