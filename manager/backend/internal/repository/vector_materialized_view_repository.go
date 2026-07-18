package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// VectorMaterializedViewRepository 维护矢量物化视图任务定义和结果状态。
type VectorMaterializedViewRepository struct {
	db *gorm.DB
}

func NewVectorMaterializedViewRepository(db *gorm.DB) *VectorMaterializedViewRepository {
	return &VectorMaterializedViewRepository{db: db}
}

func (r *VectorMaterializedViewRepository) CreateTask(ctx context.Context, task *models.VectorMaterializedViewTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *VectorMaterializedViewRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.VectorMaterializedViewTask, error) {
	var task models.VectorMaterializedViewTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *VectorMaterializedViewRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.VectorMaterializedViewTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.VectorMaterializedViewTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.VectorMaterializedViewTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *VectorMaterializedViewRepository) UpdateTask(ctx context.Context, task *models.VectorMaterializedViewTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *VectorMaterializedViewRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.VectorMaterializedViewTask{}).Error
}

func (r *VectorMaterializedViewRepository) ListAllTasks(ctx context.Context, tenantID uint) ([]*models.VectorMaterializedViewTask, error) {
	var tasks []*models.VectorMaterializedViewTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&tasks).Error
	return tasks, err
}

func (r *VectorMaterializedViewRepository) DisableTaskForCleanup(ctx context.Context, tenantID uint, id uint, reason string) error {
	return r.db.WithContext(ctx).
		Model(&models.VectorMaterializedViewTask{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(map[string]interface{}{
			"enabled":               false,
			"next_run_at":           nil,
			"last_execution_status": strings.TrimSpace(reason),
			"updated_at":            time.Now(),
		}).Error
}

func (r *VectorMaterializedViewRepository) ClaimExecution(
	ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, overwriteExistingResult bool,
) (*models.VectorMaterializedViewTask, error) {
	var task models.VectorMaterializedViewTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task,
		TaskType:  commonExecution.TaskTypeVectorMaterializedViewGeneration,
		TaskLabel: "vector materialized view",
		TaskName:  func() string { return task.Name },
		TaskConfig: func() commonModels.JSONMap {
			return task.Config
		},
		CurrentResultModel:      &models.VectorMaterializedView{},
		OverwriteExistingResult: overwriteExistingResult,
	})
	if err != nil {
		return nil, err
	}
	task.LastExecutionID = &execution.ExecutionID
	status := commonExecution.ExecutionStatusPending
	task.LastExecutionStatus = &status
	return &task, nil
}

func (r *VectorMaterializedViewRepository) StartExecution(
	ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Start(
		ctx, taskID, tenantID, executionID, startedAt, &models.VectorMaterializedViewTask{}, "vector materialized view",
	)
}

func (r *VectorMaterializedViewRepository) CompleteExecution(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	resultID uint,
	resultFields map[string]interface{},
	executionFields map[string]interface{},
	completedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel:       &models.VectorMaterializedViewTask{},
		ResultModel:     &models.VectorMaterializedView{},
		ResultID:        resultID,
		ResultFields:    resultFields,
		ExecutionFields: executionFields,
	}, "vector materialized view")
}

type VectorMaterializedViewFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *VectorMaterializedViewRepository) ListResults(ctx context.Context, filter VectorMaterializedViewFilter) ([]*models.VectorMaterializedView, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.VectorMaterializedView{}).
		Where("tenant_id = ?", filter.TenantID)
	if filter.ItemID > 0 {
		query = query.Where("item_id = ?", filter.ItemID)
	}
	if itemFingerprint := strings.TrimSpace(filter.ItemFingerprint); itemFingerprint != "" {
		query = query.Where("item_fingerprint = ?", itemFingerprint)
	}
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if q := strings.TrimSpace(filter.Q); q != "" {
		like := "%" + q + "%"
		query = query.Where(
			"locator ILIKE ? OR item_fingerprint ILIKE ? OR target_table ILIKE ? OR error_message ILIKE ?",
			like, like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.VectorMaterializedView
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *VectorMaterializedViewRepository) ListAllResults(ctx context.Context, tenantID uint) ([]*models.VectorMaterializedView, error) {
	var results []*models.VectorMaterializedView
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&results).Error
	return results, err
}

func (r *VectorMaterializedViewRepository) GetResult(ctx context.Context, id uint, tenantID uint) (*models.VectorMaterializedView, error) {
	var result models.VectorMaterializedView
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *VectorMaterializedViewRepository) GetCurrentResult(ctx context.Context, tenantID uint, itemFingerprint, geometryColumn string, targetSRID int) (*models.VectorMaterializedView, error) {
	var result models.VectorMaterializedView
	err := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND item_fingerprint = ? AND source_geometry_column = ? AND target_srid = ?",
			tenantID,
			strings.TrimSpace(itemFingerprint),
			strings.TrimSpace(geometryColumn),
			targetSRID,
		).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *VectorMaterializedViewRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.VectorMaterializedView, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.VectorMaterializedView
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.VectorMaterializedViewStatusReady).
		Order("updated_at DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *VectorMaterializedViewRepository) ListResultsBySourceTable(ctx context.Context, tenantID uint, engineID uint, schema string, table string) ([]*models.VectorMaterializedView, error) {
	var results []*models.VectorMaterializedView
	err := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND source_engine_id = ? AND source_schema = ? AND source_table = ?",
			tenantID,
			engineID,
			strings.TrimSpace(schema),
			strings.TrimSpace(table),
		).
		Order("updated_at DESC, id DESC").
		Find(&results).Error
	return results, err
}

func (r *VectorMaterializedViewRepository) GetReadyByFingerprintGeometry(ctx context.Context, tenantID uint, itemFingerprint, geometryColumn string, targetSRID int) (*models.VectorMaterializedView, error) {
	var result models.VectorMaterializedView
	err := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND item_fingerprint = ? AND source_geometry_column = ? AND target_srid = ? AND status = ?",
			tenantID,
			strings.TrimSpace(itemFingerprint),
			strings.TrimSpace(geometryColumn),
			targetSRID,
			models.VectorMaterializedViewStatusReady,
		).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *VectorMaterializedViewRepository) CreateResult(ctx context.Context, result *models.VectorMaterializedView) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *VectorMaterializedViewRepository) UpdateResult(ctx context.Context, result *models.VectorMaterializedView) error {
	return r.db.WithContext(ctx).Save(result).Error
}

func (r *VectorMaterializedViewRepository) UpdateResultFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.VectorMaterializedView{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *VectorMaterializedViewRepository) MarkResultStale(ctx context.Context, id uint, tenantID uint, reason string) error {
	fields := map[string]interface{}{
		"status": models.VectorMaterializedViewStatusStale,
	}
	if strings.TrimSpace(reason) != "" {
		fields["error_message"] = strings.TrimSpace(reason)
	}
	return r.UpdateResultFields(ctx, id, tenantID, fields)
}

func (r *VectorMaterializedViewRepository) DeleteResult(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.VectorMaterializedView{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.VectorMaterializedViewStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.VectorMaterializedView{}).Error
	})
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
