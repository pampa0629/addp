package models

import (
	"time"

	commonconfiguration "github.com/addp/common/configuration"
	"gorm.io/datatypes"
)

// ModuleRegistry 模块注册表
// 用于动态模块发现和 Gateway 路由
type ModuleRegistry struct {
	ID                      uint           `gorm:"primaryKey" json:"id"`
	ModuleName              string         `gorm:"uniqueIndex;not null;size:50" json:"module_name"` // 'system', 'manager', 'meta' 等
	ModuleURL               string         `gorm:"not null;size:255" json:"module_url"`             // 'http://localhost:8180'
	RoutePrefix             string         `gorm:"not null;size:50" json:"route_prefix"`            // '/system', '/manager' 等
	HealthCheckURL          string         `gorm:"size:255" json:"health_check_url"`                // 健康检查端点
	Status                  string         `gorm:"default:up;size:20" json:"status"`                // 'up', 'down'
	LastHeartbeat           time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"last_heartbeat"` // 最后心跳时间
	Metadata                datatypes.JSON `gorm:"type:jsonb" json:"metadata"`                      // 扩展信息（版本、权重等）
	ConfigurationManagement datatypes.JSON `gorm:"type:jsonb" json:"configuration_management"`
	CreatedAt               time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ModuleRegistry) TableName() string {
	return "module_registry"
}

// ModuleRegistrationRequest 模块注册请求
type ModuleRegistrationRequest struct {
	ModuleName              string                                     `json:"module_name" binding:"required"`
	ModuleURL               string                                     `json:"module_url" binding:"required"`
	RoutePrefix             string                                     `json:"route_prefix" binding:"required"`
	HealthCheckURL          string                                     `json:"health_check_url"`
	Metadata                map[string]interface{}                     `json:"metadata"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management"`
}

// HeartbeatRequest 心跳请求
type HeartbeatRequest struct {
	ModuleName string `json:"module_name" binding:"required"`
}

// ModuleInfo 模块信息响应
type ModuleInfo struct {
	ID                      uint                                       `json:"id"`
	ModuleName              string                                     `json:"module_name"`
	ModuleURL               string                                     `json:"module_url"`
	RoutePrefix             string                                     `json:"route_prefix"`
	HealthCheckURL          string                                     `json:"health_check_url"`
	Status                  string                                     `json:"status"`
	LastHeartbeat           time.Time                                  `json:"last_heartbeat"`
	Metadata                map[string]interface{}                     `json:"metadata"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management,omitempty"`
	CreatedAt               time.Time                                  `json:"created_at"`
	UpdatedAt               time.Time                                  `json:"updated_at"`
}

// ConfigurationManagementEntryView projects a registered entry with current module availability.
type ConfigurationManagementEntryView struct {
	commonconfiguration.ManagementEntry
	ModuleStatus string `json:"module_status"`
	Available    bool   `json:"available"`
}
