package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// FulfillmentCheck is a durable pre-send coordination fact, not an acceptance
// receipt, approval decision or source Grant. ResolvedAt means System's exact
// immutable outcome was checked, not that content access was granted.
type FulfillmentCheck struct {
	RequestID      uuid.UUID       `gorm:"type:uuid;primaryKey"`
	TenantID       int64           `gorm:"not null;index:ix_catalog_fulfillment_entry,priority:1"`
	CatalogEntryID uuid.UUID       `gorm:"type:uuid;not null;index:ix_catalog_fulfillment_entry,priority:2"`
	RequestBinding json.RawMessage `gorm:"type:jsonb;not null"`
	CreatedAt      time.Time       `gorm:"not null;default:CURRENT_TIMESTAMP;autoCreateTime:false"`
	ResolvedAt     *time.Time
}

func (FulfillmentCheck) TableName() string { return "catalog.fulfillment_checks" }
