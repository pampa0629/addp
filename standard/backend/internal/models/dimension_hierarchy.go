package models

import "time"

// DimensionHierarchy 维度层级定义（如"时间层级"年→季→月→日，"地区层级"国→省→市→区）
// 属于语义/治理层，描述业务上的上下钻路径，与物理表无关
type DimensionHierarchy struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64      `gorm:"not null;index" json:"tenant_id"`
	DomainID    *int64     `gorm:"index" json:"domain_id,omitempty"`
	Name        string     `gorm:"size:200;not null" json:"name"`
	Code        string     `gorm:"size:100;not null" json:"code"`
	Description string     `gorm:"type:text" json:"description"`
	CreatedBy   int64      `gorm:"not null" json:"created_by"`
	UpdatedBy   *int64     `json:"updated_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Levels      []DimensionHierarchyLevel `gorm:"foreignKey:HierarchyID;constraint:OnDelete:CASCADE" json:"levels,omitempty"`
}

func (DimensionHierarchy) TableName() string {
	return "standard.dimension_hierarchies"
}

// DimensionHierarchyLevel 层级中的每一层（如时间层级的"年"、"季度"、"月"、"日"）
type DimensionHierarchyLevel struct {
	ID          int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	HierarchyID int64  `gorm:"not null;index" json:"hierarchy_id"`
	LevelNum    int    `gorm:"not null" json:"level_num"`    // 1=最粗粒度，数字越大粒度越细
	Name        string `gorm:"size:100;not null" json:"name"` // e.g., "年", "季度", "月", "日"
	ElementID   *int64 `json:"element_id,omitempty"`          // 可选关联 standard.elements（无 DB FK 约束）
	Description string `gorm:"type:text" json:"description"`
	SortOrder   int    `gorm:"default:0" json:"sort_order"`
}

func (DimensionHierarchyLevel) TableName() string {
	return "standard.dimension_hierarchy_levels"
}

// CreateDimensionHierarchyRequest 创建维度层级请求
type CreateDimensionHierarchyRequest struct {
	DomainID    *int64  `json:"domain_id,omitempty"`
	Name        string  `json:"name" binding:"required"`
	Code        string  `json:"code" binding:"required"`
	Description string  `json:"description"`
}

// UpdateDimensionHierarchyRequest 更新维度层级请求
type UpdateDimensionHierarchyRequest struct {
	DomainID    *int64  `json:"domain_id,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
}

// UpsertHierarchyLevelRequest 新增或更新层级中的一层
type UpsertHierarchyLevelRequest struct {
	LevelNum    int    `json:"level_num" binding:"required"`
	Name        string `json:"name" binding:"required"`
	ElementID   *int64 `json:"element_id,omitempty"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}
