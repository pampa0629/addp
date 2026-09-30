package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"
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
