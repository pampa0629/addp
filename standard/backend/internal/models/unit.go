package models

import (
	"gorm.io/gorm"
	"time"
)

// MeasurementCategory 度量类别
type MeasurementCategory struct {
	ID           int64            `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID     *int64           `gorm:"index;uniqueIndex:uq_standard_measurement_categories_tenant_code" json:"tenant_id,omitempty"`
	Name         string           `gorm:"size:100;not null" json:"name"`
	Code         string           `gorm:"size:50;not null;uniqueIndex:uq_standard_measurement_categories_tenant_code" json:"code"`
	Description  string           `gorm:"type:text" json:"description"`
	SortOrder    int              `gorm:"default:0" json:"sort_order"`
	Translations UnitTranslations `gorm:"type:jsonb;serializer:json" json:"translations,omitempty"`
	ScopeType    string           `gorm:"-" json:"scope_type" enums:"platform,tenant_common"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	Version      int64            `gorm:"not null;default:1" json:"version"`
}

func (MeasurementCategory) TableName() string {
	return "standard.measurement_categories"
}

// Unit 计量单位
type Unit struct {
	ID           int64            `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID     *int64           `gorm:"index" json:"tenant_id,omitempty"`
	Code         string           `gorm:"size:100" json:"code,omitempty"`
	CategoryID   int64            `gorm:"not null;index" json:"category_id"`
	Name         string           `gorm:"size:100;not null" json:"name"`
	Symbol       string           `gorm:"size:30" json:"symbol"`
	Description  string           `gorm:"type:text" json:"description"`
	SortOrder    int              `gorm:"default:0" json:"sort_order"`
	Translations UnitTranslations `gorm:"type:jsonb;serializer:json" json:"translations,omitempty"`
	ScopeType    string           `gorm:"-" json:"scope_type" enums:"platform,tenant_common"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	Version      int64            `gorm:"not null;default:1" json:"version"`

	Category *MeasurementCategory `gorm:"foreignKey:CategoryID;-:migration" json:"category,omitempty"`
}

func (Unit) TableName() string {
	return "standard.units"
}

// CreateMeasurementCategoryRequest 创建度量类别请求
type CreateMeasurementCategoryRequest struct {
	Translations UnitTranslations `json:"translations"`
	Name         string           `json:"name"`
	Code         string           `json:"code" binding:"required"`
	Description  string           `json:"description"`
	SortOrder    int              `json:"sort_order"`
}

// UpdateMeasurementCategoryRequest 更新度量类别请求
type UpdateMeasurementCategoryRequest struct {
	Translations UnitTranslations `json:"translations"`
	Version      int64            `json:"version" binding:"required,gt=0"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	SortOrder    int              `json:"sort_order"`
}

// CreateUnitRequest 创建计量单位请求
type CreateUnitRequest struct {
	Translations UnitTranslations `json:"translations"`
	Code         string           `json:"code"`
	CategoryID   int64            `json:"category_id" binding:"required"`
	Name         string           `json:"name"`
	Symbol       string           `json:"symbol"`
	Description  string           `json:"description"`
	SortOrder    int              `json:"sort_order"`
}

// UpdateUnitRequest 更新计量单位请求
type UpdateUnitRequest struct {
	Translations UnitTranslations `json:"translations"`
	Version      int64            `json:"version" binding:"required,gt=0"`
	Name         string           `json:"name"`
	Symbol       string           `json:"symbol"`
	Description  string           `json:"description"`
	SortOrder    int              `json:"sort_order"`
}

// UnitContext 来自已验证的 AuthContext；平台上下文没有 Tenant。
type UnitContext struct {
	Type     string
	TenantID int64
}

func TenantUnitContext(id int64) UnitContext { return UnitContext{Type: "tenant", TenantID: id} }
func (c UnitContext) Valid() bool {
	return (c.Type == "platform" && c.TenantID == 0) || (c.Type == "tenant" && c.TenantID > 0)
}
func (c UnitContext) OwnerID() *int64 {
	if c.Type == "platform" {
		return nil
	}
	id := c.TenantID
	return &id
}
func (c UnitContext) Owns(id *int64) bool {
	if c.Type == "platform" {
		return id == nil
	}
	return id != nil && *id == c.TenantID
}

type UnitTranslation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Symbol      string `json:"symbol,omitempty"`
}
type UnitTranslations map[string]UnitTranslation

func (t UnitTranslations) Display(lang string) UnitTranslation {
	if v, ok := t[lang]; ok {
		return v
	}
	return t["zh-cn"]
}
func (m *MeasurementCategory) AfterFind(*gorm.DB) error {
	m.ScopeType = unitScope(m.TenantID)
	return nil
}
func (m *Unit) AfterFind(*gorm.DB) error { m.ScopeType = unitScope(m.TenantID); return nil }
func unitScope(id *int64) string {
	if id == nil {
		return "platform"
	}
	return "tenant_common"
}
func (m *MeasurementCategory) Localize(lang string) {
	m.ScopeType = unitScope(m.TenantID)
	if m.TenantID == nil {
		v := m.Translations.Display(lang)
		m.Name = v.Name
		m.Description = v.Description
	}
}
func (m *Unit) Localize(lang string) {
	m.ScopeType = unitScope(m.TenantID)
	if m.TenantID == nil {
		v := m.Translations.Display(lang)
		m.Name = v.Name
		m.Description = v.Description
		m.Symbol = v.Symbol
	}
	if m.Category != nil {
		m.Category.Localize(lang)
	}
}
