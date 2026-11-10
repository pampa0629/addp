package models

import (
	"encoding/json"
	"time"
)

// FileExecution is Owner-private protocol state, never a public task result.
type FileExecution struct {
	ExecutionID             string          `gorm:"primaryKey;type:varchar(64)"`
	TenantID                uint            `gorm:"not null"`
	ActorPrincipalID        int64           `gorm:"not null"`
	ActorMembershipID       int64           `gorm:"not null"`
	AuthorizationVersion    int64           `gorm:"not null"`
	ConfigJSON              json.RawMessage `gorm:"type:jsonb;not null"`
	IntentJSON              json.RawMessage `gorm:"type:jsonb;not null"`
	IssuedAuthorizationJSON json.RawMessage `gorm:"type:jsonb"`
	AdmissionState          string          `gorm:"type:varchar(16);not null"`
	AdmissionJSON           json.RawMessage `gorm:"type:jsonb"`
	RuntimeExecutionID      *string         `gorm:"type:varchar(64)"`
	RuntimeManifestJSON     json.RawMessage `gorm:"type:jsonb"`
	PublicationJSON         json.RawMessage `gorm:"type:jsonb"`
	InputOwnerInstanceID    string          `gorm:"type:text"`
	InputDirectory          string          `gorm:"type:text"`
	AdmissionExpiresAt      *time.Time
	Version                 int64 `gorm:"not null"`
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (FileExecution) TableName() string { return "develop.file_executions" }
