package models

import "time"

type MaterializationGroup struct {
	ID          int64                        `gorm:"primaryKey" json:"id"`
	TenantID    int64                        `gorm:"not null;index" json:"tenant_id"`
	Code        string                       `gorm:"size:100;not null" json:"code"`
	Name        string                       `gorm:"size:200;not null" json:"name"`
	Description string                       `gorm:"type:text;not null;default:''" json:"description"`
	Version     int64                        `gorm:"not null;default:1" json:"version"`
	CreatedBy   int64                        `gorm:"not null" json:"created_by"`
	UpdatedBy   int64                        `gorm:"not null" json:"updated_by"`
	CreatedAt   time.Time                    `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time                    `gorm:"not null" json:"updated_at"`
	Members     []MaterializationGroupMember `gorm:"foreignKey:GroupID" json:"members"`
}

func (MaterializationGroup) TableName() string { return "model.materialization_groups" }

type MaterializationGroupMember struct {
	GroupID        int64 `gorm:"primaryKey" json:"group_id"`
	TenantID       int64 `gorm:"not null;index" json:"tenant_id"`
	LogicalTableID int64 `gorm:"primaryKey" json:"logical_table_id"`
	Position       int   `gorm:"not null" json:"position"`
}

func (MaterializationGroupMember) TableName() string {
	return "model.materialization_group_members"
}
