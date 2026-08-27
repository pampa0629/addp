package repository

import (
	"errors"

	"github.com/addp/workbench/internal/models"
	"gorm.io/gorm"
)

var (
	ErrViewNotFound        = errors.New("workbench view not found")
	ErrViewVersionConflict = errors.New("workbench view version conflict")
)

type ViewRepository struct{ db *gorm.DB }

func NewViewRepository(db *gorm.DB) *ViewRepository { return &ViewRepository{db: db} }

func (r *ViewRepository) List(tenantID, ownerUserID int64, offset, limit int) ([]models.View, int64, error) {
	query := r.db.Model(&models.View{}).Where("tenant_id = ? AND owner_user_id = ?", tenantID, ownerUserID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	views := make([]models.View, 0)
	if err := query.Order("updated_at DESC, id ASC").Offset(offset).Limit(limit).Find(&views).Error; err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

func (r *ViewRepository) Get(tenantID, ownerUserID int64, id string) (*models.View, error) {
	var view models.View
	if err := r.db.Where("tenant_id = ? AND owner_user_id = ? AND id = ?", tenantID, ownerUserID, id).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrViewNotFound
		}
		return nil, err
	}
	return &view, nil
}

func (r *ViewRepository) Create(view *models.View) error { return r.db.Create(view).Error }

func (r *ViewRepository) Update(view *models.View, expectedVersion int64) error {
	result := r.db.Model(&models.View{}).
		Where("tenant_id = ? AND owner_user_id = ? AND id = ? AND version = ?", view.TenantID, view.OwnerUserID, view.ID, expectedVersion).
		Updates(map[string]interface{}{
			"name": view.Name, "description": view.Description,
			"contract_fingerprint":  view.ContractFingerprint,
			"parameter_definitions": view.ParameterDefinitions, "query_template": view.QueryTemplate,
			"default_parameter_values": view.DefaultParameterValues,
			"renderer_type":            view.RendererType, "renderer_config": view.RendererConfig,
			"version": gorm.Expr("version + 1"), "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := r.db.Model(&models.View{}).Where("tenant_id = ? AND owner_user_id = ? AND id = ?", view.TenantID, view.OwnerUserID, view.ID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrViewNotFound
	}
	return ErrViewVersionConflict
}

func (r *ViewRepository) Delete(tenantID, ownerUserID int64, id string) error {
	result := r.db.Where("tenant_id = ? AND owner_user_id = ? AND id = ?", tenantID, ownerUserID, id).Delete(&models.View{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrViewNotFound
	}
	return nil
}
