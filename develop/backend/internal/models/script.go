package models

import (
	"time"

	"gorm.io/gorm"
)

// Script SQL脚本
type Script struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"size:255;not null" json:"name"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	SQLContent  string         `gorm:"type:text;not null" json:"sql_content"`
	ResourceID  uint           `gorm:"not null;index" json:"resource_id"` // 目标数据源ID
	Version     int            `gorm:"default:1" json:"version"`
	Status      string         `gorm:"size:20;default:'draft';index" json:"status"` // draft, published, archived
	CreatedBy   uint           `gorm:"not null;index" json:"created_by"`
	TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名
func (Script) TableName() string {
	return "develop.scripts"
}

// ScriptVersion 脚本版本历史
type ScriptVersion struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ScriptID          uint      `gorm:"not null;index" json:"script_id"`
	Version           int       `gorm:"not null" json:"version"`
	SQLContent        string    `gorm:"type:text;not null" json:"sql_content"`
	ChangeDescription string    `gorm:"type:text" json:"change_description,omitempty"`
	CreatedBy         uint      `gorm:"not null" json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
}

// TableName 指定表名
func (ScriptVersion) TableName() string {
	return "develop.script_versions"
}

// ScriptDependency 脚本依赖关系
type ScriptDependency struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	ScriptID           uint      `gorm:"not null;index" json:"script_id"`
	DependsOnScriptID  uint      `gorm:"not null;index" json:"depends_on_script_id"`
	CreatedAt          time.Time `json:"created_at"`
}

// TableName 指定表名
func (ScriptDependency) TableName() string {
	return "develop.script_dependencies"
}
