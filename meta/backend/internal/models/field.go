package models

import (
	"time"
)

// MetadataField 字段级元数据
type MetadataField struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	TableID         uint      `gorm:"not null;index:idx_table_tenant" json:"table_id"`
	TenantID        uint      `gorm:"not null;index:idx_table_tenant" json:"tenant_id"`
	FieldName       string    `gorm:"size:255;not null" json:"field_name"`

	// 字段属性
	DataType        string    `gorm:"size:100;not null" json:"data_type"`        // 数据类型
	ColumnType      string    `gorm:"size:255" json:"column_type"`               // 完整类型(如 varchar(255))
	IsNullable      bool      `json:"is_nullable"`                               // 是否可空
	DefaultValue    string    `gorm:"type:text" json:"default_value,omitempty"`  // 默认值
	ColumnComment   string    `gorm:"type:text" json:"column_comment"`           // 字段注释

	// 字段约束
	IsPrimaryKey    bool      `json:"is_primary_key"`                            // 是否主键
	IsUniqueKey     bool      `json:"is_unique_key"`                             // 是否唯一键
	IsForeignKey    bool      `json:"is_foreign_key"`                            // 是否外键

	// 位置信息
	OrdinalPosition int       `json:"ordinal_position"`                          // 字段顺序

	// 字符集信息（字符串类型）
	CharacterSet    string    `gorm:"size:50" json:"character_set,omitempty"`
	Collation       string    `gorm:"size:50" json:"collation,omitempty"`

	// 数值信息（数值类型）
	NumericPrecision int      `json:"numeric_precision,omitempty"`               // 数值精度
	NumericScale     int      `json:"numeric_scale,omitempty"`                   // 数值标度

	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (MetadataField) TableName() string {
	return "fields"
}
