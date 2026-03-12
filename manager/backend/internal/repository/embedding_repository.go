package repository

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// EmbeddingRepository 向量数据库访问层
type EmbeddingRepository struct {
	db *gorm.DB
}

// NewEmbeddingRepository 创建向量仓库
func NewEmbeddingRepository(db *gorm.DB) *EmbeddingRepository {
	return &EmbeddingRepository{db: db}
}

// UpsertEmbedding 插入或更新向量
// 使用 fingerprint + modality 作为唯一键
func (r *EmbeddingRepository) UpsertEmbedding(ctx context.Context, embedding *models.Embedding) error {
	// 先查询是否存在
	var existing models.Embedding
	err := r.db.WithContext(ctx).
		Where("fingerprint = ? AND modality = ?", embedding.Fingerprint, embedding.Modality).
		First(&existing).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 不存在，创建新记录（使用原生 SQL 以支持 vector 类型）
		vectorStr := vectorToString(embedding.Embedding)
		log.Printf("[DEBUG] 开始插入向量: fingerprint=%s, modality=%s, vector_length=%d",
			embedding.Fingerprint, embedding.Modality, len(embedding.Embedding))

		// 使用 fmt.Sprintf 构建完整 SQL，避免 GORM 参数绑定问题
		// 使用 vector 类型（通过 search_path=manager 自动解析）
		sql := fmt.Sprintf(`
			INSERT INTO manager.embeddings
			(engine_id, bucket, path, name, fingerprint, data_updated_at, embedding, modality, model, file_size, content_type, metadata, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '%s'::vector, ?, ?, ?, ?, ?, ?, ?, ?)
		`, vectorStr)

		err := r.db.WithContext(ctx).Exec(sql,
			embedding.EngineID,
			embedding.Bucket,
			embedding.Path,
			embedding.Name,
			embedding.Fingerprint,
			embedding.DataUpdatedAt,
			// vectorStr 已经在 SQL 中，这里移除
			embedding.Modality,
			embedding.Model,
			embedding.FileSize,
			embedding.ContentType,
			embedding.Metadata,
			embedding.TenantID,
			time.Now(),
			time.Now(),
		).Error

		if err != nil {
			log.Printf("[ERROR] 向量插入失败: %v", err)
			return err
		}

		log.Printf("[DEBUG] 向量插入成功: fingerprint=%s", embedding.Fingerprint)
		return nil
	}

	if err != nil {
		return err
	}

	// 存在，更新记录
	log.Printf("[DEBUG] 更新现有向量: id=%d, fingerprint=%s", existing.ID, embedding.Fingerprint)

	vectorStr := vectorToString(embedding.Embedding)
	// 使用 fmt.Sprintf 构建完整 SQL，避免 GORM 参数绑定问题
	// 使用 vector 类型（通过 search_path=manager 自动解析）
	sql := fmt.Sprintf(`
		UPDATE manager.embeddings
		SET engine_id = ?, bucket = ?, path = ?, name = ?, fingerprint = ?, data_updated_at = ?,
		    embedding = '%s'::vector, modality = ?, model = ?, file_size = ?, content_type = ?,
		    metadata = ?, tenant_id = ?, updated_at = ?
		WHERE id = ?
	`, vectorStr)

	err = r.db.WithContext(ctx).Exec(sql,
		embedding.EngineID,
		embedding.Bucket,
		embedding.Path,
		embedding.Name,
		embedding.Fingerprint,
		embedding.DataUpdatedAt,
		// vectorStr 已经在 SQL 中，这里移除
		embedding.Modality,
		embedding.Model,
		embedding.FileSize,
		embedding.ContentType,
		embedding.Metadata,
		embedding.TenantID,
		time.Now(),
		existing.ID,
	).Error

	if err != nil {
		log.Printf("[ERROR] 向量更新失败: %v", err)
		return err
	}

	log.Printf("[DEBUG] 向量更新成功: id=%d", existing.ID)
	return nil
}

// BatchUpsertEmbeddings 批量插入或更新向量
func (r *EmbeddingRepository) BatchUpsertEmbeddings(ctx context.Context, embeddings []*models.Embedding) error {
	if len(embeddings) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, emb := range embeddings {
			if err := r.UpsertEmbedding(ctx, emb); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetByFingerprint 根据指纹查询向量（用于去重检查）
// 注意：不查询 embedding 字段，避免 pgvector 类型 Scan 错误
func (r *EmbeddingRepository) GetByFingerprint(ctx context.Context, fingerprint string, modality string) (*models.Embedding, error) {
	var emb models.Embedding
	err := r.db.WithContext(ctx).
		Select("id", "engine_id", "bucket", "path", "name", "fingerprint", "data_updated_at", "modality", "model", "file_size", "content_type", "metadata", "tenant_id", "created_at", "updated_at").
		Where("fingerprint = ? AND modality = ?", fingerprint, modality).
		First(&emb).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // 未找到记录，返回 nil
	}
	return &emb, err
}

// GetEmbedding 查询单个向量
func (r *EmbeddingRepository) GetEmbedding(ctx context.Context, engineID uint, bucket, path, name string, modality string) (*models.Embedding, error) {
	var emb models.Embedding
	err := r.db.WithContext(ctx).
		Where("engine_id = ? AND bucket = ? AND path = ? AND name = ? AND modality = ?",
			engineID, bucket, path, name, modality).
		First(&emb).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &emb, err
}

// QuerySimilar 相似度检索
// 返回与查询向量最相似的 topK 个结果
func (r *EmbeddingRepository) QuerySimilar(ctx context.Context, queryVector []float32, modality string, topK int, maxDistance float64, tenantID *uint) ([]*models.Embedding, error) {
	query := r.db.WithContext(ctx).
		Select("*, embedding <=> ? as distance", vectorToString(queryVector)).
		Where("modality = ?", modality)

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	if maxDistance > 0 {
		query = query.Where("embedding <=> ? <= ?", vectorToString(queryVector), maxDistance)
	}

	var results []*models.Embedding
	err := query.
		Order("distance").
		Limit(topK).
		Find(&results).Error

	return results, err
}

// DeleteEmbedding 删除向量
func (r *EmbeddingRepository) DeleteEmbedding(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.Embedding{}, id).Error
}

// DeleteEmbeddingsByFingerprints 批量删除向量（按指纹）
func (r *EmbeddingRepository) DeleteEmbeddingsByFingerprints(ctx context.Context, fingerprints []string) error {
	if len(fingerprints) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("fingerprint IN ?", fingerprints).
		Delete(&models.Embedding{}).Error
}

// DeleteEmbeddingsByEngine 删除指定引擎的所有向量
func (r *EmbeddingRepository) DeleteEmbeddingsByEngine(ctx context.Context, engineID uint, bucket string) error {
	query := r.db.WithContext(ctx).Where("engine_id = ?", engineID)
	if bucket != "" {
		query = query.Where("bucket = ?", bucket)
	}
	return query.Delete(&models.Embedding{}).Error
}

// CountEmbeddings 统计向量数量
func (r *EmbeddingRepository) CountEmbeddings(ctx context.Context, engineID uint, bucket string, modality string) (int64, error) {
	query := r.db.WithContext(ctx).Model(&models.Embedding{})

	if engineID > 0 {
		query = query.Where("engine_id = ?", engineID)
	}
	if bucket != "" {
		query = query.Where("bucket = ?", bucket)
	}
	if modality != "" {
		query = query.Where("modality = ?", modality)
	}

	var count int64
	err := query.Count(&count).Error
	return count, err
}

// FindOutdatedEmbeddings 查找过期向量
// 对象的 data_updated_at 晚于向量的 data_updated_at，表示对象已修改，向量需要更新
func (r *EmbeddingRepository) FindOutdatedEmbeddings(ctx context.Context, engineID uint, bucket string, objectUpdatedAt time.Time) ([]*models.Embedding, error) {
	var results []*models.Embedding
	err := r.db.WithContext(ctx).
		Where("engine_id = ? AND bucket = ?", engineID, bucket).
		Where("data_updated_at < ?", objectUpdatedAt).
		Find(&results).Error

	return results, err
}

// GetEmbeddingStatus 获取向量化状态
func (r *EmbeddingRepository) GetEmbeddingStatus(ctx context.Context, engineID uint, bucket string) (*models.EmbeddingStatus, error) {
	status := &models.EmbeddingStatus{
		EngineID: engineID,
		Bucket:   bucket,
	}

	// 统计总向量数
	count, err := r.CountEmbeddings(ctx, engineID, bucket, "")
	if err != nil {
		return nil, err
	}
	status.VectorizedCount = int(count)

	// 查询最后向量化时间
	var lastEmb models.Embedding
	err = r.db.WithContext(ctx).
		Where("engine_id = ? AND bucket = ?", engineID, bucket).
		Order("created_at DESC").
		First(&lastEmb).Error

	if err == nil {
		status.LastVectorizedAt = &lastEmb.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return status, nil
}

// vectorToString 将向量转换为 PostgreSQL 向量字符串
func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}

	result := "["
	for i, val := range v {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%f", val)
	}
	result += "]"
	return result
}

// ===== 任务定义相关方法 =====

// CreateEmbeddingTask 创建向量化任务定义
func (r *EmbeddingRepository) CreateEmbeddingTask(ctx context.Context, task *models.EmbeddingTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

// GetEmbeddingTask 根据 ID 查询任务定义
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

// ListEmbeddingTasks 分页查询任务定义列表
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

// UpdateEmbeddingTask 更新任务定义
func (r *EmbeddingRepository) UpdateEmbeddingTask(ctx context.Context, task *models.EmbeddingTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

// DeleteEmbeddingTask 软删除任务定义
func (r *EmbeddingRepository) DeleteEmbeddingTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.EmbeddingTask{}).Error
}

// UpdateEmbeddingTaskLastExecution 回写最近执行信息
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
