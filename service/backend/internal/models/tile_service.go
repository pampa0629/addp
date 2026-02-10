package models

import (
	"time"
)

// ============================================================================
// 瓦片服务模型
// ============================================================================

// TileService 瓦片服务主表模型
type TileService struct {
	ID           uint        `gorm:"primarykey" json:"id"`
	TenantID     uint        `gorm:"not null;index:idx_tile_services_tenant" json:"tenant_id"`
	ServiceName  string      `gorm:"type:varchar(255);not null;uniqueIndex:unique_tile_service_name" json:"service_name"`
	Title        string      `gorm:"type:varchar(255);not null" json:"title"`
	Description  string      `gorm:"type:text" json:"description"`
	Keywords     StringArray `gorm:"type:text[]" json:"keywords"`
	DefaultSRID  int         `gorm:"column:default_srid;type:integer;default:3857" json:"default_srid"`
	Extent       JSONB       `gorm:"type:jsonb" json:"extent"`
	Protocols    JSONB       `gorm:"type:jsonb;not null" json:"protocols"`
	PublicAccess bool        `gorm:"type:boolean;default:false" json:"public_access"`
	Status       string      `gorm:"type:varchar(50);default:'active'" json:"status"`
	ErrorMessage string      `gorm:"type:text" json:"error_message"`
	CreatedBy    uint        `gorm:"not null" json:"created_by"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`

	// 关联：图层列表（用于 Preload）
	Layers []TileServiceLayer `gorm:"foreignKey:ServiceID" json:"layers,omitempty"`
}

// TableName 指定表名
func (TileService) TableName() string {
	return "service.tile_services"
}

// IsXYZEnabled 是否启用 XYZ Tiles 协议
func (ts *TileService) IsXYZEnabled() bool {
	if ts.Protocols == nil {
		return false
	}
	if xyz, ok := ts.Protocols["xyz"].(map[string]interface{}); ok {
		if enabled, ok := xyz["enabled"].(bool); ok {
			return enabled
		}
	}
	return false
}

// IsOGCTilesEnabled 是否启用 OGC Tiles API
func (ts *TileService) IsOGCTilesEnabled() bool {
	if ts.Protocols == nil {
		return false
	}
	if ogcTiles, ok := ts.Protocols["ogc_tiles"].(map[string]interface{}); ok {
		if enabled, ok := ogcTiles["enabled"].(bool); ok {
			return enabled
		}
	}
	return false
}

// ============================================================================
// 瓦片服务图层模型
// ============================================================================

// TileServiceLayer 瓦片服务图层表模型
type TileServiceLayer struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	ServiceID    uint      `gorm:"not null;index:idx_tile_service_layers_service" json:"service_id"`
	LayerName    string    `gorm:"type:varchar(255);not null" json:"layer_name"`
	Title        string    `gorm:"type:varchar(255);not null" json:"title"`
	Description  string    `gorm:"type:text" json:"description"`
	LayerType    string    `gorm:"type:varchar(50);not null" json:"layer_type"` // "dynamic" or "static"
	LayerConfig  JSONB     `gorm:"type:jsonb;not null" json:"layer_config"`
	DisplayOrder int       `gorm:"type:integer;default:0" json:"display_order"`
	Enabled      bool      `gorm:"type:boolean;default:true" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 指定表名
func (TileServiceLayer) TableName() string {
	return "service.tile_service_layers"
}

// IsDynamic 是否为动态图层
func (tsl *TileServiceLayer) IsDynamic() bool {
	return tsl.LayerType == "dynamic"
}

// IsStatic 是否为静态图层
func (tsl *TileServiceLayer) IsStatic() bool {
	return tsl.LayerType == "static"
}

// GetEngineID 获取 Engine ID（仅动态图层有效）
func (tsl *TileServiceLayer) GetEngineID() uint {
	if !tsl.IsDynamic() || tsl.LayerConfig == nil {
		return 0
	}
	if source, ok := tsl.LayerConfig["source"].(map[string]interface{}); ok {
		if engineID, ok := source["engine_id"].(float64); ok {
			return uint(engineID)
		}
	}
	return 0
}

// ============================================================================
// DTO（数据传输对象）
// ============================================================================

// TileServiceDTO 瓦片服务响应 DTO
type TileServiceDTO struct {
	ID           uint                 `json:"id"`
	TenantID     uint                 `json:"tenant_id"`
	ServiceName  string               `json:"service_name"`
	Title        string               `json:"title"`
	Description  string               `json:"description"`
	Keywords     []string             `json:"keywords"`
	DefaultSRID  int                  `json:"default_srid"`
	Extent       interface{}          `json:"extent"`
	Protocols    interface{}          `json:"protocols"`
	PublicAccess bool                 `json:"public_access"`
	Status       string               `json:"status"`
	ErrorMessage string               `json:"error_message,omitempty"`
	CreatedBy    uint                 `json:"created_by"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	Layers       []TileServiceLayerDTO `json:"layers,omitempty"`
	Endpoints    map[string]string    `json:"endpoints,omitempty"` // 服务端点 URL
}

// TileServiceLayerDTO 瓦片服务图层响应 DTO
type TileServiceLayerDTO struct {
	ID           uint        `json:"id"`
	ServiceID    uint        `json:"service_id"`
	LayerName    string      `json:"layer_name"`
	Title        string      `json:"title"`
	Description  string      `json:"description"`
	LayerType    string      `json:"layer_type"`
	LayerConfig  interface{} `json:"layer_config"`
	DisplayOrder int         `json:"display_order"`
	Enabled      bool        `json:"enabled"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// ============================================================================
// 请求对象
// ============================================================================

// CreateTileServiceRequest 创建瓦片服务请求
type CreateTileServiceRequest struct {
	ServiceName  string                   `json:"service_name" binding:"required"`
	Title        string                   `json:"title" binding:"required"`
	Description  string                   `json:"description"`
	Keywords     []string                 `json:"keywords"`
	DefaultSRID  int                      `json:"default_srid"` // 默认 3857
	Protocols    map[string]interface{}   `json:"protocols"`
	PublicAccess bool                     `json:"public_access"`
	FirstLayer   *CreateTileLayerRequest  `json:"first_layer" binding:"required"` // 第一个图层（必需）
}

// CreateTileLayerRequest 创建图层请求
type CreateTileLayerRequest struct {
	LayerName   string                 `json:"layer_name" binding:"required"`
	Title       string                 `json:"title" binding:"required"`
	Description string                 `json:"description"`
	LayerType   string                 `json:"layer_type" binding:"required"` // "dynamic" or "static"
	LayerConfig map[string]interface{} `json:"layer_config" binding:"required"`
}

// UpdateTileServiceRequest 更新瓦片服务请求
type UpdateTileServiceRequest struct {
	Title        *string                `json:"title"`
	Description  *string                `json:"description"`
	Keywords     []string               `json:"keywords"`
	Protocols    map[string]interface{} `json:"protocols"`
	PublicAccess *bool                  `json:"public_access"`
	Status       *string                `json:"status"`
}

// UpdateTileLayerRequest 更新图层请求
type UpdateTileLayerRequest struct {
	Title        *string                `json:"title"`
	Description  *string                `json:"description"`
	LayerConfig  map[string]interface{} `json:"layer_config"`
	DisplayOrder *int                   `json:"display_order"`
	Enabled      *bool                  `json:"enabled"`
}
