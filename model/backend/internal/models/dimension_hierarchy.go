package models

import "time"

// DimensionHierarchy is a drill path owned by one dimension logical table.
// It is an aggregate child and therefore uses the parent LogicalTable version.
type DimensionHierarchy struct {
	ID          int64                     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64                     `gorm:"not null;index" json:"tenant_id"`
	TableID     int64                     `gorm:"not null;index" json:"table_id"`
	Name        string                    `gorm:"size:200;not null" json:"name"`
	Description string                    `gorm:"type:text;not null;default:''" json:"description"`
	CreatedAt   time.Time                 `json:"created_at"`
	UpdatedAt   time.Time                 `json:"updated_at"`
	Levels      []DimensionHierarchyLevel `gorm:"foreignKey:HierarchyID" json:"levels"`
}

func (DimensionHierarchy) TableName() string { return "model.dimension_hierarchies" }

type DimensionHierarchyLevel struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	HierarchyID int64     `gorm:"not null;index" json:"hierarchy_id"`
	FieldID     int64     `gorm:"not null;index" json:"field_id"`
	LevelNum    int       `gorm:"not null" json:"level_num"`
	LevelName   string    `gorm:"size:100;not null" json:"level_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (DimensionHierarchyLevel) TableName() string { return "model.dimension_hierarchy_levels" }

type CreateDimensionHierarchyRequest struct {
	Version     int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	Name        string `json:"name" binding:"required,max=200" maxLength:"200"`
	Description string `json:"description"`
}

type UpdateDimensionHierarchyRequest struct {
	Version     int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	Name        string `json:"name" binding:"required,max=200" maxLength:"200"`
	Description string `json:"description"`
}

type UpsertDimensionHierarchyLevelRequest struct {
	Version   int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	FieldID   int64  `json:"field_id" binding:"required,gt=0" minimum:"1"`
	LevelNum  int    `json:"level_num" binding:"required,gt=0" minimum:"1"`
	LevelName string `json:"level_name" binding:"required,max=100" maxLength:"100"`
}

type DimensionHierarchyMutationResponse struct {
	Hierarchy DimensionHierarchy `json:"hierarchy"`
	Version   int64              `json:"version"`
}

type DimensionHierarchyLevelMutationResponse struct {
	Level   DimensionHierarchyLevel `json:"level"`
	Version int64                   `json:"version"`
}
