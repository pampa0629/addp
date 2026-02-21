package models

import "time"

// Classification 数据分类（树形，用户自定义）
type Classification struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64      `gorm:"not null;index" json:"tenant_id"`
	Name        string     `gorm:"size:100;not null" json:"name"`
	Code        string     `gorm:"size:50;not null" json:"code"`
	Description string     `gorm:"type:text" json:"description"`
	ParentID    *int64     `gorm:"index" json:"parent_id,omitempty"`
	SortOrder   int        `gorm:"default:0" json:"sort_order"`
	CreatedBy   int64      `gorm:"not null" json:"created_by"`
	UpdatedBy   *int64     `json:"updated_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Classification) TableName() string {
	return "standard.classifications"
}

// GradingLevel 数据分级（固定 L1-L4，用户可修改名称/描述/颜色）
type GradingLevel struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index" json:"tenant_id"`
	Level       string    `gorm:"size:10;not null" json:"level"`    // L1/L2/L3/L4
	Name        string    `gorm:"size:50;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	Color       string    `gorm:"size:20" json:"color"`             // 十六进制颜色
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (GradingLevel) TableName() string {
	return "standard.grading_levels"
}

// CreateClassificationRequest 创建数据分类请求
type CreateClassificationRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateClassificationRequest 更新数据分类请求
type UpdateClassificationRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateGradingLevelRequest 更新数据分级请求（只允许修改名称/描述/颜色，不允许增删）
type UpdateGradingLevelRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}
