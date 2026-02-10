package models

import (
	"time"
)

// RegisteredService 注册服务模型
type RegisteredService struct {
	ID uint `gorm:"primarykey" json:"id"`

	// 基本信息
	TenantID    uint        `gorm:"not null;index:idx_registered_services_tenant" json:"tenant_id"`
	ServiceName string      `gorm:"not null;size:255;uniqueIndex:unique_registered_service_name" json:"service_name"`
	Title       string      `gorm:"not null;size:255" json:"title"`
	Description string      `gorm:"type:text" json:"description"`
	Keywords    StringArray `gorm:"type:text[]" json:"keywords"`

	// 服务类型
	ServiceType string `gorm:"size:50;not null;check:service_type IN ('wms', 'wfs', 'wmts', 'ogc_api', 'xyz', 'rest');index:idx_registered_services_type" json:"service_type"`

	// 服务端点
	EndpointURL string `gorm:"type:text;not null" json:"endpoint_url"`

	// 元数据（GetCapabilities解析结果）
	Metadata JSONB `gorm:"type:jsonb;default:'{}';index:idx_registered_services_metadata_gin,type:gin" json:"metadata"`

	// 认证配置
	AuthType   string `gorm:"size:50;default:'none';check:auth_type IN ('none', 'basic', 'bearer', 'api_key')" json:"auth_type"`
	AuthConfig JSONB `gorm:"type:jsonb;default:'{}'" json:"auth_config"`

	// 健康检查
	HealthCheckURL string     `gorm:"type:text" json:"health_check_url"`
	LastCheckedAt  *time.Time `json:"last_checked_at"`

	// 状态
	Status       string `gorm:"size:50;default:'active';check:status IN ('active', 'inactive', 'error');index:idx_registered_services_status" json:"status"`
	ErrorMessage string `gorm:"type:text" json:"error_message"`

	// 审计字段
	CreatedBy uint      `gorm:"not null" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 关联
	Layers []RegisteredServiceLayer `gorm:"foreignKey:ServiceID;constraint:OnDelete:CASCADE" json:"layers,omitempty"`
}

// TableName 指定表名
func (RegisteredService) TableName() string {
	return "service.registered_services"
}

// IsOGCService 是否为OGC服务
func (r *RegisteredService) IsOGCService() bool {
	return r.ServiceType == "wms" || r.ServiceType == "wfs" || r.ServiceType == "wmts" || r.ServiceType == "ogc_api"
}

// RequiresAuth 是否需要认证
func (r *RegisteredService) RequiresAuth() bool {
	return r.AuthType != "none"
}

// RegisteredServiceLayer 注册服务图层模型
type RegisteredServiceLayer struct {
	ID uint `gorm:"primarykey" json:"id"`

	// 关联
	ServiceID uint `gorm:"not null;index:idx_registered_layers_service;uniqueIndex:unique_service_layer" json:"service_id"`

	// 基本信息
	LayerName   string `gorm:"not null;size:255;uniqueIndex:unique_service_layer" json:"layer_name"`
	DisplayName string `gorm:"size:255" json:"display_name"`
	Description string `gorm:"type:text" json:"description"`

	// 几何信息（从GetCapabilities解析）
	GeometryType string `gorm:"size:50" json:"geometry_type"`
	CRS          string `gorm:"size:50" json:"crs"` // 坐标参考系统
	BBox         JSONB  `gorm:"type:jsonb" json:"bbox"`

	// 图层元数据
	Metadata JSONB `gorm:"type:jsonb;default:'{}';index:idx_registered_layers_metadata_gin,type:gin" json:"metadata"`

	// 控制
	Enabled bool `gorm:"default:true;index:idx_registered_layers_enabled" json:"enabled"`

	// 审计字段
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (RegisteredServiceLayer) TableName() string {
	return "service.registered_service_layers"
}

// CreateRegisteredServiceRequest 创建注册服务请求
type CreateRegisteredServiceRequest struct {
	ServiceName string   `json:"service_name" binding:"required"`
	Title       string   `json:"title" binding:"required"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`

	// 服务类型
	ServiceType string `json:"service_type" binding:"required,oneof=wms wfs wmts ogc_api xyz rest"`

	// 服务端点
	EndpointURL string `json:"endpoint_url" binding:"required"`

	// 认证配置（可选）
	AuthType   string                 `json:"auth_type" binding:"omitempty,oneof=none basic bearer api_key"`
	AuthConfig map[string]interface{} `json:"auth_config"`

	// 健康检查端点（可选）
	HealthCheckURL string `json:"health_check_url"`

	// 是否自动刷新元数据
	AutoRefreshMetadata bool `json:"auto_refresh_metadata"`
}

// UpdateRegisteredServiceRequest 更新注册服务请求
type UpdateRegisteredServiceRequest struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`

	// 认证配置更新
	AuthType   *string                `json:"auth_type,omitempty" binding:"omitempty,oneof=none basic bearer api_key"`
	AuthConfig map[string]interface{} `json:"auth_config,omitempty"`

	// 健康检查URL更新
	HealthCheckURL *string `json:"health_check_url,omitempty"`

	// 状态更新
	Status *string `json:"status,omitempty" binding:"omitempty,oneof=active inactive error"`
}

// RegisteredServiceDTO 注册服务 DTO
type RegisteredServiceDTO struct {
	ID uint `json:"id"`

	TenantID    uint     `json:"tenant_id"`
	ServiceName string   `json:"service_name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`

	ServiceType string `json:"service_type"`
	EndpointURL string `json:"endpoint_url"`

	// 元数据
	Metadata map[string]interface{} `json:"metadata"`

	// 认证配置（不返回敏感信息）
	AuthType       string `json:"auth_type"`
	HasAuthConfig  bool   `json:"has_auth_config"` // 是否配置了认证信息
	HealthCheckURL string `json:"health_check_url"`
	LastCheckedAt  *time.Time `json:"last_checked_at"`

	// 图层信息
	Layers []RegisteredServiceLayerDTO `json:"layers"`

	// 状态
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`

	// 服务端点
	Endpoints map[string]string `json:"endpoints"`

	// 审计
	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RegisteredServiceLayerDTO 注册服务图层 DTO
type RegisteredServiceLayerDTO struct {
	ID uint `json:"id"`

	ServiceID uint `json:"service_id"`

	LayerName   string `json:"layer_name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`

	GeometryType string                 `json:"geometry_type"`
	CRS          string                 `json:"crs"`
	BBox         map[string]interface{} `json:"bbox"`

	Metadata map[string]interface{} `json:"metadata"`

	Enabled bool `json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RefreshMetadataRequest 刷新元数据请求
type RefreshMetadataRequest struct {
	Force bool `json:"force"` // 是否强制刷新（即使最近刚刷新过）
}

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	ServiceID   uint      `json:"service_id"`
	ServiceName string    `json:"service_name"`
	Status      string    `json:"status"` // healthy, unhealthy, error
	StatusCode  int       `json:"status_code,omitempty"`
	Message     string    `json:"message"`
	CheckedAt   time.Time `json:"checked_at"`
	ResponseTime int64    `json:"response_time"` // 响应时间（毫秒）
}
