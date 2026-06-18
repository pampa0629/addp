package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmbeddingRepository 维护 Manager 向量化结果 artifact state 和向量化任务定义。
type EmbeddingRepository struct {
	db *gorm.DB
}

func NewEmbeddingRepository(db *gorm.DB) *EmbeddingRepository {
	return &EmbeddingRepository{db: db}
}

type EmbeddingSimilarityResult struct {
	Embedding *models.Embedding
	Distance  float64
}

type EmbeddingListFilter struct {
	TenantID uint
	EngineID uint
	NodeID   uint
	ItemID   uint
	ItemIDs  []uint
	Status   string
	Query    string
	Page     int
	PageSize int
}

func (r *EmbeddingRepository) GetByItemFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.Embedding, error) {
	var emb models.Embedding
	err := r.db.WithContext(ctx).
		Select("id", "tenant_id", "item_fingerprint", "item_id", "engine_id", "locator", "source_version", "model", "dimension", "status", "status_reason", "error_message", "last_execution_id", "vectorized_at", "created_at", "updated_at").
		Where("tenant_id = ? AND item_fingerprint = ?", tenantID, itemFingerprint).
		First(&emb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &emb, err
}

func (r *EmbeddingRepository) GetByItemID(ctx context.Context, tenantID uint, itemID uint) (*models.Embedding, error) {
	var emb models.Embedding
	err := r.db.WithContext(ctx).
		Select("id", "tenant_id", "item_fingerprint", "item_id", "engine_id", "locator", "source_version", "model", "dimension", "status", "status_reason", "error_message", "last_execution_id", "vectorized_at", "created_at", "updated_at").
		Where("tenant_id = ? AND item_id = ?", tenantID, itemID).
		First(&emb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &emb, err
}

func (r *EmbeddingRepository) UpsertEmbeddingState(ctx context.Context, embedding *models.Embedding) error {
	if embedding == nil {
		return errors.New("embedding state is nil")
	}
	if embedding.TenantID == 0 || strings.TrimSpace(embedding.ItemFingerprint) == "" {
		return errors.New("tenant_id and item_fingerprint are required")
	}
	if strings.TrimSpace(embedding.Status) == "" {
		return errors.New("embedding status is required")
	}

	now := time.Now()
	if embedding.Status == models.EmbeddingStatusReady && embedding.VectorizedAt == nil {
		embedding.VectorizedAt = &now
	}

	vectorSQL := "NULL"
	if embedding.Status == models.EmbeddingStatusReady {
		if len(embedding.Embedding) == 0 {
			return errors.New("ready embedding state requires vector")
		}
		vectorSQL = fmt.Sprintf("'%s'::manager.vector", vectorToString(embedding.Embedding))
	}

	sql := fmt.Sprintf(`
		INSERT INTO manager.embeddings
			(tenant_id, item_fingerprint, item_id, engine_id, locator, source_version, embedding, model, dimension, status, status_reason, error_message, last_execution_id, vectorized_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, %s, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, item_fingerprint) DO UPDATE
		SET item_id = EXCLUDED.item_id,
		    engine_id = EXCLUDED.engine_id,
		    locator = EXCLUDED.locator,
		    source_version = EXCLUDED.source_version,
		    embedding = EXCLUDED.embedding,
		    model = EXCLUDED.model,
		    dimension = EXCLUDED.dimension,
		    status = EXCLUDED.status,
		    status_reason = EXCLUDED.status_reason,
		    error_message = EXCLUDED.error_message,
		    last_execution_id = EXCLUDED.last_execution_id,
		    vectorized_at = EXCLUDED.vectorized_at,
		    updated_at = EXCLUDED.updated_at
	`, vectorSQL)

	return r.db.WithContext(ctx).Exec(sql,
		embedding.TenantID,
		embedding.ItemFingerprint,
		embedding.ItemID,
		embedding.EngineID,
		embedding.Locator,
		embedding.SourceVersion,
		embedding.Model,
		embedding.Dimension,
		embedding.Status,
		emptyToNil(embedding.StatusReason),
		emptyToNil(embedding.ErrorMessage),
		embedding.LastExecutionID,
		embedding.VectorizedAt,
		now,
		now,
	).Error
}

func (r *EmbeddingRepository) QueryReadySimilar(ctx context.Context, tenantID uint, queryVector []float32, model string, dimension int, topK int, maxDistance float64) ([]EmbeddingSimilarityResult, error) {
	if tenantID == 0 {
		return nil, errors.New("tenant_id is required")
	}
	if len(queryVector) == 0 {
		return nil, errors.New("query vector is required")
	}
	if strings.TrimSpace(model) == "" || dimension <= 0 {
		return nil, errors.New("model and dimension are required")
	}
	if topK <= 0 {
		topK = 10
	}

	vector := vectorToString(queryVector)
	args := []any{tenantID, models.EmbeddingStatusReady, model, dimension}
	distanceClause := ""
	if maxDistance > 0 {
		distanceClause = " AND embedding OPERATOR(manager.<=>) ?::manager.vector <= ?"
		args = append(args, vector, maxDistance)
	}
	args = append(args, vector, topK)

	querySQL := fmt.Sprintf(`
		SELECT id, tenant_id, item_fingerprint, item_id, engine_id, locator, source_version, model, dimension,
		       status, status_reason, error_message, last_execution_id, vectorized_at, created_at, updated_at,
		       embedding OPERATOR(manager.<=>) ?::manager.vector AS distance
		FROM manager.embeddings
		WHERE tenant_id = ? AND status = ? AND model = ? AND dimension = ? AND embedding IS NOT NULL%s
		ORDER BY embedding OPERATOR(manager.<=>) ?::manager.vector
		LIMIT ?
	`, distanceClause)

	queryArgs := append([]any{vector}, args...)
	rows, err := r.db.WithContext(ctx).Raw(querySQL, queryArgs...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]EmbeddingSimilarityResult, 0)
	for rows.Next() {
		var emb models.Embedding
		var distance float64
		var statusReason sql.NullString
		var errorMessage sql.NullString
		if err := rows.Scan(
			&emb.ID,
			&emb.TenantID,
			&emb.ItemFingerprint,
			&emb.ItemID,
			&emb.EngineID,
			&emb.Locator,
			&emb.SourceVersion,
			&emb.Model,
			&emb.Dimension,
			&emb.Status,
			&statusReason,
			&errorMessage,
			&emb.LastExecutionID,
			&emb.VectorizedAt,
			&emb.CreatedAt,
			&emb.UpdatedAt,
			&distance,
		); err != nil {
			return nil, err
		}
		emb.StatusReason = statusReason.String
		emb.ErrorMessage = errorMessage.String
		results = append(results, EmbeddingSimilarityResult{Embedding: &emb, Distance: distance})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *EmbeddingRepository) ListEmbeddings(ctx context.Context, filter EmbeddingListFilter) ([]*models.Embedding, int64, error) {
	if filter.TenantID == 0 {
		return nil, 0, errors.New("tenant_id is required")
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}

	query := r.db.WithContext(ctx).Model(&models.Embedding{}).Where("tenant_id = ?", filter.TenantID)
	if filter.EngineID > 0 {
		query = query.Where("engine_id = ?", filter.EngineID)
	}
	if filter.ItemID > 0 {
		query = query.Where("item_id = ?", filter.ItemID)
	}
	if len(filter.ItemIDs) > 0 {
		query = query.Where("item_id IN ?", filter.ItemIDs)
	}
	if strings.TrimSpace(filter.Status) != "" {
		query = query.Where("status = ?", strings.TrimSpace(filter.Status))
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		like := "%" + q + "%"
		query = query.Where("locator ILIKE ? OR item_fingerprint ILIKE ? OR error_message ILIKE ?", like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []*models.Embedding
	err := query.
		Select("id", "tenant_id", "item_fingerprint", "item_id", "engine_id", "locator", "source_version", "model", "dimension", "status", "status_reason", "error_message", "last_execution_id", "vectorized_at", "created_at", "updated_at").
		Order("updated_at DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&items).Error
	return items, total, err
}

func (r *EmbeddingRepository) DeleteEmbedding(ctx context.Context, tenantID uint, id uint) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&models.Embedding{}).Error
}

func (r *EmbeddingRepository) ListAllEmbeddings(ctx context.Context, tenantID uint) ([]*models.Embedding, error) {
	var embeddings []*models.Embedding
	err := r.db.WithContext(ctx).
		Select("id", "tenant_id", "item_fingerprint", "item_id", "engine_id", "locator", "source_version", "model", "dimension", "status", "status_reason", "error_message", "last_execution_id", "vectorized_at", "created_at", "updated_at").
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&embeddings).Error
	return embeddings, err
}

func (r *EmbeddingRepository) MarkEmbeddingMissingSource(ctx context.Context, tenantID uint, id uint, reason string) error {
	return r.db.WithContext(ctx).
		Model(&models.Embedding{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(map[string]interface{}{
			"status":        models.EmbeddingStatusMissingSource,
			"status_reason": models.EmbeddingReasonSourceMissing,
			"error_message": strings.TrimSpace(reason),
			"updated_at":    time.Now(),
		}).Error
}

func (r *EmbeddingRepository) DeleteEmbeddingsByItemFingerprints(ctx context.Context, tenantID uint, itemFingerprints []string) error {
	if len(itemFingerprints) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint IN ?", tenantID, itemFingerprints).
		Delete(&models.Embedding{}).Error
}

func (r *EmbeddingRepository) DeleteEmbeddingsByEngine(ctx context.Context, tenantID uint, engineID uint) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Delete(&models.Embedding{}).Error
}

func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(v))
	for _, val := range v {
		parts = append(parts, fmt.Sprintf("%f", val))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func emptyToNil(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// ===== 任务定义相关方法 =====

func (r *EmbeddingRepository) CreateEmbeddingTask(ctx context.Context, task *models.EmbeddingTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *EmbeddingRepository) GetEmbeddingTask(ctx context.Context, id uint, tenantID uint) (*models.EmbeddingTask, error) {
	var task models.EmbeddingTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *EmbeddingRepository) GetEmbeddingTaskByID(ctx context.Context, id uint) (*models.EmbeddingTask, error) {
	var task models.EmbeddingTask
	err := r.db.WithContext(ctx).
		Where("id = ?", id).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *EmbeddingRepository) ListEmbeddingTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.EmbeddingTask, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.EmbeddingTask{}).Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []*models.EmbeddingTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *EmbeddingRepository) UpdateEmbeddingTask(ctx context.Context, task *models.EmbeddingTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *EmbeddingRepository) DeleteEmbeddingTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.EmbeddingTask{}).Error
}

func (r *EmbeddingRepository) ListAllEmbeddingTasks(ctx context.Context, tenantID uint) ([]*models.EmbeddingTask, error) {
	var tasks []*models.EmbeddingTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&tasks).Error
	return tasks, err
}

func (r *EmbeddingRepository) DisableEmbeddingTaskForCleanup(ctx context.Context, tenantID uint, id uint, reason string) error {
	return r.db.WithContext(ctx).
		Model(&models.EmbeddingTask{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(map[string]interface{}{
			"enabled":               false,
			"next_run_at":           nil,
			"last_execution_status": strings.TrimSpace(reason),
			"updated_at":            time.Now(),
		}).Error
}

func (r *EmbeddingRepository) UpdateEmbeddingTaskLastExecution(ctx context.Context, id uint, executionID, status string, runAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.EmbeddingTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_execution_id":     executionID,
			"last_execution_status": status,
			"last_run_at":           runAt,
		}).Error
}

func (r *EmbeddingRepository) ListEmbeddingTasksMissingNextRun(ctx context.Context) ([]models.EmbeddingTask, error) {
	var tasks []models.EmbeddingTask
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NULL", true).
		Find(&tasks).Error
	return tasks, err
}

func (r *EmbeddingRepository) UpdateEmbeddingTaskNextRun(ctx context.Context, id uint, nextRunAt *time.Time) error {
	updates := map[string]interface{}{
		"next_run_at": nextRunAt,
	}
	return r.db.WithContext(ctx).
		Model(&models.EmbeddingTask{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *EmbeddingRepository) ListDueEmbeddingTaskIDs(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if limit <= 0 {
		limit = 100
	}
	var taskIDs []uint
	err := r.db.WithContext(ctx).
		Model(&models.EmbeddingTask{}).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Limit(limit).
		Pluck("id", &taskIDs).Error
	return taskIDs, err
}

func (r *EmbeddingRepository) ClaimDueEmbeddingTask(ctx context.Context, taskID uint, schedule string, now time.Time, nextRunAt *time.Time) (*models.EmbeddingTask, error) {
	var claimed *models.EmbeddingTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.EmbeddingTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND enabled = ? AND schedule = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", taskID, true, schedule, now).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		if err := tx.Model(&models.EmbeddingTask{}).
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
