package models

import (
	"time"

	"gorm.io/datatypes"
)

const (
	ScopePlatform  = "platform"
	ScopeTenant    = "tenant"
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type ProviderConnection struct {
	ID                   string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name                 string    `gorm:"size:120;not null" json:"name"`
	ScopeType            string    `gorm:"size:16;not null;index" json:"scope_type"`
	TenantID             *uint     `gorm:"index" json:"tenant_id,omitempty"`
	AdapterType          string    `gorm:"size:40;not null" json:"adapter_type"`
	Endpoint             string    `gorm:"type:text;not null" json:"endpoint"`
	AllowAllTenants      bool      `gorm:"not null;default:false" json:"allow_all_tenants"`
	Status               string    `gorm:"size:16;not null;default:active" json:"status"`
	CredentialCiphertext string    `gorm:"type:text" json:"-"`
	CredentialVersion    uint64    `gorm:"not null;default:0" json:"-"`
	CreatedBy            uint      `gorm:"not null" json:"created_by"`
	UpdatedBy            uint      `gorm:"not null" json:"updated_by"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (ProviderConnection) TableName() string { return "inference.provider_connections" }

type ProviderTenantGrant struct {
	ProviderConnectionID string    `gorm:"type:uuid;primaryKey" json:"provider_connection_id"`
	TenantID             uint      `gorm:"primaryKey" json:"tenant_id"`
	CreatedAt            time.Time `json:"created_at"`
}

func (ProviderTenantGrant) TableName() string { return "inference.provider_tenant_grants" }

type ModelDeployment struct {
	ID                           string         `gorm:"type:uuid;primaryKey" json:"id"`
	ProviderConnectionID         string         `gorm:"type:uuid;not null;index" json:"provider_connection_id"`
	Name                         string         `gorm:"size:120;not null" json:"name"`
	UpstreamModel                string         `gorm:"size:255;not null" json:"upstream_model"`
	Operations                   datatypes.JSON `gorm:"type:jsonb;not null" json:"operations"`
	Modalities                   datatypes.JSON `gorm:"type:jsonb;not null" json:"modalities"`
	Dimension                    int            `gorm:"not null;default:0" json:"dimension,omitempty"`
	ChatMaxOutputTokensParameter string         `gorm:"size:32;not null;default:max_tokens" json:"chat_max_output_tokens_parameter"`
	ChatTemperatureMode          string         `gorm:"size:32;not null;default:configurable" json:"chat_temperature_mode"`
	Status                       string         `gorm:"size:16;not null;default:active" json:"status"`
	CreatedBy                    uint           `gorm:"not null" json:"created_by"`
	UpdatedBy                    uint           `gorm:"not null" json:"updated_by"`
	CreatedAt                    time.Time      `json:"created_at"`
	UpdatedAt                    time.Time      `json:"updated_at"`
}

func (ModelDeployment) TableName() string { return "inference.model_deployments" }

type ModelProfile struct {
	ID                string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name              string    `gorm:"size:120;not null" json:"name"`
	Code              string    `gorm:"size:80;not null" json:"code"`
	ScopeType         string    `gorm:"size:16;not null;index" json:"scope_type"`
	TenantID          *uint     `gorm:"index" json:"tenant_id,omitempty"`
	ModelDeploymentID string    `gorm:"type:uuid;not null;index" json:"model_deployment_id"`
	Status            string    `gorm:"size:16;not null;default:active" json:"status"`
	Version           uint64    `gorm:"not null;default:1" json:"version"`
	CreatedBy         uint      `gorm:"not null" json:"created_by"`
	UpdatedBy         uint      `gorm:"not null" json:"updated_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (ModelProfile) TableName() string { return "inference.model_profiles" }

type CredentialAudit struct {
	ID                   string    `gorm:"type:uuid;primaryKey" json:"id"`
	ProviderConnectionID string    `gorm:"type:uuid;not null;index" json:"provider_connection_id"`
	OldVersion           uint64    `gorm:"not null" json:"old_version"`
	NewVersion           uint64    `gorm:"not null" json:"new_version"`
	Action               string    `gorm:"size:16;not null" json:"action"`
	PrincipalID          uint      `gorm:"not null" json:"principal_id"`
	CreatedAt            time.Time `json:"created_at"`
}

func (CredentialAudit) TableName() string { return "inference.credential_audits" }
