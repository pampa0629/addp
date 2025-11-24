package repository

import (
	"context"
	"lineage/models"

	"gorm.io/gorm"
)

// LineageRepository 血缘数据访问层
type LineageRepository struct {
	db *gorm.DB
}

// NewLineageRepository 创建 Repository 实例
func NewLineageRepository(db *gorm.DB) *LineageRepository {
	return &LineageRepository{db: db}
}

// Create 创建血缘记录
func (r *LineageRepository) Create(ctx context.Context, lineage *models.DataLineage) (*models.DataLineage, error) {
	if err := r.db.WithContext(ctx).Create(lineage).Error; err != nil {
		return nil, err
	}
	return lineage, nil
}

// CreateBatch 批量创建
func (r *LineageRepository) CreateBatch(ctx context.Context, lineages []*models.DataLineage) error {
	return r.db.WithContext(ctx).Create(lineages).Error
}

// FindByID 根据 ID 查询
func (r *LineageRepository) FindByID(ctx context.Context, id uint) (*models.DataLineage, error) {
	var lineage models.DataLineage
	err := r.db.WithContext(ctx).First(&lineage, id).Error
	if err != nil {
		return nil, err
	}
	return &lineage, nil
}

// FindByTargetItemID 查询上游血缘（数据来源）
func (r *LineageRepository) FindByTargetItemID(ctx context.Context, itemID uint) ([]*models.DataLineage, error) {
	var lineages []*models.DataLineage
	err := r.db.WithContext(ctx).
		Where("target_item_id = ?", itemID).
		Order("created_at DESC").
		Find(&lineages).Error
	return lineages, err
}

// FindBySourceItemID 查询下游血缘（数据去向）
func (r *LineageRepository) FindBySourceItemID(ctx context.Context, itemID uint) ([]*models.DataLineage, error) {
	var lineages []*models.DataLineage
	err := r.db.WithContext(ctx).
		Where("source_item_id = ?", itemID).
		Order("created_at DESC").
		Find(&lineages).Error
	return lineages, err
}

// FindByExternalWorkflowID 根据外部工作流 ID 查询
func (r *LineageRepository) FindByExternalWorkflowID(ctx context.Context, workflowID string) ([]*models.DataLineage, error) {
	var lineages []*models.DataLineage
	err := r.db.WithContext(ctx).
		Where("external_workflow_id = ?", workflowID).
		Order("created_at DESC").
		Find(&lineages).Error
	return lineages, err
}

// FindByExternalExecutionID 根据外部执行 ID 查询
func (r *LineageRepository) FindByExternalExecutionID(ctx context.Context, executionID string) ([]*models.DataLineage, error) {
	var lineages []*models.DataLineage
	err := r.db.WithContext(ctx).
		Where("external_execution_id = ?", executionID).
		Order("created_at DESC").
		Find(&lineages).Error
	return lineages, err
}

// Query 灵活查询（支持多条件组合）
func (r *LineageRepository) Query(ctx context.Context, query *models.LineageQuery) ([]*models.DataLineage, error) {
	db := r.db.WithContext(ctx)

	// 按数据项查询
	if query.SourceItemID != nil {
		db = db.Where("source_item_id = ?", *query.SourceItemID)
	}
	if query.TargetItemID != nil {
		db = db.Where("target_item_id = ?", *query.TargetItemID)
	}

	// 按工作流查询
	if query.ExternalWorkflowID != "" {
		db = db.Where("external_workflow_id = ?", query.ExternalWorkflowID)
	}
	if query.ExternalExecutionID != "" {
		db = db.Where("external_execution_id = ?", query.ExternalExecutionID)
	}
	if query.WorkflowEngine != "" {
		db = db.Where("workflow_engine = ?", query.WorkflowEngine)
	}

	// 按时间范围查询
	if query.StartTimeFrom != nil {
		db = db.Where("start_time >= ?", *query.StartTimeFrom)
	}
	if query.StartTimeTo != nil {
		db = db.Where("start_time <= ?", *query.StartTimeTo)
	}

	// 按状态查询
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}

	// 按类型查询
	if query.LineageType != "" {
		db = db.Where("lineage_type = ?", query.LineageType)
	}
	if query.SourceType != "" {
		db = db.Where("source_type = ?", query.SourceType)
	}
	if query.TargetType != "" {
		db = db.Where("target_type = ?", query.TargetType)
	}

	// 分页
	if query.Limit > 0 {
		db = db.Limit(query.Limit)
	} else {
		db = db.Limit(100) // 默认限制 100 条
	}
	if query.Offset > 0 {
		db = db.Offset(query.Offset)
	}

	var lineages []*models.DataLineage
	err := db.Order("created_at DESC").Find(&lineages).Error
	return lineages, err
}

// Count 统计记录数
func (r *LineageRepository) Count(ctx context.Context, query *models.LineageQuery) (int64, error) {
	db := r.db.WithContext(ctx).Model(&models.DataLineage{})

	// 应用查询条件（与 Query 方法相同的逻辑）
	if query.SourceItemID != nil {
		db = db.Where("source_item_id = ?", *query.SourceItemID)
	}
	if query.TargetItemID != nil {
		db = db.Where("target_item_id = ?", *query.TargetItemID)
	}
	// ... 其他条件

	var count int64
	err := db.Count(&count).Error
	return count, err
}

// Delete 删除血缘记录（软删除）
func (r *LineageRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.DataLineage{}, id).Error
}
