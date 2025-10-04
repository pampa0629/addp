package models

import (
	"time"
)

// MetadataField 字段级元数据 (Level 2 - 深度扫描)
type MetadataField struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	TableID       uint      `gorm:"not null;index" json:"table_id"`
	TenantID      uint      `gorm:"not null;index" json:"tenant_id"`
	Name          string    `gorm:"column:field_name;size:255;not null" json:"field_name"`
	FieldPosition int       `json:"field_position,omitempty"`                   // 字段顺序
	DataType      string    `gorm:"size:100" json:"data_type,omitempty"`        // 数据类型
	ColumnType    string    `gorm:"size:255" json:"column_type,omitempty"`      // 完整类型定义（如 varchar(100)）
	IsNullable    bool      `json:"is_nullable"`            // 是否可空
	ColumnKey     string    `gorm:"size:20" json:"column_key,omitempty"`        // PRI, UNI, MUL
	ColumnDefault string    `gorm:"type:text" json:"column_default,omitempty"`  // 默认值
	Extra         string    `gorm:"size:100" json:"extra,omitempty"`            // auto_increment 等
	FieldComment  string    `gorm:"type:text" json:"field_comment,omitempty"`   // 字段注释
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (MetadataField) TableName() string {
	return "fields"
}
