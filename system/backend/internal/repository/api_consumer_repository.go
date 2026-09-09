package repository

import (
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type APIConsumerRepository struct {
	db *gorm.DB
}

func NewAPIConsumerRepository(db *gorm.DB) *APIConsumerRepository {
	return &APIConsumerRepository{db: db}
}

func (r *APIConsumerRepository) Create(consumer *models.APIConsumer) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		grants := consumer.ServiceGrants
		consumer.ServiceGrants = nil
		if err := tx.Create(consumer).Error; err != nil {
			return err
		}
		for index := range grants {
			grants[index].APIConsumerID = consumer.ID
		}
		if len(grants) > 0 {
			if err := tx.Create(&grants).Error; err != nil {
				return err
			}
		}
		consumer.ServiceGrants = grants
		return nil
	})
}

func (r *APIConsumerRepository) FindByIDAndTenant(id, tenantID uint) (*models.APIConsumer, error) {
	var consumer models.APIConsumer
	err := r.db.Preload("ServiceGrants").
		Preload("Credentials", "status = ?", "active").
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&consumer).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &consumer, nil
}

func (r *APIConsumerRepository) FindByTenantID(tenantID uint) ([]models.APIConsumer, error) {
	var consumers []models.APIConsumer
	err := r.db.Preload("ServiceGrants").
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&consumers).Error
	return consumers, err
}

func (r *APIConsumerRepository) Update(consumer *models.APIConsumer, grants []models.APIConsumerServiceGrant) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.APIConsumer{}).
			Where("id = ? AND tenant_id = ?", consumer.ID, consumer.TenantID).
			Updates(map[string]any{
				"name": consumer.Name, "description": consumer.Description,
				"rate_limit_per_minute": consumer.RateLimitPerMinute,
				"status":                consumer.Status, "updated_at": time.Now().UTC(),
			}).Error; err != nil {
			return err
		}
		if grants == nil {
			return nil
		}
		if err := tx.Where("api_consumer_id = ?", consumer.ID).
			Delete(&models.APIConsumerServiceGrant{}).Error; err != nil {
			return err
		}
		for index := range grants {
			grants[index].APIConsumerID = consumer.ID
		}
		if len(grants) > 0 {
			return tx.Create(&grants).Error
		}
		return nil
	})
}

func (r *APIConsumerRepository) Delete(id, tenantID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.APIConsumer{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
		}
		return nil
	})
}

func (r *APIConsumerRepository) CreateCredential(credential *models.APIConsumerCredential) error {
	return r.db.Create(credential).Error
}

func (r *APIConsumerRepository) FindCredentials(consumerID uint) ([]models.APIConsumerCredential, error) {
	var credentials []models.APIConsumerCredential
	err := r.db.Where("api_consumer_id = ?", consumerID).
		Order("created_at DESC").Find(&credentials).Error
	return credentials, err
}

func (r *APIConsumerRepository) UpdateCredentialLastUsed(id uint) error {
	return r.db.Model(&models.APIConsumerCredential{}).
		Where("id = ?", id).
		Update("last_used_at", gorm.Expr("NOW()")).Error
}

func (r *APIConsumerRepository) RevokeCredential(consumerID, credentialID, revokedBy uint) error {
	now := time.Now().UTC()
	result := r.db.Model(&models.APIConsumerCredential{}).
		Where("id = ? AND api_consumer_id = ? AND status = ?", credentialID, consumerID, "active").
		Updates(map[string]any{
			"status": "revoked", "revoked_at": now, "revoked_by_principal_id": revokedBy,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *APIConsumerRepository) FindCredentialWithConsumer(
	keyHash string,
) (*models.APIConsumerCredential, *models.APIConsumer, error) {
	var credential models.APIConsumerCredential
	if err := r.db.Where("key_hash = ? AND status = ?", keyHash, "active").
		First(&credential).Error; err != nil {
		return nil, nil, commonrepo.WrapDBError(err)
	}
	var consumer models.APIConsumer
	if err := r.db.Preload("ServiceGrants").
		Where("id = ?", credential.APIConsumerID).First(&consumer).Error; err != nil {
		return nil, nil, commonrepo.WrapDBError(err)
	}
	return &credential, &consumer, nil
}
