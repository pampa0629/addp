package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Model3DGLBRepository 维护单体三维模型 GLB 快显任务定义和结果。
type Model3DGLBRepository struct {
	db *gorm.DB
}

func NewModel3DGLBRepository(db *gorm.DB) *Model3DGLBRepository {
	return &Model3DGLBRepository{db: db}
}

func (r *Model3DGLBRepository) CreateTask(ctx context.Context, task *models.Model3DGLBTask) error {
	return createTaskDefinition(ctx, r.db, commonExecution.TaskTypeModel3DGLBGeneration, task)
}

func (r *Model3DGLBRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.Model3DGLBTask, error) {
	var task models.Model3DGLBTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND task_type = ?", id, tenantID, commonExecution.TaskTypeModel3DGLBGeneration).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *Model3DGLBRepository) GetTaskByItemFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Model3DGLBTask, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var task models.Model3DGLBTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND task_type = ? AND config->'source'->>'item_fingerprint' = ?", tenantID, commonExecution.TaskTypeModel3DGLBGeneration, itemFingerprint).
		Order("updated_at DESC, id DESC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *Model3DGLBRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DGLBTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.Model3DGLBTask{}).
		Where("tenant_id = ? AND task_type = ?", tenantID, commonExecution.TaskTypeModel3DGLBGeneration)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.Model3DGLBTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *Model3DGLBRepository) UpdateTask(ctx context.Context, task *models.Model3DGLBTask) error {
	return updateTaskDefinition(ctx, r.db, commonExecution.TaskTypeModel3DGLBGeneration, task)
}

func (r *Model3DGLBRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.Model3DGLBTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND task_type = ?", id, tenantID, commonExecution.TaskTypeModel3DGLBGeneration).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var active int64
		if err := tx.Model(&commonExecution.TaskExecution{}).Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ? AND execution_config->>'config_version' = ?", tenantID, commonExecution.ModuleManager, commonExecution.TaskTypeModel3DGLBGeneration, *commonExecution.NewSourceTaskIDFromUint(id), []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}, models.Model3DGLBFileConfigVersion).Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return commonAPI.ErrConflict
		}
		return deleteTaskDefinition(ctx, tx, commonExecution.TaskTypeModel3DGLBGeneration, id, tenantID)
	})
}

func (r *Model3DGLBRepository) ClaimExecution(
	ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, overwriteExistingResult bool, snapshot *Model3DGLBTaskSnapshot,
) (*models.Model3DGLBTask, error) {
	if snapshot == nil || snapshot.TaskID != taskID || snapshot.TenantID != tenantID || execution == nil {
		return nil, commonAPI.ErrConflict
	}
	var task models.Model3DGLBTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel:      &task,
		TaskType:       commonExecution.TaskTypeModel3DGLBGeneration,
		TaskTypeColumn: true,
		TaskLabel:      "model 3d GLB",
		TaskName:       func() string { return task.Name },
		TaskConfig: func() commonModels.JSONMap {
			return task.Config
		},
		CurrentResultModel:      &models.Model3DGLB{},
		OverwriteExistingResult: overwriteExistingResult,
		BeforeCreate: func(tx *gorm.DB) error {
			if err := verifyModel3DGLBTaskSnapshot(ctx, tx, snapshot); err != nil {
				return err
			}
			if execution.ExecutionConfig["config_version"] == models.Model3DGLBFileConfigVersion {
				source, _ := jsonMap(execution.ExecutionConfig["source"])
				if task.SemanticKey == "" || firstString(source, "item_fingerprint") != task.SemanticKey {
					return commonAPI.ErrConflict
				}
				// The existing current result is the serialization point shared with
				// deletion, including recreation after its generating task was removed.
				var current models.Model3DGLB
				err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, task.SemanticKey, models.Model3DGLBStatusDeleted).Order("updated_at DESC, id DESC").First(&current).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if err == nil && !overwriteExistingResult {
					return ErrExistingResultActionRequired
				}
				active, err := activeModel3DGLBSourceExecutions(tx, tenantID, task.SemanticKey)
				if err != nil {
					return err
				}
				if active != 0 {
					return commonAPI.ErrConflict
				}
			}
			if len(execution.ExecutionConfig) == 0 {
				decoder := json.NewDecoder(bytes.NewReader(snapshot.Config))
				decoder.UseNumber()
				if err := decoder.Decode(&execution.ExecutionConfig); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	task.LastExecutionID = &execution.ExecutionID
	status := commonExecution.ExecutionStatusPending
	task.LastExecutionStatus = &status
	return &task, nil
}

func (r *Model3DGLBRepository) ValidateClaim(
	ctx context.Context, taskID, tenantID uint, executionID string,
) error {
	return newTaskExecutionLifecycle(r.db).ValidateClaim(
		ctx, taskID, tenantID, executionID, &models.Model3DGLBTask{}, "model 3d GLB",
	)
}

func (r *Model3DGLBRepository) CompleteExecution(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	resultID uint,
	resultFields map[string]interface{},
	executionFields map[string]interface{},
	completedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel:       &models.Model3DGLBTask{},
		ResultModel:     &models.Model3DGLB{},
		ResultID:        resultID,
		ResultFields:    resultFields,
		ExecutionFields: executionFields,
	}, "model 3d GLB")
}

func (r *Model3DGLBRepository) Create(ctx context.Context, result *models.Model3DGLB) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *Model3DGLBRepository) GetByID(ctx context.Context, id uint, tenantID uint) (*models.Model3DGLB, error) {
	var result models.Model3DGLB
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&result).Error; err != nil {
			return err
		}
		// Read the published pointer and exact scope from the same row version.
		return hydrateModel3DGLBOutputMetadata(ctx, tx, &result)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

type Model3DGLBFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *Model3DGLBRepository) List(ctx context.Context, filter Model3DGLBFilter) ([]*models.Model3DGLB, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.Model3DGLB{}).
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
			"locator ILIKE ? OR item_fingerprint ILIKE ? OR file_name ILIKE ? OR error_message ILIKE ?",
			like, like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.Model3DGLB
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *Model3DGLBRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Model3DGLB, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.Model3DGLB
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.Model3DGLBStatusReady).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DGLBRepository) GetCurrentByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Model3DGLB, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.Model3DGLB
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, itemFingerprint, models.Model3DGLBStatusDeleted).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DGLBRepository) UpdateFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.Model3DGLB{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *Model3DGLBRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Model3DGLB{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.Model3DGLBStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.Model3DGLB{}).Error
	})
}
