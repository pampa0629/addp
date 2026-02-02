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

	// 服务类型配置
	EnabledWFS        bool `gorm:"default:true" json:"enabled_wfs"`
	EnabledOGCAPI     bool `gorm:"default:true" json:"enabled_ogc_api"`
	EnabledWMTS       bool `gorm:"default:true" json:"enabled_wmts"`
	EnabledWMS        bool `gorm:"default:false" json:"enabled_wms"`
	EnabledRestQuery  bool `gorm:"default:true" json:"enabled_rest_query"`  // 简化REST查询API
	PublicAccess      bool `gorm:"default:false" json:"public_access"`       // 是否公开访问（无需JWT）

	// 服务参数
	DefaultSRID  int `gorm:"default:4326" json:"default_srid"`
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

// CreateInternalServiceRequest 创建内部服务请求
type CreateInternalServiceRequest struct {
	ServiceName string `json:"service_name" binding:"required,alphanum"`
	Title       string `json:"title" binding:"required"`
	Abstract    string `json:"abstract"`
	Keywords    []string `json:"keywords"`

	EnabledWFS       bool `json:"enabled_wfs"`
	EnabledOGCAPI    bool `json:"enabled_ogc_api"`
	EnabledWMTS      bool `json:"enabled_wmts"`
	EnabledWMS       bool `json:"enabled_wms"`
	EnabledRestQuery bool `json:"enabled_rest_query"`
	PublicAccess     bool `json:"public_access"`

	DefaultSRID  int `json:"default_srid" binding:"omitempty,oneof=4326 3857 4490 2000 4214"`
	MaxFeatures  int `json:"max_features" binding:"omitempty,gte=1,lte=10000"`

	ProviderName string `json:"provider_name"`
	ProviderSite string `json:"provider_site"`
	ContactPerson string `json:"contact_person"`
	ContactEmail string `json:"contact_email"`

	EngineID uint `json:"engine_id" binding:"required"`
}

// UpdateInternalServiceRequest 更新内部服务请求
type UpdateInternalServiceRequest struct {
	Title        *string `json:"title,omitempty"`
	Abstract     *string `json:"abstract,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`

	EnabledWFS       *bool `json:"enabled_wfs,omitempty"`
	EnabledOGCAPI    *bool `json:"enabled_ogc_api,omitempty"`
	EnabledWMTS      *bool `json:"enabled_wmts,omitempty"`
	EnabledWMS       *bool `json:"enabled_wms,omitempty"`
	EnabledRestQuery *bool `json:"enabled_rest_query,omitempty"`
	PublicAccess     *bool `json:"public_access,omitempty"`

	DefaultSRID  *int `json:"default_srid,omitempty"`
	MaxFeatures  *int `json:"max_features,omitempty"`

	ProviderName *string `json:"provider_name,omitempty"`
	ProviderSite *string `json:"provider_site,omitempty"`
	ContactPerson *string `json:"contact_person,omitempty"`
	ContactEmail *string `json:"contact_email,omitempty"`

	Status *string `json:"status,omitempty" binding:"omitempty,oneof=active inactive error"`
}

// InternalServiceDTO 内部服务 DTO
type InternalServiceDTO struct {
	ID uint `json:"id"`

	TenantID    uint `json:"tenant_id"`
	ServiceName string `json:"service_name"`
	Title       string `json:"title"`
	Abstract    string `json:"abstract"`
	Keywords    []string `json:"keywords"`

	EnabledWFS       bool `json:"enabled_wfs"`
	EnabledOGCAPI    bool `json:"enabled_ogc_api"`
	EnabledWMTS      bool `json:"enabled_wmts"`
	EnabledWMS       bool `json:"enabled_wms"`
	EnabledRestQuery bool `json:"enabled_rest_query"`
	PublicAccess     bool `json:"public_access"`

	DefaultSRID  int `json:"default_srid"`
	MaxFeatures  int `json:"max_features"`

	ProviderName string `json:"provider_name"`
	ProviderSite string `json:"provider_site"`
	ContactPerson string `json:"contact_person"`
	ContactEmail string `json:"contact_email"`

	EngineID uint `json:"engine_id"`
	Status   string `json:"status"`

	Layers    []InternalServiceLayerDTO `json:"layers,omitempty"`
	CreatedBy uint `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
