package repository

import (
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type EngineRepository struct {
	db *gorm.DB
}

func NewEngineRepository(db *gorm.DB) *EngineRepository {
	return &EngineRepository{db: db}
}

// ListAllActive 获取所有激活引擎，可选按租户过滤
func (r *EngineRepository) ListAllActive(tenantID *uint) ([]models.Engine, error) {
	var engines []models.Engine
	query := r.db.Where("is_active = ?", true)

	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	if err := query.Order("id").Find(&engines).Error; err != nil {
		return nil, err
	}
	return engines, nil
}

// List 获取引擎列表
func (r *EngineRepository) List(page, pageSize int, engineType string) ([]models.Engine, int64, error) {
	var engines []models.Engine
	var total int64

	query := r.db.Model(&models.Engine{}).Where("is_active = ?", true)

	if engineType != "" {
		query = query.Where("engine_type = ?", engineType)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Find(&engines).Error; err != nil {
		return nil, 0, err
	}

	return engines, total, nil
}

// GetByID 根据ID获取引擎
func (r *EngineRepository) GetByID(id uint) (*models.Engine, error) {
	var engine models.Engine
	if err := r.db.First(&engine, id).Error; err != nil {
		return nil, err
	}
	return &engine, nil
}
