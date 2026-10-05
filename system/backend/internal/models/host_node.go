package models

import "time"

// HostNode is the System-owned deployment identity, without runtime metrics or credentials.
type HostNode struct {
	NodeID                string                  `gorm:"primaryKey;type:uuid" json:"node_id"`
	DisplayName           string                  `gorm:"not null;size:255" json:"display_name"`
	NodeKind              string                  `gorm:"not null;size:20" json:"node_kind"`
	Addresses             []string                `gorm:"serializer:json;type:jsonb;not null" json:"addresses"`
	Enabled               bool                    `gorm:"not null" json:"enabled"`
	AllowedModuleBindings []HostNodeModuleBinding `gorm:"serializer:json;type:jsonb;not null" json:"allowed_module_bindings"`
	Version               int64                   `gorm:"not null" json:"version"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
}

func (HostNode) TableName() string { return "host_nodes" }

type HostNodeModuleBinding struct {
	ClientID   string `json:"client_id" binding:"required"`
	ModuleName string `json:"module_name" binding:"required"`
}
type HostNodeInput struct {
	DisplayName           string                  `json:"display_name" binding:"required"`
	NodeKind              string                  `json:"node_kind" binding:"required" enums:"physical,virtual"`
	Addresses             []string                `json:"addresses" binding:"required"`
	Enabled               *bool                   `json:"enabled" binding:"required"`
	AllowedModuleBindings []HostNodeModuleBinding `json:"allowed_module_bindings" binding:"required"`
}
type HostNodeUpdateRequest struct {
	HostNodeInput
	Version int64 `json:"version" binding:"required,gt=0"`
}
type HostNodePage struct {
	Data       []HostNode `json:"data"`
	Total      int64      `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	TotalPages int64      `json:"total_pages"`
}
