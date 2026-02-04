package models

import (
	"time"

	"gorm.io/gorm"
)

// InternalService 内部发布的 OGC 服务
type InternalService struct {
	ID uint `gorm:"primarykey" json:"id"`

	// 基本信息
	TenantID    uint   `gorm:"not null;index:idx_internal_service_tenant" json:"tenant_id"`
	ServiceName string `gorm:"not null;size:255;uniqueIndex:idx_internal_service_unique" json:"service_name"`
	Title       string `gorm:"not null;size:255" json:"title"`
	Abstract    string `gorm:"type:text" json:"abstract"`
	Keywords    StringArray `gorm:"type:text[]" json:"keywords"`

	// 服务类型和配置
	ServiceType string `gorm:"size:50;not null;default:'spatial';check:service_type IN ('spatial', 'table')" json:"service_type"`
	Config      JSONB  `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	PublicAccess bool  `gorm:"default:false" json:"public_access"` // 是否公开访问（无需JWT）

	// 服务参数
	DefaultSRID  int `gorm:"column:default_srid;default:4326" json:"default_srid"`
	MaxFeatures  int `gorm:"default:1000" json:"max_features"`

	// 元数据
	ProviderName string `gorm:"size:255" json:"provider_name"`
	ProviderSite string `gorm:"size:255" json:"provider_site"`
	ContactPerson string `gorm:"size:255" json:"contact_person"`
	ContactEmail string `gorm:"size:255" json:"contact_email"`

	// 存储引擎关联
	EngineID uint `gorm:"not null;index:idx_internal_service_engine" json:"engine_id"`

	// 状态
	Status string `gorm:"size:20;default:'active'" json:"status"` // active, inactive, error

	// 关联
	Layers []InternalServiceLayer `gorm:"foreignKey:ServiceID;constraint:OnDelete:CASCADE" json:"layers,omitempty"`

	// 审计
	CreatedBy uint `gorm:"not null" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (InternalService) TableName() string {
	return "service.internal_services"
}

// GetProtocolConfig 获取指定协议的配置
func (s *InternalService) GetProtocolConfig(protocol string) map[string]interface{} {
	if s.Config == nil {
		return nil
	}
	protocols, ok := s.Config["protocols"].(map[string]interface{})
	if !ok {
		return nil
	}
	config, ok := protocols[protocol].(map[string]interface{})
	if !ok {
		return nil
	}
	return config
}

// IsProtocolEnabled 检查指定协议是否启用
func (s *InternalService) IsProtocolEnabled(protocol string) bool {
	config := s.GetProtocolConfig(protocol)
	if config == nil {
		return false
	}
	enabled, ok := config["enabled"].(bool)
	if !ok {
		return false
	}
	return enabled
}

// AllowMultipleLayers 是否允许多图层
func (s *InternalService) AllowMultipleLayers() bool {
	if s.ServiceType == "table" {
		return false
	}
	if s.Config != nil {
		if allow, ok := s.Config["allow_multiple_layers"].(bool); ok {
			return allow
		}
	}
	// 默认空间服务允许多图层
	return s.ServiceType == "spatial"
}

// IsSpatialService 是否为空间服务
func (s *InternalService) IsSpatialService() bool {
	return s.ServiceType == "spatial"
}

// CreateInternalServiceRequest 创建内部服务请求
type CreateInternalServiceRequest struct {
	ServiceName string   `json:"service_name" binding:"required"`
	Title       string   `json:"title" binding:"required"`
	Abstract    string   `json:"abstract"`
	Keywords    []string `json:"keywords"`

	// 服务类型
	ServiceType string `json:"service_type" binding:"required,oneof=spatial table"`

	// 协议配置
	ProtocolsConfig map[string]interface{} `json:"protocols_config"`

	// 空间服务专用（ServiceType == 'spatial' 时需要）
	DefaultSRID *int `json:"default_srid"`

	MaxFeatures  int  `json:"max_features" binding:"omitempty,gte=1,lte=10000"`
	PublicAccess bool `json:"public_access"`

	ProviderName  string `json:"provider_name"`
	ProviderSite  string `json:"provider_site"`
	ContactPerson string `json:"contact_person"`
	ContactEmail  string `json:"contact_email"`

	EngineID uint `json:"engine_id" binding:"required"`

	// 第一个图层配置（创建服务时自动创建）
	FirstLayer *AddLayerRequest `json:"first_layer"`
}

// UpdateInternalServiceRequest 更新内部服务请求
type UpdateInternalServiceRequest struct {
	Title    *string  `json:"title,omitempty"`
	Abstract *string  `json:"abstract,omitempty"`
	Keywords []string `json:"keywords,omitempty"`

	// 协议配置更新
	ProtocolsConfig map[string]interface{} `json:"protocols_config,omitempty"`
	PublicAccess    *bool                  `json:"public_access,omitempty"`

	DefaultSRID *int `json:"default_srid,omitempty"`
	MaxFeatures *int `json:"max_features,omitempty"`

	ProviderName  *string `json:"provider_name,omitempty"`
	ProviderSite  *string `json:"provider_site,omitempty"`
	ContactPerson *string `json:"contact_person,omitempty"`
	ContactEmail  *string `json:"contact_email,omitempty"`

	Status *string `json:"status,omitempty" binding:"omitempty,oneof=active inactive error"`
}

// InternalServiceDTO 内部服务 DTO
type InternalServiceDTO struct {
	ID uint `json:"id"`

	TenantID    uint     `json:"tenant_id"`
	ServiceName string   `json:"service_name"`
	Title       string   `json:"title"`
	Abstract    string   `json:"abstract"`
	Keywords    []string `json:"keywords"`

	ServiceType  string                 `json:"service_type"`
	Config       map[string]interface{} `json:"config"`
	PublicAccess bool                   `json:"public_access"`

	DefaultSRID int `json:"default_srid"`
	MaxFeatures int `json:"max_features"`

	ProviderName  string `json:"provider_name"`
	ProviderSite  string `json:"provider_site"`
	ContactPerson string `json:"contact_person"`
	ContactEmail  string `json:"contact_email"`

	EngineID uint   `json:"engine_id"`
	Status   string `json:"status"`

	Layers    []InternalServiceLayerDTO `json:"layers,omitempty"`
	CreatedBy uint                      `json:"created_by"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}
