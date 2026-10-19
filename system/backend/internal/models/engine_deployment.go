package models

import "time"

// BusinessDeploymentInventory is a credential-free declaration, never a live observation or grant.
type BusinessDeploymentInventory struct {
	Schema     string                      `json:"schema"`
	ObservedAt string                      `json:"observed_at"`
	Source     BusinessDeploymentSource    `json:"source"`
	Services   []BusinessDeploymentService `json:"services"`
}
type BusinessDeploymentSource struct {
	Type           string `json:"type"`
	Runtime        string `json:"runtime"`
	DockerEngineID string `json:"docker_engine_id"`
	Project        string `json:"project"`
}
type BusinessDeploymentService struct {
	Service    string                        `json:"service"`
	EngineType string                        `json:"engine_type"`
	Containers []BusinessDeploymentContainer `json:"containers"`
}
type BusinessDeploymentContainer struct {
	ContainerID    string                   `json:"container_id"`
	StartedAt      string                   `json:"started_at"`
	RestartCount   int                      `json:"restart_count"`
	PublishedPorts []BusinessDeploymentPort `json:"published_ports"`
}
type BusinessDeploymentPort struct {
	Address       string `json:"address"`
	Port          int    `json:"port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
}

// EngineDeployment belongs to Platform operations, without Tenant Engine or data permissions.
type EngineDeployment struct {
	DeploymentID      string                      `gorm:"primaryKey;type:uuid" json:"deployment_id"`
	DisplayName       string                      `gorm:"not null;size:255" json:"display_name"`
	EngineType        string                      `gorm:"not null;size:50" json:"engine_type"`
	Enabled           bool                        `gorm:"not null" json:"enabled"`
	Inventory         BusinessDeploymentInventory `gorm:"serializer:json;type:jsonb;not null" json:"inventory"`
	DeclaredNodeID    string                      `gorm:"type:uuid;not null" json:"declared_node_id"`
	Version           int64                       `gorm:"not null" json:"version"`
	CreatedAt         time.Time                   `gorm:"autoCreateTime:false" json:"created_at"`
	UpdatedAt         time.Time                   `gorm:"autoUpdateTime:false" json:"updated_at"`
	NodeID            string                      `gorm:"-" json:"node_id"`
	HostBindingState  string                      `gorm:"-" json:"host_binding_state"`
	HostBindingReason string                      `gorm:"-" json:"host_binding_reason"`
}

func (EngineDeployment) TableName() string { return "engine_deployments" }

type EngineDeploymentInput struct {
	DisplayName    string                      `json:"display_name" binding:"required"`
	Enabled        *bool                       `json:"enabled" binding:"required"`
	Inventory      BusinessDeploymentInventory `json:"inventory" binding:"required"`
	DeclaredNodeID string                      `json:"declared_node_id" binding:"required"`
}
type EngineDeploymentUpdateRequest struct {
	EngineDeploymentInput
	Version int64 `json:"version" binding:"required,gt=0"`
}
type EngineDeploymentPage struct {
	Data       []EngineDeployment `json:"data"`
	Total      int64              `json:"total"`
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	TotalPages int64              `json:"total_pages"`
}
type EngineDeploymentFilter struct {
	Search string
	NodeID string
}

// DeploymentDiscovery is a deployment-side snapshot, never a management grant.
type DeploymentDiscovery struct {
	Scopes []DeploymentDiscoveryScope `json:"scopes"`
}
type DeploymentDiscoveryScope struct {
	Schema     string                        `json:"schema"`
	Scope      string                        `json:"scope"`
	State      string                        `json:"state"`
	ObservedAt string                        `json:"observed_at,omitempty"`
	Reason     string                        `json:"reason,omitempty"`
	Source     *BusinessDeploymentSource     `json:"source,omitempty"`
	Services   []DiscoveredDeploymentService `json:"services"`
}
type DiscoveredDeploymentService struct {
	Service    string                          `json:"service"`
	EngineType string                          `json:"engine_type"`
	Containers []DiscoveredDeploymentContainer `json:"containers"`
}
type DiscoveredDeploymentContainer struct {
	ContainerID    string                   `json:"container_id"`
	Status         string                   `json:"status"`
	StartedAt      string                   `json:"started_at"`
	RestartCount   int                      `json:"restart_count"`
	PublishedPorts []BusinessDeploymentPort `json:"published_ports"`
}
