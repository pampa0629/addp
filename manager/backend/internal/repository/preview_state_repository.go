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

// PreviewStateRepository 维护数据项预览模式偏好和交互视角状态。
type PreviewStateRepository struct {
	db *gorm.DB
}

func NewPreviewStateRepository(db *gorm.DB) *PreviewStateRepository {
	return &PreviewStateRepository{db: db}
}

// GetDB 获取数据库连接（用于复杂查询）
func (r *PreviewStateRepository) GetDB() *gorm.DB {
	return r.db
}

func (r *PreviewStateRepository) Create(state *models.PreviewState) error {
	return r.db.Create(state).Error
}

func (r *PreviewStateRepository) GetByIdentity(tenantID uint, itemFingerprint string, locator string) (*models.PreviewState, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, commonapi.ErrNotFound
	}
	var state models.PreviewState
	err := r.db.Where("tenant_id = ? AND item_fingerprint = ?", tenantID, itemFingerprint).First(&state).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &state, nil
}

func (r *PreviewStateRepository) EnsurePreference(state *models.PreviewState) error {
	if state == nil {
		return nil
	}
	existing, err := r.GetByIdentity(state.TenantID, state.ItemFingerprint, state.Locator)
	if err != nil {
		if err == commonapi.ErrNotFound {
			if strings.TrimSpace(state.PreferredMode) == "" {
				state.PreferredMode = models.PreviewModeBasicPreview
			}
			return r.db.Create(state).Error
		}
		return err
	}
	if strings.TrimSpace(state.PreferredMode) == "" || strings.TrimSpace(state.PreferredMode) == existing.PreferredMode {
		return nil
	}
	return r.db.Model(&models.PreviewState{}).Where("id = ?", existing.ID).Update("preferred_mode", state.PreferredMode).Error
}

func (r *PreviewStateRepository) GetByID(id uint) (*models.PreviewState, error) {
	var state models.PreviewState
	err := r.db.First(&state, id).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &state, nil
}

func (r *PreviewStateRepository) Update(state *models.PreviewState) error {
	return r.db.Save(state).Error
}

// ListParams 列表参数
type ListParams struct {
	Status   string
	EngineID uint
	Page     int
	PageSize int
}

// GetStatistics 获取统计信息
func (r *PreviewStateRepository) GetStatistics(tenantID uint) (*Statistics, error) {
	var stats Statistics

	r.db.Model(&models.PreviewState{}).
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

func (r *PreviewStateRepository) Delete(id uint) error {
	return r.db.Delete(&models.PreviewState{}, id).Error
}

func (r *PreviewStateRepository) ListPreviewStates(ctx context.Context, tenantID uint) ([]*models.PreviewState, error) {
	var results []*models.PreviewState
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("updated_at DESC, id DESC").
		Find(&results).Error
	return results, err
}

func (r *PreviewStateRepository) DeleteByTenantAndFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ?", tenantID, strings.TrimSpace(itemFingerprint)).
		Delete(&models.PreviewState{}).Error
}

// UpdatePreferredMode 更新用户偏好的显示模式
func (r *PreviewStateRepository) UpdatePreferredMode(
	tenantID uint,
	itemFingerprint string,
	locator string,
	mode string,
) error {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return errors.New("preview state item_fingerprint is required")
	}
	updates := map[string]interface{}{
		"preferred_mode": mode,
		"locator":        strings.TrimSpace(locator),
	}
	result := r.db.Model(&models.PreviewState{}).
		Where("tenant_id = ? AND item_fingerprint = ?", tenantID, itemFingerprint).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return r.db.Create(&models.PreviewState{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		Locator:         strings.TrimSpace(locator),
		PreferredMode:   mode,
	}).Error
}

func (r *PreviewStateRepository) UpdateViewState(
	tenantID uint,
	itemFingerprint string,
	locator string,
	viewState commonModels.JSONMap,
) error {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return errors.New("preview state item_fingerprint is required")
	}
	if viewState == nil {
		viewState = commonModels.JSONMap{}
	}
	updates := map[string]interface{}{
		"view_state": viewState,
		"locator":    strings.TrimSpace(locator),
	}
	result := r.db.Model(&models.PreviewState{}).
		Where("tenant_id = ? AND item_fingerprint = ?", tenantID, itemFingerprint).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return r.db.Create(&models.PreviewState{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		Locator:         strings.TrimSpace(locator),
		PreferredMode:   models.PreviewModeBasicPreview,
		ViewState:       viewState,
	}).Error
}
