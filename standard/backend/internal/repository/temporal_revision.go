package repository

import (
	"errors"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func normalizeAsOf(asOf time.Time) time.Time {
	if asOf.IsZero() {
		return time.Now().UTC()
	}
	return asOf.UTC()
}

func effectiveAt(query *gorm.DB, prefix string, asOf time.Time) *gorm.DB {
	asOf = normalizeAsOf(asOf)
	return query.Where(
		prefix+".status = ? AND "+prefix+".effective_from <= ? AND ("+prefix+".effective_to IS NULL OR "+prefix+".effective_to > ?)",
		"published", asOf, asOf,
	)
}

func intervalsOverlap(leftFrom time.Time, leftTo *time.Time, rightFrom time.Time, rightTo *time.Time) bool {
	return (leftTo == nil || rightFrom.Before(*leftTo)) && (rightTo == nil || leftFrom.Before(*rightTo))
}

// The caller holds the identity lock and publishes the new revision in this transaction.
func preparePublishedInterval(tx *gorm.DB, model any, ownerColumn string, ownerID int64, from time.Time, to *time.Time) error {
	var published []struct {
		ID                         int64
		EffectiveFrom, EffectiveTo *time.Time
	}
	err := tx.Model(model).Select("id", "effective_from", "effective_to").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(ownerColumn+" = ? AND status = ?", ownerID, models.RevisionStatusPublished).
		Order("effective_from ASC, revision_no ASC").Find(&published).Error
	if err != nil {
		return err
	}
	for _, previous := range published {
		if previous.EffectiveFrom == nil {
			return ErrEffectiveIntervalConflict
		}
		if previous.EffectiveTo == nil && previous.EffectiveFrom.Before(from) {
			if err := tx.Model(model).Where("id = ? AND status = ?", previous.ID, models.RevisionStatusPublished).
				Update("effective_to", from).Error; err != nil {
				return err
			}
			continue
		}
		if intervalsOverlap(*previous.EffectiveFrom, previous.EffectiveTo, from, to) {
			return ErrEffectiveIntervalConflict
		}
	}
	return nil
}

// latestRevision reads history for management display after the identity's tenant check.
func latestRevision[T any](db *gorm.DB, ownerColumn string, identityID int64) (*T, error) {
	var revision T
	err := db.Where(ownerColumn+" = ?", identityID).Order("revision_no DESC").First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &revision, nil
}
