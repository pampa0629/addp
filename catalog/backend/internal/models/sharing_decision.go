package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SharingDecision is an immutable business confirmation, never a Grant or a
// System acceptance receipt. Identity provenance is derived from AuthContext.
type SharingDecision struct {
	ID                      uuid.UUID       `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID                int64           `gorm:"not null;index" json:"-"`
	CatalogEntryID          uuid.UUID       `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	EntryVersion            int64           `gorm:"not null" json:"entry_version,string" swaggertype:"string"`
	SourceBindingID         uuid.UUID       `gorm:"type:uuid;not null" json:"source_binding_id"`
	SourceVersion           string          `gorm:"size:20;not null" json:"source_version"`
	ResponsibilityID        uuid.UUID       `gorm:"type:uuid;not null" json:"responsibility_id"`
	EngineID                int64           `gorm:"not null" json:"engine_id,string" swaggertype:"string"`
	CatalogPath             json.RawMessage `gorm:"type:jsonb;not null" json:"-"`
	ConfirmedBy             int64           `gorm:"not null" json:"confirmed_by,string" swaggertype:"string"`
	ConfirmerMembershipID   int64           `gorm:"not null" json:"confirmer_membership_id,string" swaggertype:"string"`
	AuthorizationVersion    int64           `gorm:"not null" json:"authorization_version,string" swaggertype:"string"`
	RecipientType           string          `gorm:"size:32;not null" json:"recipient_type"`
	RecipientID             int64           `gorm:"not null" json:"recipient_id,string" swaggertype:"string"`
	Action                  string          `gorm:"size:16;not null" json:"action"`
	SelfBeneficiary         bool            `gorm:"not null" json:"self_beneficiary"`
	ConfirmerInProjectGroup bool            `gorm:"not null" json:"confirmer_in_project_group"`
	Reason                  string          `gorm:"type:text;not null" json:"reason"`
	ExpiryMode              string          `gorm:"size:32;not null" json:"expiry_mode" enums:"at_time,until_revoked"`
	ExpiresAt               *time.Time      `json:"expires_at" format:"date-time" extensions:"x-nullable"`
	CreatedAt               time.Time       `gorm:"not null" json:"created_at"`
}

func (SharingDecision) TableName() string { return "catalog.sharing_decisions" }
