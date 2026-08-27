package repository

import (
	"errors"
	"time"

	"github.com/addp/workbench/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrResourceGrantConflict = errors.New("resource grant source identity conflict")

type ResourceAccessRuleRepository struct{ db *gorm.DB }

func NewResourceAccessRuleRepository(db *gorm.DB) *ResourceAccessRuleRepository {
	return &ResourceAccessRuleRepository{db: db}
}

func (r *ResourceAccessRuleRepository) FulfillAssetGrant(input models.ResourceAccessRule) (*models.ResourceAccessRule, error) {
	var result models.ResourceAccessRule
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var existing models.ResourceAccessRule
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND source_module = ? AND source_identity = ?", input.TenantID, models.ResourceAccessSourceAsset, input.SourceIdentity).
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			input.ID = uuid.NewString()
			input.SourceModule = models.ResourceAccessSourceAsset
			input.Effect = models.ResourceAccessEffectAllow
			if createErr := tx.Create(&input).Error; createErr != nil {
				return createErr
			}
			result = input
			return nil
		}
		if err != nil {
			return err
		}
		if !sameAssetGrant(existing, input) || existing.RevokedAt != nil {
			return ErrResourceGrantConflict
		}
		result = existing
		return nil
	})
	return &result, err
}

func (r *ResourceAccessRuleRepository) RevokeAssetGrant(input models.ResourceAccessRule, revokedAt time.Time) (*models.ResourceAccessRule, error) {
	var result models.ResourceAccessRule
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var existing models.ResourceAccessRule
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND source_module = ? AND source_identity = ?", input.TenantID, models.ResourceAccessSourceAsset, input.SourceIdentity).
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			input.ID = uuid.NewString()
			input.SourceModule = models.ResourceAccessSourceAsset
			input.Effect = models.ResourceAccessEffectAllow
			input.RevokedAt = &revokedAt
			if createErr := tx.Create(&input).Error; createErr != nil {
				return createErr
			}
			result = input
			return nil
		}
		if err != nil {
			return err
		}
		if !sameAssetGrant(existing, input) {
			return ErrResourceGrantConflict
		}
		if existing.RevokedAt == nil {
			if err := tx.Model(&existing).Updates(map[string]any{"revoked_at": revokedAt, "updated_at": revokedAt}).Error; err != nil {
				return err
			}
			existing.RevokedAt = &revokedAt
		}
		result = existing
		return nil
	})
	return &result, err
}

func (r *ResourceAccessRuleRepository) CanExecuteDataApplication(tenantID, subjectID int64, resourceID string, now time.Time) (bool, error) {
	base := func() *gorm.DB {
		return r.db.Model(&models.ResourceAccessRule{}).
			Where("tenant_id = ? AND resource_type = ? AND resource_id = ? AND subject_type = ? AND subject_id = ? AND permission = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)",
				tenantID, models.ResourceTypeDataApplication, resourceID, models.ResourceAccessSubjectUser, subjectID, models.DataApplicationExecutePermission, now)
	}
	var denyCount int64
	if err := base().Where("effect = ?", models.ResourceAccessEffectDeny).Count(&denyCount).Error; err != nil {
		return false, err
	}
	if denyCount > 0 {
		return false, nil
	}
	var allowCount int64
	if err := base().Where("effect = ?", models.ResourceAccessEffectAllow).Count(&allowCount).Error; err != nil {
		return false, err
	}
	return allowCount > 0, nil
}

func sameAssetGrant(existing, input models.ResourceAccessRule) bool {
	return existing.ResourceType == input.ResourceType && existing.ResourceID == input.ResourceID &&
		existing.SubjectType == input.SubjectType && existing.SubjectID == input.SubjectID &&
		existing.Permission == input.Permission && existing.Effect == models.ResourceAccessEffectAllow &&
		sameOptionalTime(existing.ExpiresAt, input.ExpiresAt)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UTC().Equal(right.UTC())
}
