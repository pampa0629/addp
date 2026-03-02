package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type ApplicationRepository struct {
	db *gorm.DB
}

func NewApplicationRepository(db *gorm.DB) *ApplicationRepository {
	return &ApplicationRepository{db: db}
}

// Create 创建应用
func (r *ApplicationRepository) Create(app *models.Application) error {
	return r.db.Create(app).Error
}

// FindByID 根据 ID 查询应用
func (r *ApplicationRepository) FindByID(id uint) (*models.Application, error) {
	var app models.Application
	err := r.db.Preload("APIKeys", "status = ?", "active").
		First(&app, id).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &app, nil
}

// FindByTenantID 根据租户 ID 查询应用列表
func (r *ApplicationRepository) FindByTenantID(tenantID uint) ([]models.Application, error) {
	var apps []models.Application
	err := r.db.Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Order("created_at DESC").
		Find(&apps).Error
	return apps, err
}

// Update 更新应用
func (r *ApplicationRepository) Update(app *models.Application) error {
	return r.db.Save(app).Error
}

// Delete 软删除应用
func (r *ApplicationRepository) Delete(id uint) error {
	return r.db.Model(&models.Application{}).
		Where("id = ?", id).
		Update("deleted_at", gorm.Expr("NOW()")).Error
}

// CreateAPIKey 创建 API Key
func (r *ApplicationRepository) CreateAPIKey(apiKey *models.APIKey) error {
	return r.db.Create(apiKey).Error
}

// FindAPIKeyByHash 根据 Hash 查询 API Key
func (r *ApplicationRepository) FindAPIKeyByHash(keyHash string) (*models.APIKey, error) {
	var apiKey models.APIKey
	err := r.db.Preload("Application").
		Where("key_hash = ? AND status = ?", keyHash, "active").
		First(&apiKey).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &apiKey, nil
}

// FindAPIKeysByAppID 根据应用 ID 查询 API Key 列表
func (r *ApplicationRepository) FindAPIKeysByAppID(appID uint) ([]models.APIKey, error) {
	var keys []models.APIKey
	err := r.db.Where("application_id = ?", appID).
		Order("created_at DESC").
		Find(&keys).Error
	return keys, err
}

// UpdateAPIKeyLastUsed 更新 API Key 最后使用时间
func (r *ApplicationRepository) UpdateAPIKeyLastUsed(id uint) error {
	return r.db.Model(&models.APIKey{}).
		Where("id = ?", id).
		Update("last_used_at", gorm.Expr("NOW()")).Error
}

// RevokeAPIKey 撤销 API Key
func (r *ApplicationRepository) RevokeAPIKey(id uint, revokedBy uint) error {
	return r.db.Model(&models.APIKey{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     "revoked",
			"revoked_at": gorm.Expr("NOW()"),
			"revoked_by": revokedBy,
		}).Error
}

// FindAPIKeyWithApp 根据 Key Hash 查询 API Key 及关联的应用信息
func (r *ApplicationRepository) FindAPIKeyWithApp(keyHash string) (*models.APIKey, *models.Application, error) {
	var apiKey models.APIKey
	err := r.db.Where("key_hash = ? AND status = ?", keyHash, "active").
		First(&apiKey).Error
	if err != nil {
		return nil, nil, commonrepo.WrapDBError(err)
	}

	var app models.Application
	err = r.db.First(&app, apiKey.ApplicationID).Error
	if err != nil {
		return nil, nil, commonrepo.WrapDBError(err)
	}

	return &apiKey, &app, nil
}
