package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Model3DTilesRepository 维护分块三维模型瓦片任务定义和受管结果。
type Model3DTilesRepository struct {
	db *gorm.DB
}

func (r *Model3DTilesRepository) GetTaskByItemFingerprintAndFormat(ctx context.Context, tenantID uint, itemFingerprint, targetFormat string) (*models.Model3DTilesTask, error) {
	var task models.Model3DTilesTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config->'source'->>'item_fingerprint' = ? AND config->>'target_format' = ?", tenantID, strings.TrimSpace(itemFingerprint), strings.TrimSpace(targetFormat)).
		Order("updated_at DESC, id DESC").First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func NewModel3DTilesRepository(db *gorm.DB) *Model3DTilesRepository {
	return &Model3DTilesRepository{db: db}
}

func (r *Model3DTilesRepository) CreateResult(ctx context.Context, result *models.Model3DTiles) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *Model3DTilesRepository) GetResult(ctx context.Context, id, tenantID uint) (*models.Model3DTiles, error) {
	var result models.Model3DTiles
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DTilesRepository) GetCurrentResult(ctx context.Context, tenantID uint, itemFingerprint, targetFormat string) (*models.Model3DTiles, error) {
	var result models.Model3DTiles
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND target_format = ? AND status <> ?", tenantID, strings.TrimSpace(itemFingerprint), strings.TrimSpace(targetFormat), models.Model3DTilesStatusDeleted).
		Order("updated_at DESC, id DESC").First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DTilesRepository) ListCurrentResults(ctx context.Context, tenantID uint, itemFingerprint string) ([]*models.Model3DTiles, error) {
	var results []*models.Model3DTiles
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, strings.TrimSpace(itemFingerprint), models.Model3DTilesStatusDeleted).
		Order("target_format ASC, updated_at DESC").Find(&results).Error
	return results, err
}

type Model3DTilesFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	TargetFormat    string
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *Model3DTilesRepository) ListResults(ctx context.Context, filter Model3DTilesFilter) ([]*models.Model3DTiles, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.Model3DTiles{}).Where("tenant_id = ?", filter.TenantID)
	if filter.ItemID > 0 {
		query = query.Where("item_id = ?", filter.ItemID)
	}
	if value := strings.TrimSpace(filter.ItemFingerprint); value != "" {
		query = query.Where("item_fingerprint = ?", value)
	}
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if value := strings.TrimSpace(filter.TargetFormat); value != "" {
		query = query.Where("target_format = ?", value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		query = query.Where("status = ?", value)
	}
	if value := strings.ToLower(strings.TrimSpace(filter.Q)); value != "" {
		pattern := "%" + value + "%"
		query = query.Where("LOWER(locator) LIKE ? OR LOWER(item_fingerprint) LIKE ? OR LOWER(manifest_ref) LIKE ?", pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.Model3DTiles
	err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *Model3DTilesRepository) MarkOtherFingerprintResultsStale(ctx context.Context, tenantID uint, locator, currentFingerprint string) error {
	locator, currentFingerprint = strings.TrimSpace(locator), strings.TrimSpace(currentFingerprint)
	if locator == "" || currentFingerprint == "" {
		return nil
	}
	return r.db.WithContext(ctx).Model(&models.Model3DTiles{}).
		Where("tenant_id = ? AND locator = ? AND item_fingerprint <> ? AND status NOT IN ?", tenantID, locator, currentFingerprint, []string{models.Model3DTilesStatusStale, models.Model3DTilesStatusDeleted}).
		Updates(map[string]interface{}{"status": models.Model3DTilesStatusStale, "updated_at": time.Now()}).Error
}

func (r *Model3DTilesRepository) UpdateResultFields(ctx context.Context, id, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.Model3DTiles{}).Where("id = ? AND tenant_id = ?", id, tenantID).Updates(fields).Error
}

func (r *Model3DTilesRepository) DeleteResult(ctx context.Context, id, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Model3DTiles{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.Model3DTilesStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Model3DTiles{}).Error
	})
}

func (r *Model3DTilesRepository) CreateTask(ctx context.Context, task *models.Model3DTilesTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *Model3DTilesRepository) CreateExecutionIfIdle(ctx context.Context, taskID, tenantID uint, exec *commonExecution.TaskExecution, runAt time.Time) (bool, error) {
	active := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.Model3DTilesTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).
			First(&task).Error; err != nil {
			return err
		}

		var activeCount int64
		if err := tx.Model(&commonExecution.TaskExecution{}).
			Where(
				"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
				int(tenantID),
				commonExecution.ModuleManager,
				commonExecution.TaskTypeModel3DTilesGeneration,
				commonExecution.NewSourceTaskIDFromUint(taskID),
				[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning},
			).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			active = true
			return nil
		}
		if err := tx.Create(exec).Error; err != nil {
			return err
		}
		return tx.Model(&models.Model3DTilesTask{}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).
			Updates(map[string]interface{}{
				"last_execution_id":     exec.ExecutionID,
				"last_execution_status": exec.Status,
				"last_run_at":           runAt,
			}).Error
	})
	return active, err
}

func (r *Model3DTilesRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.Model3DTilesTask, error) {
	var task models.Model3DTilesTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *Model3DTilesRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DTilesTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.Model3DTilesTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.Model3DTilesTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *Model3DTilesRepository) UpdateTask(ctx context.Context, task *models.Model3DTilesTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *Model3DTilesRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.Model3DTilesTask{}).Error
}

func (r *Model3DTilesRepository) UpdateTaskLastExecution(ctx context.Context, id uint, tenantID uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.Model3DTilesTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}
