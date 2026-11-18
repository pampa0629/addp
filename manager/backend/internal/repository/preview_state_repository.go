package repository

import (
	"context"
	"errors"
	"strings"

	commonapi "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PreviewStateRepository 维护数据项预览模式偏好和交互视角状态。
type PreviewStateRepository struct {
	db *gorm.DB
}

func NewPreviewStateRepository(db *gorm.DB) *PreviewStateRepository {
	return &PreviewStateRepository{db: db}
}

func MigratePreviewStateSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('addp.manager.preview_state_schema', 0))").Error; err != nil {
			return err
		}
		return tx.Exec(migrations.PreviewStateUserScopeSQL).Error
	})
}

// GetDB 获取数据库连接（用于复杂查询）
func (r *PreviewStateRepository) GetDB() *gorm.DB {
	return r.db
}

func (r *PreviewStateRepository) GetByIdentity(ctx context.Context, tenantID, userID uint, itemFingerprint string) (*models.PreviewState, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if userID == 0 || itemFingerprint == "" {
		return nil, commonapi.ErrNotFound
	}
	var state models.PreviewState
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND user_id = ?", tenantID, itemFingerprint, userID).
		First(&state).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &state, nil
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
	ctx context.Context, token CleanupTargetToken,
	tenantID, userID uint,
	itemFingerprint string,
	locator string,
	mode string,
) error {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return errors.New("preview state item_fingerprint is required")
	}
	if token.Key != (CleanupTargetKey{TenantID: tenantID, Kind: CleanupTargetPreviewState, Identity: itemFingerprint}) {
		return commonapi.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := GuardCleanupTargetWrite(ctx, tx, token); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "item_fingerprint"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"preferred_mode", "locator", "updated_at"}),
		}).Create(&models.PreviewState{
			TenantID:        tenantID,
			UserID:          userID,
			ItemFingerprint: itemFingerprint,
			Locator:         strings.TrimSpace(locator),
			PreferredMode:   mode,
		}).Error
	})
}

func (r *PreviewStateRepository) UpdateViewState(
	ctx context.Context, token CleanupTargetToken,
	tenantID, userID uint,
	itemFingerprint string,
	locator string,
	initialPreferredMode string,
	viewState commonModels.JSONMap,
) error {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return errors.New("preview state item_fingerprint is required")
	}
	if viewState == nil {
		viewState = commonModels.JSONMap{}
	}
	if token.Key != (CleanupTargetKey{TenantID: tenantID, Kind: CleanupTargetPreviewState, Identity: itemFingerprint}) {
		return commonapi.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := GuardCleanupTargetWrite(ctx, tx, token); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "item_fingerprint"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"view_state", "locator", "updated_at"}),
		}).Create(&models.PreviewState{
			TenantID:        tenantID,
			UserID:          userID,
			ItemFingerprint: itemFingerprint,
			Locator:         strings.TrimSpace(locator),
			PreferredMode:   initialPreferredMode,
			ViewState:       viewState,
		}).Error
	})
}
