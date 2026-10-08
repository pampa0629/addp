package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/platform"
	"gorm.io/gorm"
)

// ActivePlatformDefinition reads global platform content, not Tenant content.
// The public boundary authorizes semantic reads; no synthetic actor is needed.
// Only one PG read snapshot is used, without remote calls or mutation.
func (r *PlatformRevisionRepository) ActivePlatformDefinition(ctx context.Context, capability string) (*models.PlatformRevision, error) {
	if platform.ValidateIdentity(capability, 1) != nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var record models.PlatformRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var head models.PlatformCapability
		if err := tx.Where("capability = ?", capability).First(&head).Error; err != nil {
			return err
		}
		var err error
		record, err = readActivePlatformRecord(tx, head)
		if err != nil {
			return err
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, normalizeError(err)
	}
	return &record, nil
}

// ActivePlatformDefinitions never drops a broken active pointer. Selection and
// every referenced revision/projection share one read-only PG snapshot.
func (r *PlatformRevisionRepository) ActivePlatformDefinitions(ctx context.Context) ([]models.PlatformRevision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records := make([]models.PlatformRevision, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var heads []models.PlatformCapability
		if err := tx.Where("active_revision IS NOT NULL OR active_generation IS NOT NULL OR activation_version <> 0").Order("capability ASC").Limit(platform.CatalogMaxItems + 1).Find(&heads).Error; err != nil {
			return err
		}
		if len(heads) > platform.CatalogMaxItems {
			return ErrPlatformCatalogTooLarge
		}
		for _, head := range heads {
			record, err := readActivePlatformRecord(tx, head)
			if err != nil {
				return err
			}
			records = append(records, record)
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, normalizeError(err)
	}
	return records, nil
}

var ErrPlatformCatalogTooLarge = errors.New("platform catalog too large")

func readActivePlatformRecord(tx *gorm.DB, head models.PlatformCapability) (models.PlatformRevision, error) {
	var record models.PlatformRevision
	if head.ActiveRevision == nil && head.ActiveGeneration == nil && head.ActivationVersion == 0 {
		return record, ErrNotActive
	}
	if head.ActiveRevision == nil || head.ActiveGeneration == nil || head.ActivationVersion == 0 {
		return record, ErrIntegrity
	}
	if err := tx.Where("capability = ? AND revision = ?", head.Capability, *head.ActiveRevision).First(&record).Error; err != nil {
		return record, platformReferenceError(err)
	}
	var projection models.PlatformProjection
	if err := tx.Where("capability = ? AND generation = ?", head.Capability, *head.ActiveGeneration).First(&projection).Error; err != nil {
		return record, platformReferenceError(err)
	}
	if projection.Status != "ready" || projection.Revision != record.Revision || projection.Digest != record.Digest || projection.BaselineActivationVersion != head.ActivationVersion-1 {
		return record, ErrIntegrity
	}
	return record, nil
}

func platformReferenceError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrIntegrity
	}
	return err
}
