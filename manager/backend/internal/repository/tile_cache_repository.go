package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func (r *TileCacheRepository) UpdateTaskLastExecution(ctx context.Context, id uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.TileCacheTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

func (r *TileCacheRepository) ListTileCacheTasksMissingNextRun(ctx context.Context) ([]models.TileCacheTask, error) {
	var tasks []models.TileCacheTask
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NULL", true).
		Find(&tasks).Error
	return tasks, err
}

func (r *TileCacheRepository) UpdateTileCacheTaskNextRun(ctx context.Context, id uint, nextRunAt *time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.TileCacheTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"next_run_at": nextRunAt}).Error
}

func (r *TileCacheRepository) ListDueTileCacheTaskIDs(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if limit <= 0 {
		limit = 100
	}
	var taskIDs []uint
	err := r.db.WithContext(ctx).
		Model(&models.TileCacheTask{}).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Limit(limit).
		Pluck("id", &taskIDs).Error
	return taskIDs, err
}

func (r *TileCacheRepository) ClaimDueTileCacheTask(ctx context.Context, taskID uint, schedule string, now time.Time, nextRunAt *time.Time) (*models.TileCacheTask, error) {
	var claimed *models.TileCacheTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.TileCacheTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND enabled = ? AND schedule = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", taskID, true, schedule, now).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		if err := tx.Model(&models.TileCacheTask{}).
			Where("id = ?", task.ID).
			Updates(map[string]interface{}{
				"next_run_at": nextRunAt,
			}).Error; err != nil {
			return err
		}

		claimed = &task
		return nil
	})
	return claimed, err
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
