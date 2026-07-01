package repository

import (
	"context"
	"errors"
	"strings"

	commonapi "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	commonrepo "github.com/addp/common/repository"
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

func (r *QuickViewRepository) GetByIdentity(tenantID uint, itemFingerprint string, locator string) (*models.QuickView, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, commonapi.ErrNotFound
	}
	var qv models.QuickView
	err := r.db.Where("tenant_id = ? AND item_fingerprint = ?", tenantID, itemFingerprint).First(&qv).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &qv, nil
}

func (r *QuickViewRepository) EnsurePreference(qv *models.QuickView) error {
	if qv == nil {
		return nil
	}
	existing, err := r.GetByIdentity(qv.TenantID, qv.ItemFingerprint, qv.Locator)
	if err != nil {
		if err == commonapi.ErrNotFound {
			if strings.TrimSpace(qv.PreferredMode) == "" {
				qv.PreferredMode = models.QuickViewPreferredModeBasicPreview
			}
			return r.db.Create(qv).Error
		}
		return err
	}
	if strings.TrimSpace(qv.PreferredMode) == "" || strings.TrimSpace(qv.PreferredMode) == existing.PreferredMode {
		return nil
	}
	return r.db.Model(&models.QuickView{}).Where("id = ?", existing.ID).Update("preferred_mode", qv.PreferredMode).Error
}

// GetByID 根据ID获取快显记录
func (r *QuickViewRepository) GetByID(id uint) (*models.QuickView, error) {
	var qv models.QuickView
	err := r.db.First(&qv, id).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &qv, nil
}

// Update 更新快显记录
// Deprecated: 使用 UpdateStatusOnly, UpdateGenerationResult 等专用方法以确保字段保护
func (r *QuickViewRepository) Update(qv *models.QuickView) error {
	return r.db.Save(qv).Error
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

	r.db.Model(&models.QuickView{}).
		Where("tenant_id = ?", tenantID).
		Count(&stats.Total)

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

func (r *QuickViewRepository) ListQuickViews(ctx context.Context, tenantID uint) ([]*models.QuickView, error) {
	var results []*models.QuickView
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&results).Error
	return results, err
}

func (r *QuickViewRepository) DeleteByTenantAndFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ?", tenantID, strings.TrimSpace(itemFingerprint)).
		Delete(&models.QuickView{}).Error
}

// UpdatePreferredMode 更新用户偏好的显示模式
func (r *QuickViewRepository) UpdatePreferredMode(
	tenantID uint,
	itemFingerprint string,
	locator string,
	mode string,
) error {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return errors.New("quick view item_fingerprint is required")
	}
	updates := map[string]interface{}{
		"preferred_mode": mode,
		"locator":        strings.TrimSpace(locator),
	}
	result := r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND item_fingerprint = ?", tenantID, itemFingerprint).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return r.db.Create(&models.QuickView{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		Locator:         strings.TrimSpace(locator),
		PreferredMode:   mode,
	}).Error
}

func (r *QuickViewRepository) UpdateViewState(
	tenantID uint,
	itemFingerprint string,
	locator string,
	viewState commonModels.JSONMap,
) error {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return errors.New("quick view item_fingerprint is required")
	}
	if viewState == nil {
		viewState = commonModels.JSONMap{}
	}
	updates := map[string]interface{}{
		"view_state": viewState,
		"locator":    strings.TrimSpace(locator),
	}
	result := r.db.Model(&models.QuickView{}).
		Where("tenant_id = ? AND item_fingerprint = ?", tenantID, itemFingerprint).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return r.db.Create(&models.QuickView{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		Locator:         strings.TrimSpace(locator),
		PreferredMode:   models.QuickViewPreferredModeBasicPreview,
		ViewState:       viewState,
	}).Error
}
