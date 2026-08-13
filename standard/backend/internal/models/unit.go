package models

import "time"

// MeasurementCategory 度量类别
type MeasurementCategory struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index;uniqueIndex:uq_standard_measurement_categories_tenant_code" json:"tenant_id"`
	Name        string    `gorm:"size:100;not null" json:"name"`
	Code        string    `gorm:"size:50;not null;uniqueIndex:uq_standard_measurement_categories_tenant_code" json:"code"`
	Description string    `gorm:"type:text" json:"description"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	IsSystem    bool      `gorm:"default:false" json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int64     `gorm:"not null;default:1" json:"version"`
}

func (MeasurementCategory) TableName() string {
	return "standard.measurement_categories"
}

// Unit 计量单位
type Unit struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index" json:"tenant_id"`
	CategoryID  int64     `gorm:"not null;index" json:"category_id"`
	Name        string    `gorm:"size:100;not null" json:"name"`
	Symbol      string    `gorm:"size:30" json:"symbol"`
	Description string    `gorm:"type:text" json:"description"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	IsSystem    bool      `gorm:"default:false" json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int64     `gorm:"not null;default:1" json:"version"`

	Category *MeasurementCategory `gorm:"foreignKey:CategoryID;-:migration" json:"category,omitempty"`
}

func (Unit) TableName() string {
	return "standard.units"
}

// CreateMeasurementCategoryRequest 创建度量类别请求
type CreateMeasurementCategoryRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateMeasurementCategoryRequest 更新度量类别请求
type UpdateMeasurementCategoryRequest struct {
	Version     int64  `json:"version" binding:"required"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// CreateUnitRequest 创建计量单位请求
type CreateUnitRequest struct {
	CategoryID  int64  `json:"category_id" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateUnitRequest 更新计量单位请求
type UpdateUnitRequest struct {
	Version     int64  `json:"version" binding:"required"`
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}
