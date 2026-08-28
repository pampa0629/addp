package repository

import (
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
