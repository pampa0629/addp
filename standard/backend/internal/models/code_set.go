package models

import "time"

// CodeSet 码值集
type CodeSet struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index:idx_codeset_tenant" json:"tenant_id"`
	Code        string    `gorm:"size:100;not null;uniqueIndex:idx_codeset_tenant_code" json:"code"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	Type        string    `gorm:"size:50;default:custom" json:"type"` // system/custom
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (CodeSet) TableName() string {
	return "standard.code_sets"
}

// CodeItem 码值项
type CodeItem struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CodeSetID   int64     `gorm:"not null;index:idx_codeitem_set" json:"code_set_id"`
	Code        string    `gorm:"size:100;not null;uniqueIndex:idx_codeitem_set_code" json:"code"`
	Value       string    `gorm:"size:200;not null" json:"value"`
	Description string    `gorm:"type:text" json:"description"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	IsActive    bool      `gorm:"default:true" json:"is_active"`
	ParentID    *int64    `json:"parent_id"` // 预留树形结构
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (CodeItem) TableName() string {
	return "standard.code_items"
}

// CreateCodeSetRequest 创建码值集请求
type CreateCodeSetRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// UpdateCodeSetRequest 更新码值集请求
type UpdateCodeSetRequest struct {
	Name        string `json:"name" binding:"required"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// CreateCodeItemRequest 创建码值项请求
type CreateCodeItemRequest struct {
	Code        string `json:"code" binding:"required"`
	Value       string `json:"value" binding:"required"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	IsActive    bool   `json:"is_active"`
}

// UpdateCodeItemRequest 更新码值项请求
type UpdateCodeItemRequest struct {
	Value       string `json:"value" binding:"required"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	IsActive    bool   `json:"is_active"`
}
