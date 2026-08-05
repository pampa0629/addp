package repository

import (
	"context"
	"errors"

	"github.com/addp/inference/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }
func (s *Store) DB() *gorm.DB     { return s.db }

func (s *Store) Transaction(ctx context.Context, fn func(*Store) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(NewStore(tx)) })
}

func (s *Store) CreateProvider(ctx context.Context, value *models.ProviderConnection, grants []models.ProviderTenantGrant) error {
	return s.Transaction(ctx, func(tx *Store) error {
		if err := tx.db.Create(value).Error; err != nil {
			return err
		}
		if len(grants) > 0 {
			return tx.db.Create(&grants).Error
		}
		return nil
	})
}

func (s *Store) SaveProvider(ctx context.Context, value *models.ProviderConnection, grants []models.ProviderTenantGrant) error {
	return s.Transaction(ctx, func(tx *Store) error {
		if err := tx.db.Save(value).Error; err != nil {
			return err
		}
		if err := tx.db.Where("provider_connection_id = ?", value.ID).Delete(&models.ProviderTenantGrant{}).Error; err != nil {
			return err
		}
		if len(grants) > 0 {
			return tx.db.Create(&grants).Error
		}
		return nil
	})
}

func (s *Store) ListProviders(ctx context.Context, scopeType string, tenantID uint) ([]models.ProviderConnection, error) {
	var values []models.ProviderConnection
	query := s.db.WithContext(ctx).Order("created_at ASC")
	if scopeType == models.ScopePlatform {
		query = query.Where("scope_type = ?", models.ScopePlatform)
	} else {
		query = query.Where("scope_type = ? OR tenant_id = ?", models.ScopePlatform, tenantID)
	}
	return values, query.Find(&values).Error
}

func (s *Store) GetProvider(ctx context.Context, id string, lock bool) (*models.ProviderConnection, error) {
	var value models.ProviderConnection
	query := s.db.WithContext(ctx)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&value, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *Store) ProviderGrants(ctx context.Context, id string) ([]uint, error) {
	var grants []models.ProviderTenantGrant
	if err := s.db.WithContext(ctx).Where("provider_connection_id = ?", id).Order("tenant_id ASC").Find(&grants).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(grants))
	for _, grant := range grants {
		ids = append(ids, grant.TenantID)
	}
	return ids, nil
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&models.ProviderConnection{}, "id = ?", id).Error
}
func (s *Store) CountDeploymentsForProvider(ctx context.Context, id string) (int64, error) {
	var count int64
	return count, s.db.WithContext(ctx).Model(&models.ModelDeployment{}).Where("provider_connection_id = ?", id).Count(&count).Error
}

func (s *Store) CreateDeployment(ctx context.Context, value *models.ModelDeployment) error {
	return s.db.WithContext(ctx).Create(value).Error
}
func (s *Store) SaveDeployment(ctx context.Context, value *models.ModelDeployment) error {
	return s.db.WithContext(ctx).Save(value).Error
}
func (s *Store) DeleteDeployment(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&models.ModelDeployment{}, "id = ?", id).Error
}
func (s *Store) GetDeployment(ctx context.Context, id string) (*models.ModelDeployment, error) {
	var value models.ModelDeployment
	if err := s.db.WithContext(ctx).First(&value, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &value, nil
}
func (s *Store) ListDeployments(ctx context.Context) ([]models.ModelDeployment, error) {
	var values []models.ModelDeployment
	return values, s.db.WithContext(ctx).Order("created_at ASC").Find(&values).Error
}
func (s *Store) CountProfilesForDeployment(ctx context.Context, id string) (int64, error) {
	var count int64
	return count, s.db.WithContext(ctx).Model(&models.ModelProfile{}).Where("model_deployment_id = ?", id).Count(&count).Error
}

func (s *Store) CreateProfile(ctx context.Context, value *models.ModelProfile) error {
	return s.db.WithContext(ctx).Create(value).Error
}
func (s *Store) SaveProfile(ctx context.Context, value *models.ModelProfile) error {
	return s.db.WithContext(ctx).Save(value).Error
}
func (s *Store) GetProfile(ctx context.Context, id string) (*models.ModelProfile, error) {
	var value models.ModelProfile
	if err := s.db.WithContext(ctx).First(&value, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &value, nil
}
func (s *Store) ListProfiles(ctx context.Context) ([]models.ModelProfile, error) {
	var values []models.ModelProfile
	return values, s.db.WithContext(ctx).Order("created_at ASC").Find(&values).Error
}

func (s *Store) RotateCredential(ctx context.Context, providerID, ciphertext, action string, principalID uint) (*models.ProviderConnection, error) {
	var result *models.ProviderConnection
	err := s.Transaction(ctx, func(tx *Store) error {
		provider, err := tx.GetProvider(ctx, providerID, true)
		if err != nil {
			return err
		}
		oldVersion := provider.CredentialVersion
		provider.CredentialVersion++
		provider.CredentialCiphertext = ciphertext
		provider.UpdatedBy = principalID
		if err := tx.db.Save(provider).Error; err != nil {
			return err
		}
		audit := models.CredentialAudit{ID: uuid.NewString(), ProviderConnectionID: providerID, OldVersion: oldVersion, NewVersion: provider.CredentialVersion, Action: action, PrincipalID: principalID}
		if err := tx.db.Create(&audit).Error; err != nil {
			return err
		}
		result = provider
		return nil
	})
	return result, err
}

func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
