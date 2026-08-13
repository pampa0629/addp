package models

import "time"

// Domain 业务域
type Domain struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index;uniqueIndex:uq_standard_domains_tenant_code" json:"tenant_id"`
	Name        string    `gorm:"size:100;not null" json:"name"`
	Code        string    `gorm:"size:50;not null;uniqueIndex:uq_standard_domains_tenant_code" json:"code"`
	Description string    `gorm:"type:text" json:"description"`
	ParentID    *int64    `gorm:"index" json:"parent_id,omitempty"`
	Icon        string    `gorm:"size:50" json:"icon"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	CreatedBy   int64     `gorm:"not null" json:"created_by"`
	UpdatedBy   *int64    `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int64     `gorm:"not null;default:1" json:"version"`
}

func (Domain) TableName() string {
	return "standard.domains"
}

// CreateDomainRequest 创建业务域请求
type CreateDomainRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	Icon        string `json:"icon"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateDomainRequest 更新业务域请求
type UpdateDomainRequest struct {
	Version     int64  `json:"version" binding:"required"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	Icon        string `json:"icon"`
	SortOrder   int    `json:"sort_order"`
}
