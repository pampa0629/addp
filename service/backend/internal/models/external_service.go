package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ExternalService 外部服务注册模型
type ExternalService struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	TenantID    uint           `gorm:"not null;index:idx_external_service_tenant" json:"tenant_id"`
	Name        string         `gorm:"not null;size:255" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	ServiceType string         `gorm:"not null;size:50;index:idx_external_service_type" json:"service_type"` // 'wms', 'wfs', 'wmts', 'ogc_api', 'data_api', 'rest'
	URL         string         `gorm:"not null;type:text" json:"url"`
	Metadata    JSONB          `gorm:"type:jsonb" json:"metadata"`                     // 服务元数据（GetCapabilities 解析结果）
	AuthType    string         `gorm:"size:50" json:"auth_type"`                       // 'none', 'basic', 'bearer', 'api_key'
	AuthConfig  JSONB          `gorm:"type:jsonb" json:"auth_config"`                  // 认证配置（加密存储）
	Status      string         `gorm:"size:20;default:'active'" json:"status"`         // 'active', 'inactive', 'error'
	HealthCheck HealthCheckURL `gorm:"type:text" json:"health_check_url,omitempty"`    // 健康检查端点
	LastChecked *time.Time     `json:"last_checked_at,omitempty"`                      // 上次检查时间
	CreatedBy   uint           `gorm:"not null" json:"created_by"`                     // 创建者用户ID
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联
	Layers []ServiceLayer `gorm:"foreignKey:ServiceID;constraint:OnDelete:CASCADE" json:"layers,omitempty"`
}

// ServiceLayer 服务图层/要素类注册模型
type ServiceLayer struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	ServiceID    uint      `gorm:"not null;index:idx_service_layer_service" json:"service_id"`
	LayerName    string    `gorm:"not null;size:255" json:"layer_name"`
	DisplayName  string    `gorm:"size:255" json:"display_name"`
	GeometryType string    `gorm:"size:50" json:"geometry_type"` // 'Point', 'LineString', 'Polygon', etc.
	CRS          string    `gorm:"size:50" json:"crs"`           // 'EPSG:4326', 'EPSG:3857'
	BBox         JSONB     `gorm:"type:jsonb" json:"bbox"`       // 边界框
	Metadata     JSONB     `gorm:"type:jsonb" json:"metadata"`   // 图层元数据
	Enabled      bool      `gorm:"default:true" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// JSONB 自定义类型，用于存储 JSON 数据
type JSONB map[string]interface{}

// Value 实现 driver.Valuer 接口
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan 实现 sql.Scanner 接口
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

// HealthCheckURL 健康检查 URL 类型（可为空字符串）
type HealthCheckURL string

// Value 实现 driver.Valuer 接口
func (h HealthCheckURL) Value() (driver.Value, error) {
	if h == "" {
		return nil, nil
	}
	return string(h), nil
}

// Scan 实现 sql.Scanner 接口
func (h *HealthCheckURL) Scan(value interface{}) error {
	if value == nil {
		*h = ""
		return nil
	}
	if str, ok := value.(string); ok {
		*h = HealthCheckURL(str)
	}
	return nil
}

// TableName 指定表名
func (ExternalService) TableName() string {
	return "service.external_services"
}

// TableName 指定表名
func (ServiceLayer) TableName() string {
	return "service.service_layers"
}

// ExternalServiceDTO 外部服务 DTO（用于 API 响应）
type ExternalServiceDTO struct {
	ID              uint               `json:"id"`
	TenantID        uint               `json:"tenant_id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	ServiceType     string             `json:"service_type"`
	URL             string             `json:"url"`
	Metadata        JSONB              `json:"metadata,omitempty"`
	AuthType        string             `json:"auth_type"`
	Status          string             `json:"status"`
	HealthCheckURL  string             `json:"health_check_url,omitempty"`
	LastCheckedAt   *time.Time         `json:"last_checked_at,omitempty"`
	CreatedBy       uint               `json:"created_by"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Layers          []ServiceLayerDTO  `json:"layers,omitempty"`
}

// ServiceLayerDTO 服务图层 DTO
type ServiceLayerDTO struct {
	ID           uint      `json:"id"`
	ServiceID    uint      `json:"service_id"`
	LayerName    string    `json:"layer_name"`
	DisplayName  string    `json:"display_name"`
	GeometryType string    `json:"geometry_type"`
	CRS          string    `json:"crs"`
	BBox         JSONB     `json:"bbox,omitempty"`
	Metadata     JSONB     `json:"metadata,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateExternalServiceRequest 创建外部服务请求
type CreateExternalServiceRequest struct {
	Name           string `json:"name" binding:"required"`
	Description    string `json:"description"`
	ServiceType    string `json:"service_type" binding:"required,oneof=wms wfs wmts ogc_api data_api rest"`
	URL            string `json:"url" binding:"required,url"`
	AuthType       string `json:"auth_type" binding:"omitempty,oneof=none basic bearer api_key"`
	AuthConfig     JSONB  `json:"auth_config,omitempty"`
	HealthCheckURL string `json:"health_check_url,omitempty"`
}

// UpdateExternalServiceRequest 更新外部服务请求
type UpdateExternalServiceRequest struct {
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	URL            *string `json:"url,omitempty"`
	AuthType       *string `json:"auth_type,omitempty"`
	AuthConfig     *JSONB  `json:"auth_config,omitempty"`
	Status         *string `json:"status,omitempty" binding:"omitempty,oneof=active inactive error"`
	HealthCheckURL *string `json:"health_check_url,omitempty"`
}
