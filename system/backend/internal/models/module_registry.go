package models

import (
	"time"

	commonconfiguration "github.com/addp/common/configuration"
	commonmodels "github.com/addp/common/models"
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
	Version                 int64                   `gorm:"not null;default:1" json:"version"`
	ConfigurationManagement datatypes.JSON          `gorm:"type:jsonb" json:"configuration_management"`
	TaskProvider            datatypes.JSON          `gorm:"type:jsonb" json:"task_provider"`
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

// ModuleRegistryState 保存 Gateway 路由拓扑的单调递增修订号。
type ModuleRegistryState struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Revision  int64     `gorm:"not null" json:"revision"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ModuleRegistryState) TableName() string { return "module_registry_state" }

type ModuleRegistrationRequest struct {
	ModuleName              string                                     `json:"module_name" binding:"required"`
	InstanceID              string                                     `json:"instance_id" binding:"required"`
	Role                    string                                     `json:"role" binding:"required"`
	ModuleURL               string                                     `json:"module_url"`
	RoutePrefix             string                                     `json:"route_prefix" binding:"required"`
	HealthCheckURL          string                                     `json:"health_check_url"`
	Metadata                map[string]interface{}                     `json:"metadata"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management"`
	TaskProvider            *commonmodels.TaskProviderDeclaration      `json:"task_provider"`
}

type HeartbeatRequest struct {
	ModuleName string `json:"module_name" binding:"required"`
	InstanceID string `json:"instance_id" binding:"required"`
}

type ModuleRoutingSnapshot struct {
	Revision   int64         `json:"revision"`
	Modules    []*ModuleInfo `json:"modules"`
	ObservedAt time.Time     `json:"observed_at"`
}

type ModuleDefinitionUpdateRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
	Version int64 `json:"version" binding:"required,gt=0"`
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

type ModuleRuntimeInstanceFilter struct {
	Role     string
	Status   string
	Page     int
	PageSize int
}

type ModuleInfo struct {
	ID                      uint                                       `json:"id"`
	ModuleName              string                                     `json:"module_name"`
	RoutePrefix             string                                     `json:"route_prefix"`
	Enabled                 bool                                       `json:"enabled"`
	Version                 int64                                      `json:"version"`
	Instances               []ModuleRuntimeInstanceInfo                `json:"instances"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management,omitempty"`
	TaskProvider            *commonmodels.TaskProviderDeclaration      `json:"task_provider,omitempty"`
	CreatedAt               time.Time                                  `json:"created_at"`
	UpdatedAt               time.Time                                  `json:"updated_at"`
}

type ConfigurationManagementEntryView struct {
	commonconfiguration.ManagementEntry
	ModuleStatus string `json:"module_status"`
	Available    bool   `json:"available"`
}
