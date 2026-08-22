package models

import (
	"time"

	commonconfiguration "github.com/addp/common/configuration"
	"gorm.io/datatypes"
)

const (
	ModuleRuntimeRoleBackend   = "backend"
	ModuleRuntimeRoleWorker    = "worker"
	ModuleRuntimeRoleScheduler = "scheduler"
	ModuleRuntimeStatusUp      = "up"
	ModuleRuntimeStatusDown    = "down"
	ModuleRuntimeLeaseDuration = 30 * time.Second
)

// ModuleDefinition 是稳定模块身份和管理员意图，不随进程上下线改变。
type ModuleDefinition struct {
	ID                      uint                    `gorm:"primaryKey" json:"id"`
	ModuleName              string                  `gorm:"uniqueIndex;not null;size:50" json:"module_name"`
	RoutePrefix             string                  `gorm:"not null;size:50" json:"route_prefix"`
	Enabled                 bool                    `gorm:"not null;default:true;index" json:"enabled"`
	ConfigurationManagement datatypes.JSON          `gorm:"type:jsonb" json:"configuration_management"`
	RuntimeInstances        []ModuleRuntimeInstance `gorm:"foreignKey:ModuleDefinitionID" json:"-"`
	CreatedAt               time.Time               `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time               `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ModuleDefinition) TableName() string { return "module_definitions" }

// ModuleRuntimeInstance 是一次具体进程登记；心跳只续租这个实例。
type ModuleRuntimeInstance struct {
	ID                 uint           `gorm:"primaryKey" json:"id"`
	ModuleDefinitionID uint           `gorm:"not null;uniqueIndex:uq_module_runtime_instance;index" json:"module_definition_id"`
	InstanceID         string         `gorm:"not null;size:100;uniqueIndex:uq_module_runtime_instance" json:"instance_id"`
	Role               string         `gorm:"not null;size:30;index" json:"role"`
	ModuleURL          string         `gorm:"size:255" json:"module_url"`
	HealthCheckURL     string         `gorm:"size:255" json:"health_check_url"`
	Status             string         `gorm:"not null;default:'up';size:20;index" json:"status"`
	LastHeartbeat      time.Time      `gorm:"not null;index" json:"last_heartbeat"`
	LeaseExpiresAt     time.Time      `gorm:"not null;index" json:"lease_expires_at"`
	Metadata           datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	RegisteredAt       time.Time      `gorm:"not null" json:"registered_at"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ModuleRuntimeInstance) TableName() string { return "module_runtime_instances" }

type ModuleRegistrationRequest struct {
	ModuleName              string                                     `json:"module_name" binding:"required"`
	InstanceID              string                                     `json:"instance_id" binding:"required"`
	Role                    string                                     `json:"role" binding:"required"`
	ModuleURL               string                                     `json:"module_url"`
	RoutePrefix             string                                     `json:"route_prefix" binding:"required"`
	HealthCheckURL          string                                     `json:"health_check_url"`
	Metadata                map[string]interface{}                     `json:"metadata"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management"`
}

type HeartbeatRequest struct {
	ModuleName string `json:"module_name" binding:"required"`
	InstanceID string `json:"instance_id" binding:"required"`
}

type ModuleRuntimeInstanceInfo struct {
	ID             uint                   `json:"id"`
	InstanceID     string                 `json:"instance_id"`
	Role           string                 `json:"role"`
	ModuleURL      string                 `json:"module_url"`
	HealthCheckURL string                 `json:"health_check_url"`
	Status         string                 `json:"status"`
	LastHeartbeat  time.Time              `json:"last_heartbeat"`
	LeaseExpiresAt time.Time              `json:"lease_expires_at"`
	Metadata       map[string]interface{} `json:"metadata"`
	RegisteredAt   time.Time              `json:"registered_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type ModuleInfo struct {
	ID                      uint                                       `json:"id"`
	ModuleName              string                                     `json:"module_name"`
	RoutePrefix             string                                     `json:"route_prefix"`
	Enabled                 bool                                       `json:"enabled"`
	Instances               []ModuleRuntimeInstanceInfo                `json:"instances"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management,omitempty"`
	CreatedAt               time.Time                                  `json:"created_at"`
	UpdatedAt               time.Time                                  `json:"updated_at"`
}

type ConfigurationManagementEntryView struct {
	commonconfiguration.ManagementEntry
	ModuleStatus string `json:"module_status"`
	Available    bool   `json:"available"`
}
