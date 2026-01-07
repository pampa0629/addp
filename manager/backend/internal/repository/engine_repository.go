package repository

import (
	"github.com/addp/common/utils"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type EngineRepository struct {
	db            *gorm.DB
	encryptionKey []byte
}

func NewEngineRepository(db *gorm.DB, encryptionKey []byte) *EngineRepository {
	return &EngineRepository{
		db:            db,
		encryptionKey: encryptionKey,
	}
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

	// 解密所有引擎的连接信息
	for i := range engines {
		if err := r.decryptConnectionInfo(&engines[i]); err != nil {
			return nil, err
		}
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

	// 解密所有引擎的连接信息
	for i := range engines {
		if err := r.decryptConnectionInfo(&engines[i]); err != nil {
			return nil, 0, err
		}
	}

	return engines, total, nil
}

// GetByID 根据ID获取引擎
func (r *EngineRepository) GetByID(id uint) (*models.Engine, error) {
	var engine models.Engine
	if err := r.db.First(&engine, id).Error; err != nil {
		return nil, err
	}

	// 解密连接信息
	if err := r.decryptConnectionInfo(&engine); err != nil {
		return nil, err
	}

	return &engine, nil
}

// decryptConnectionInfo 解密引擎连接信息中的敏感字段
func (r *EngineRepository) decryptConnectionInfo(engine *models.Engine) error {
	if len(r.encryptionKey) == 0 {
		// 没有配置加密密钥，跳过解密
		return nil
	}

	decrypted := make(models.ConnectionInfo)
	for k, v := range engine.ConnectionInfo {
		decrypted[k] = v
	}

	// 定义需要解密的敏感字段
	sensitiveFields := []string{"password", "access_key", "secret_key", "token", "api_key"}

	for _, field := range sensitiveFields {
		if val, exists := engine.ConnectionInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				decryptedVal, err := utils.Decrypt(strVal, r.encryptionKey)
				if err != nil {
					// 如果解密失败，可能是未加密的旧数据，保持原值
					decrypted[field] = strVal
					continue
				}
				decrypted[field] = decryptedVal
			}
		}
	}

	engine.ConnectionInfo = decrypted
	return nil
}
