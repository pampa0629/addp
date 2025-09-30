package repository

import (
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type DataSourceRepository struct {
	db *gorm.DB
}

func NewDataSourceRepository(db *gorm.DB) *DataSourceRepository {
	return &DataSourceRepository{db: db}
}

func (r *DataSourceRepository) Create(ds *models.DataSource) error {
	return r.db.Create(ds).Error
}

func (r *DataSourceRepository) List(page, pageSize int) ([]models.DataSource, error) {
	var dataSources []models.DataSource
	offset := (page - 1) * pageSize

	err := r.db.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&dataSources).Error
	return dataSources, err
}

func (r *DataSourceRepository) GetByID(id uint) (*models.DataSource, error) {
	var ds models.DataSource
	err := r.db.First(&ds, id).Error
	if err != nil {
		return nil, err
	}
	return &ds, nil
}

func (r *DataSourceRepository) GetBySystemResourceID(systemResourceID uint) (*models.DataSource, error) {
	var ds models.DataSource
	err := r.db.Where("system_resource_id = ?", systemResourceID).First(&ds).Error
	if err != nil {
		return nil, err
	}
	return &ds, nil
}

func (r *DataSourceRepository) Update(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.DataSource{}).Where("id = ?", id).Updates(updates).Error
}

func (r *DataSourceRepository) Delete(id uint) error {
	return r.db.Delete(&models.DataSource{}, id).Error
}

func (r *DataSourceRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.DataSource{}).Count(&count).Error
	return count, err
}