package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// TileCacheRepository 维护瓦片缓存任务定义和产物状态。
type TileCacheRepository struct {
	db *gorm.DB
}

func NewTileCacheRepository(db *gorm.DB) *TileCacheRepository {
	return &TileCacheRepository{db: db}
}

func (r *TileCacheRepository) CreateTask(ctx context.Context, task *models.TileCacheTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *TileCacheRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.TileCacheTask, error) {
	var task models.TileCacheTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *TileCacheRepository) GetTileCacheTaskByID(ctx context.Context, id uint) (*models.TileCacheTask, error) {
	var task models.TileCacheTask
	err := r.db.WithContext(ctx).
		Where("id = ?", id).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *TileCacheRepository) GetTaskByTargetFingerprintAndFormat(ctx context.Context, tenantID uint, itemFingerprint, tileFormat string, excludeTaskID uint) (*models.TileCacheTask, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	tileFormat = strings.TrimSpace(strings.ToLower(tileFormat))
	if itemFingerprint == "" || tileFormat == "" {
		return nil, nil
	}
	query := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND config -> 'target' ->> 'item_fingerprint' = ? AND LOWER(COALESCE(config -> 'tile' ->> 'format', 'mvt')) = ?",
			tenantID,
			itemFingerprint,
			tileFormat,
		)
	if excludeTaskID > 0 {
		query = query.Where("id <> ?", excludeTaskID)
	}
	var task models.TileCacheTask
	err := query.Order("updated_at DESC, id DESC").First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *TileCacheRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.TileCacheTask, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.TileCacheTask{}).Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []*models.TileCacheTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *TileCacheRepository) UpdateTask(ctx context.Context, task *models.TileCacheTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *TileCacheRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.TileCacheTask{}).Error
}

func (r *TileCacheRepository) ListAllTasks(ctx context.Context, tenantID uint) ([]*models.TileCacheTask, error) {
	var tasks []*models.TileCacheTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&tasks).Error
	return tasks, err
}

func (r *TileCacheRepository) DisableTaskForCleanup(ctx context.Context, tenantID uint, id uint, reason string) error {
	return r.db.WithContext(ctx).
		Model(&models.TileCacheTask{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(map[string]interface{}{
			"enabled":               false,
			"next_run_at":           nil,
			"last_execution_status": strings.TrimSpace(reason),
			"updated_at":            time.Now(),
		}).Error
}

func (r *TileCacheRepository) ClaimExecution(ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, overwriteExistingResult bool) (*models.TileCacheTask, error) {
	var task models.TileCacheTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task,
		TaskType:  commonExecution.TaskTypeVectorTileCacheGeneration,
		TaskLabel: "vector tile cache",
		TaskName:  func() string { return task.Name },
		TaskConfig: func() commonModels.JSONMap {
			return task.Config
		},
		CurrentResultModel:      &models.TileCache{},
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

func (r *TileCacheRepository) StartExecution(ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time) error {
	return newTaskExecutionLifecycle(r.db).Start(
		ctx, taskID, tenantID, executionID, startedAt, &models.TileCacheTask{}, "vector tile cache",
	)
}

func (r *TileCacheRepository) CompleteExecution(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	tileCacheID uint,
	tileCacheFields map[string]interface{},
	executionFields map[string]interface{},
	completedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel:       &models.TileCacheTask{},
		ResultModel:     &models.TileCache{},
		ResultID:        tileCacheID,
		ResultFields:    tileCacheFields,
		ExecutionFields: executionFields,
	}, "vector tile cache")
}

type TileCacheFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *TileCacheRepository) CreateTileCache(ctx context.Context, artifact *models.TileCache) error {
	return r.db.WithContext(ctx).Create(artifact).Error
}

func (r *TileCacheRepository) GetTileCacheByFingerprintAndFormat(ctx context.Context, tenantID uint, itemFingerprint, tileFormat string) (*models.TileCache, error) {
	var artifact models.TileCache
	err := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND item_fingerprint = ? AND tile_format = ?",
			tenantID,
			strings.TrimSpace(itemFingerprint),
			strings.TrimSpace(tileFormat),
		).
		First(&artifact).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &artifact, err
}

func (r *TileCacheRepository) GetLatestReadyTileCacheByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.TileCache, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var artifact models.TileCache
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.TileCacheStatusReady).
		Order("updated_at DESC").
		First(&artifact).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &artifact, err
}

func (r *TileCacheRepository) ListReadyTileCachesByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) ([]*models.TileCache, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var artifacts []*models.TileCache
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.TileCacheStatusReady).
		Order("updated_at DESC").
		Find(&artifacts).Error
	return artifacts, err
}

func (r *TileCacheRepository) GetTileCache(ctx context.Context, id uint, tenantID uint) (*models.TileCache, error) {
	var artifact models.TileCache
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&artifact).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &artifact, err
}

func (r *TileCacheRepository) ListTileCache(ctx context.Context, filter TileCacheFilter) ([]*models.TileCache, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.TileCache{}).
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
	if strings.TrimSpace(filter.Status) != "" {
		query = query.Where("status = ?", strings.TrimSpace(filter.Status))
	}
	if q := strings.TrimSpace(filter.Q); q != "" {
		like := "%" + q + "%"
		query = query.Where("locator ILIKE ? OR item_fingerprint ILIKE ? OR storage_ref ILIKE ? OR error_message ILIKE ?", like, like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var artifacts []*models.TileCache
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&artifacts).Error
	return artifacts, total, err
}

func (r *TileCacheRepository) UpdateTileCache(ctx context.Context, artifact *models.TileCache) error {
	return r.db.WithContext(ctx).Save(artifact).Error
}

func (r *TileCacheRepository) UpdateTileCacheFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.TileCache{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *TileCacheRepository) DeleteTileCache(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.TileCache{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.TileCacheStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.TileCache{}).Error
	})
}

func (r *TileCacheRepository) ListTileCacheByItem(ctx context.Context, tenantID uint, itemID uint) ([]*models.TileCache, error) {
	var artifacts []*models.TileCache
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_id = ?", tenantID, itemID).
		Order("updated_at DESC").
		Find(&artifacts).Error
	return artifacts, err
}

func (r *TileCacheRepository) ListAllTileCaches(ctx context.Context, tenantID uint) ([]*models.TileCache, error) {
	var artifacts []*models.TileCache
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&artifacts).Error
	return artifacts, err
}

func (r *TileCacheRepository) ListTileCachesByEngine(ctx context.Context, tenantID uint, engineID uint) ([]*models.TileCache, error) {
	var artifacts []*models.TileCache
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND locator LIKE ?", tenantID, "addp://engine/"+uintString(engineID)+"/%").
		Order("updated_at DESC, id DESC").
		Find(&artifacts).Error
	return artifacts, err
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
