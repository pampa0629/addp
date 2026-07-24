package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DeadLetterRepository struct {
	db *gorm.DB
}

func NewDeadLetterRepository(db *gorm.DB) *DeadLetterRepository {
	return &DeadLetterRepository{db: db}
}

// UpsertObservation 按确定性 identity 幂等保存控制索引。
// 首次 execution/source 身份保持不变；重复观测只刷新最近错误、payload reference 和计数。
func (r *DeadLetterRepository) UpsertObservation(ctx context.Context, observation *models.DeadLetter) (*models.DeadLetter, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("dead-letter repository database is not configured")
	}
	if observation == nil {
		return nil, fmt.Errorf("dead-letter observation is required")
	}
	if observation.Identity == "" {
		return nil, fmt.Errorf("dead-letter identity is required")
	}
	if observation.FirstObservedAt.IsZero() || observation.LastObservedAt.IsZero() {
		return nil, fmt.Errorf("dead-letter observation timestamps are required")
	}
	observation.OccurrenceCount = 1
	observation.PayloadAvailable = true

	updates := map[string]interface{}{
		"last_execution_id": observation.LastExecutionID,
		"error_code":        observation.ErrorCode,
		"error_category":    observation.ErrorCategory,
		"error_message":     observation.ErrorMessage,
		"payload_topic":     observation.PayloadTopic,
		"payload_partition": observation.PayloadPartition,
		"payload_offset":    observation.PayloadOffset,
		"payload_available": true,
		"last_observed_at":  observation.LastObservedAt,
		"occurrence_count":  gorm.Expr("dead_letters.occurrence_count + 1"),
		"updated_at":        gorm.Expr("CURRENT_TIMESTAMP"),
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "identity"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(observation).Error; err != nil {
		return nil, fmt.Errorf("upsert transfer dead-letter observation: %w", err)
	}

	stored, err := r.Get(ctx, observation.Identity)
	if err != nil {
		return nil, err
	}
	if stored.ApplyIdentity != observation.ApplyIdentity || stored.SourceIdentity != observation.SourceIdentity ||
		stored.SourcePartition != observation.SourcePartition || stored.SourceOffset != observation.SourceOffset {
		return nil, fmt.Errorf("dead-letter identity conflicts with an existing source record")
	}
	return stored, nil
}

func (r *DeadLetterRepository) Get(ctx context.Context, identity string) (*models.DeadLetter, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("dead-letter repository database is not configured")
	}
	var deadLetter models.DeadLetter
	if err := r.db.WithContext(ctx).Where("identity = ?", identity).First(&deadLetter).Error; err != nil {
		return nil, err
	}
	return &deadLetter, nil
}

// ListByTask 只按认证租户下的 owner task 查询 DLQ 控制索引。
func (r *DeadLetterRepository) ListByTask(ctx context.Context, tenantID, taskID uint, request models.DeadLetterListRequest) ([]models.DeadLetter, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, fmt.Errorf("dead-letter repository database is not configured")
	}
	query := r.db.WithContext(ctx).Model(&models.DeadLetter{}).
		Where("tenant_id = ? AND task_id = ?", tenantID, taskID)
	if request.SourcePartition != "" {
		query = query.Where("source_partition = ?", request.SourcePartition)
	}
	if request.ErrorCategory != "" {
		query = query.Where("error_category = ?", request.ErrorCategory)
	}
	if request.ErrorCode != "" {
		query = query.Where("error_code = ?", request.ErrorCode)
	}
	if request.PayloadAvailable != nil {
		query = query.Where("payload_available = ?", *request.PayloadAvailable)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count transfer dead letters: %w", err)
	}
	var deadLetters []models.DeadLetter
	offset := (request.Page - 1) * request.PageSize
	if err := query.Order("last_observed_at DESC").Order("identity DESC").
		Limit(request.PageSize).Offset(offset).Find(&deadLetters).Error; err != nil {
		return nil, 0, fmt.Errorf("list transfer dead letters: %w", err)
	}
	return deadLetters, total, nil
}

// GetByTask 使用 tenant/task/identity 复合约束，禁止全局 identity 直查。
func (r *DeadLetterRepository) GetByTask(ctx context.Context, tenantID, taskID uint, identity string) (*models.DeadLetter, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("dead-letter repository database is not configured")
	}
	var deadLetter models.DeadLetter
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND task_id = ? AND identity = ?", tenantID, taskID, identity).
		First(&deadLetter).Error; err != nil {
		return nil, err
	}
	return &deadLetter, nil
}

func (r *DeadLetterRepository) ExistsByTask(ctx context.Context, tenantID, taskID uint) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("dead-letter repository database is not configured")
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.DeadLetter{}).
		Where("tenant_id = ? AND task_id = ?", tenantID, taskID).Limit(1).Count(&count).Error; err != nil {
		return false, fmt.Errorf("check transfer dead letters by task: %w", err)
	}
	return count > 0, nil
}

// ListAvailablePayloadReferences 按 identity 游标轮转读取仍标记为 available 的引用。
func (r *DeadLetterRepository) ListAvailablePayloadReferences(ctx context.Context, afterIdentity string, limit int) ([]models.DeadLetterPayloadReference, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("dead-letter repository database is not configured")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("dead-letter payload reference limit must be greater than zero")
	}
	query := r.db.WithContext(ctx).Model(&models.DeadLetter{}).
		Select("identity, payload_topic AS topic, payload_partition AS partition, payload_offset AS offset").
		Where("payload_available = ?", true)
	if afterIdentity != "" {
		query = query.Where("identity > ?", afterIdentity)
	}
	var references []models.DeadLetterPayloadReference
	if err := query.Order("identity ASC").Limit(limit).Scan(&references).Error; err != nil {
		return nil, fmt.Errorf("list available transfer dead-letter payload references: %w", err)
	}
	return references, nil
}

// MarkPayloadUnavailable 只在 payload reference 仍与 probe 快照完全一致时收敛为 unavailable。
func (r *DeadLetterRepository) MarkPayloadUnavailable(ctx context.Context, reference models.DeadLetterPayloadReference, checkedAt time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("dead-letter repository database is not configured")
	}
	if reference.Identity == "" || reference.Topic == "" || reference.Partition < 0 || reference.Offset < 0 || checkedAt.IsZero() {
		return false, fmt.Errorf("complete dead-letter payload reference and check time are required")
	}
	result := r.db.WithContext(ctx).Model(&models.DeadLetter{}).
		Where("identity = ? AND payload_topic = ? AND payload_partition = ? AND payload_offset = ? AND payload_available = ?",
			reference.Identity, reference.Topic, reference.Partition, reference.Offset, true).
		Updates(map[string]interface{}{"payload_available": false, "updated_at": checkedAt})
	if result.Error != nil {
		return false, fmt.Errorf("mark transfer dead-letter payload unavailable: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}
