package models

import "time"

// DWLayer 数仓分层定义
type DWLayer struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index" json:"tenant_id"`
	LayerCode   string    `gorm:"size:20;not null" json:"layer_code"`  // Tenant 内唯一的自定义分层编码
	LayerName   string    `gorm:"size:100;not null" json:"layer_name"` // 贴源层/明细层/汇总层/应用层
	Description string    `gorm:"type:text" json:"description"`
	NamingRule  string    `gorm:"type:text" json:"naming_rule"` // 命名规范（如：dwd_{domain}_{entity}_d）
	QualitySLA  JSONB     `gorm:"type:jsonb;serializer:json" json:"quality_sla"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	Version     int64     `gorm:"not null;default:1" json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (DWLayer) TableName() string {
	return "model.dw_layers"
}

// CreateDWLayerRequest 创建数仓分层请求
type CreateDWLayerRequest struct {
	LayerCode   string                 `json:"layer_code" binding:"required,max=20" maxLength:"20"`
	LayerName   string                 `json:"layer_name" binding:"required,max=100" maxLength:"100"`
	Description string                 `json:"description"`
	NamingRule  string                 `json:"naming_rule"`
	QualitySLA  map[string]interface{} `json:"quality_sla"`
	SortOrder   int                    `json:"sort_order" binding:"gte=0" minimum:"0"`
}

// UpdateDWLayerRequest 更新数仓分层请求
type UpdateDWLayerRequest struct {
	Version     int64                  `json:"version" binding:"required,gt=0" minimum:"1"`
	LayerName   string                 `json:"layer_name" binding:"required,max=100" maxLength:"100"`
	Description string                 `json:"description"`
	NamingRule  string                 `json:"naming_rule"`
	QualitySLA  map[string]interface{} `json:"quality_sla" extensions:"x-nullable"`
	SortOrder   *int                   `json:"sort_order" binding:"required,gte=0" minimum:"0"`
}
