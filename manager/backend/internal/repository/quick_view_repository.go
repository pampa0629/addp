package repository

import (
	"fmt"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// QuickViewRepository 快显数据访问层
type QuickViewRepository struct {
	db *gorm.DB
}

// NewQuickViewRepository 创建快显仓储
func NewQuickViewRepository(db *gorm.DB) *QuickViewRepository {
	return &QuickViewRepository{db: db}
}

// GetDB 获取数据库连接（用于复杂查询）
func (r *QuickViewRepository) GetDB() *gorm.DB {
	return r.db
}

// Create 创建快显记录
func (r *QuickViewRepository) Create(qv *models.QuickView) error {
	return r.db.Create(qv).Error
}

// GetByTable 根据表信息获取快显记录
func (r *QuickViewRepository) GetByTable(tenantID, engineID uint, schema, table string) (*models.QuickView, error) {
	var qv models.QuickView
	err := r.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		tenantID, engineID, schema, table).
		First(&qv).Error

	if err != nil {
		return nil, err
	}

	return &qv, nil
}

// GetByID 根据ID获取快显记录
func (r *QuickViewRepository) GetByID(id uint) (*models.QuickView, error) {
	var qv models.QuickView
	err := r.db.First(&qv, id).Error
	if err != nil {
		return nil, err
	}
	return &qv, nil
}

// Update 更新快显记录
// Deprecated: 使用 UpdateStatusOnly, UpdateGenerationResult 等专用方法以确保字段保护
func (r *QuickViewRepository) Update(qv *models.QuickView) error {
	return r.db.Save(qv).Error
}

// UpdateStatusOnly 仅更新 status 和 error_message，保护所有其他字段
func (r *QuickViewRepository) UpdateStatusOnly(id uint, status, errorMsg string) error {
	updates := map[string]interface{}{
		"status": status,
	}

	if errorMsg != "" {
		updates["error_message"] = errorMsg
	} else {
		updates["error_message"] = nil // 清空错误信息（使用 nil 而不是空字符串）
	}

	return r.db.Model(&models.QuickView{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateStatus 更新状态（保留向后兼容）
// Deprecated: 使用 UpdateStatusOnly 以确保字段保护
func (r *QuickViewRepository) UpdateStatus(id uint, status, errorMsg string) error {
	return r.UpdateStatusOnly(id, status, errorMsg)
}

// UpdateGenerationResult 更新预缓存结果，保护 preparation_status 和 preferred_mode
func (r *QuickViewRepository) UpdateGenerationResult(id uint, result UpdateResultParams) error {
	updates := map[string]interface{}{
		"status":          "ready",
		"actual_max_zoom": result.ActualMaxZoom,
		"total_tiles":     result.TotalTiles,
		"cached_tiles":    result.CachedTiles,
		"completed_at":    gorm.Expr("NOW()"),
	}

	return r.db.Model(&models.QuickView{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateGenerationResultWithPreferredMode 预缓存完成后，同时设置 preferred_mode = "mvt"
func (r *QuickViewRepository) UpdateGenerationResultWithPreferredMode(id uint, result UpdateResultParams) error {
	updates := map[string]interface{}{
		"status":          "ready",
		"actual_max_zoom": result.ActualMaxZoom,
		"total_tiles":     result.TotalTiles,
		"cached_tiles":    result.CachedTiles,
		"completed_at":    gorm.Expr("NOW()"),
		"preferred_mode":  "mvt", // 第三步：自动启用 MVT
	}

	return r.db.Model(&models.QuickView{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateResult 更新生成结果（保留向后兼容）
// Deprecated: 使用 UpdateGenerationResult 或 UpdateGenerationResultWithPreferredMode
func (r *QuickViewRepository) UpdateResult(id uint, result UpdateResultParams) error {
	return r.UpdateGenerationResult(id, result)
}

// UpdateResultParams 更新结果参数
type UpdateResultParams struct {
	ActualMaxZoom     int
	TotalTiles        int
	CachedTiles       int
}

// ListAll 列出所有快显任务
func (r *QuickViewRepository) ListAll(tenantID uint, params ListParams) ([]models.QuickView, int64, error) {
	var qvs []models.QuickView
	var total int64

	query := r.db.Model(&models.QuickView{}).Where("tenant_id = ?", tenantID)

	// 状态过滤
	if params.Status != "" {
		query = query.Where("status = ?", params.Status)
	}

	// 资源过滤
	if params.EngineID != 0 {
		query = query.Where("engine_id = ?", params.EngineID)
	}

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 排序和分页
	offset := (params.Page - 1) * params.PageSize
	err := query.Order("updated_at DESC").
		Offset(offset).
		Limit(params.PageSize).
		Find(&qvs).Error

	if err != nil {
		return nil, 0, err
	}

	return qvs, total, nil
}

// ListParams 列表参数
type ListParams struct {
	Status   string
	EngineID uint
	Page     int
	PageSize int
}

// GetStatistics 获取统计信息
func (r *QuickViewRepository) GetStatistics(tenantID uint) (*Statistics, error) {
	var stats Statistics

	// 总任务数
	r.db.Model(&models.QuickView{}).
		Where("tenant_id = ?", tenantID).
		Count(&stats.Total)

	// 各状态任务数
	r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND status = ?", tenantID, "generating").
		Count(&stats.Generating)

	r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND status = ?", tenantID, "ready").
		Count(&stats.Ready)

	r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND status = ?", tenantID, "failed").
		Count(&stats.Failed)

	return &stats, nil
}

// Statistics 统计信息
type Statistics struct {
	Total      int64 `json:"total"`
	Generating int64 `json:"generating"`
	Ready      int64 `json:"ready"`
	Failed     int64 `json:"failed"`
}

// Delete 删除快显记录
func (r *QuickViewRepository) Delete(id uint) error {
	return r.db.Delete(&models.QuickView{}, id).Error
}

// DeleteByTable 根据表信息删除快显记录
func (r *QuickViewRepository) DeleteByTable(tenantID, engineID uint, schema, table string) error {
	return r.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		tenantID, engineID, schema, table).
		Delete(&models.QuickView{}).Error
}

// IncrementCachedTiles 增加缓存瓦片数（用于按需生成时更新统计）
func (r *QuickViewRepository) IncrementCachedTiles(
	tenantID, engineID uint,
	schema, table string,
) error {
	// 原子性更新
	return r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
			tenantID, engineID, schema, table).
		Updates(map[string]interface{}{
			"cached_tiles": gorm.Expr("cached_tiles + 1"),
		}).Error
}

// Exists 检查快显记录是否存在
func (r *QuickViewRepository) Exists(tenantID, engineID uint, schema, table string) (bool, error) {
	var count int64
	err := r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
			tenantID, engineID, schema, table).
		Count(&count).Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// IsGenerating 检查是否正在生成
func (r *QuickViewRepository) IsGenerating(tenantID, engineID uint, schema, table string) (bool, error) {
	var count int64
	err := r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ? AND status = ?",
			tenantID, engineID, schema, table, "generating").
		Count(&count).Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// GetFingerprint 获取指纹（如果记录存在）
func (r *QuickViewRepository) GetFingerprint(tenantID, engineID uint, schema, table string) (string, error) {
	var qv models.QuickView
	err := r.db.Select("fingerprint").
		Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
			tenantID, engineID, schema, table).
		First(&qv).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", fmt.Errorf("quick view not found")
		}
		return "", err
	}

	return qv.Fingerprint, nil
}

// UpdatePreferredMode 更新用户偏好的显示模式
func (r *QuickViewRepository) UpdatePreferredMode(
	tenantID, engineID uint,
	schema, table, mode string,
) error {
	result := r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
			tenantID, engineID, schema, table).
		Update("preferred_mode", mode)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	return nil
}

// UpdatePreparationStatusAtomic 原子性更新准备状态和 quick_view.status
func (r *QuickViewRepository) UpdatePreparationStatusAtomic(
	id uint,
	prepStatus *models.PreparationStatus,
	qvStatus string,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.QuickView{}).
			Where("id = ?", id).
			Update("preparation_status", prepStatus).Error; err != nil {
			return err
		}
		return tx.Model(&models.QuickView{}).
			Where("id = ?", id).
			Update("status", qvStatus).Error
	})
}

// UpdatePreparationStatus 更新准备阶段状态（保留向后兼容）
// Deprecated: 使用 UpdatePreparationStatusAtomic 确保原子性更新
func (r *QuickViewRepository) UpdatePreparationStatus(
	id uint,
	status *models.PreparationStatus,
) error {
	return r.db.Model(&models.QuickView{}).
		Where("id = ?", id).
		Update("preparation_status", status).Error
}

// GetPreparationResult 获取准备结果（必须存在）
func (r *QuickViewRepository) GetPreparationResult(
	tenantID, engineID uint,
	schema, table string,
) (*models.PreparationStatus, error) {
	var qv models.QuickView
	err := r.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		tenantID, engineID, schema, table).
		First(&qv).Error

	if err != nil {
		return nil, err
	}

	if qv.PreparationStatus == nil {
		return nil, fmt.Errorf("preparation not completed")
	}

	return qv.PreparationStatus, nil
}

// IsPreparationCompleted 检查准备是否完成
func (r *QuickViewRepository) IsPreparationCompleted(
	tenantID, engineID uint,
	schema, table string,
) (bool, error) {
	var qv models.QuickView
	err := r.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		tenantID, engineID, schema, table).
		First(&qv).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}

	return qv.PreparationStatus != nil && qv.PreparationStatus.OverallStatus == "passed", nil
}

// GetPreparationStatus 获取准备阶段状态（保留向后兼容）
// Deprecated: 使用 GetPreparationResult 或 IsPreparationCompleted
func (r *QuickViewRepository) GetPreparationStatus(
	tenantID, engineID uint,
	schema, table string,
) (*models.PreparationStatus, error) {
	var qv models.QuickView
	err := r.db.Select("preparation_status").
		Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
			tenantID, engineID, schema, table).
		First(&qv).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("quick view not found")
		}
		return nil, err
	}

	return qv.PreparationStatus, nil
}

