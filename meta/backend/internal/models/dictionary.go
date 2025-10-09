package models

import (
	"encoding/json"
	"time"
)

// MetaNodeTypeDict 定义节点类型与描述
type MetaNodeTypeDict struct {
	TypeCode    string    `gorm:"primaryKey;size:64" json:"type_code"`
	Category    string    `gorm:"size:64" json:"category,omitempty"`
	Description string    `gorm:"type:text" json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (MetaNodeTypeDict) TableName() string {
	return "meta_node_type_dict"
}

// MetaNodeChildRule 限定父子节点的合法组合
type MetaNodeChildRule struct {
	ParentType string    `gorm:"size:64;primaryKey" json:"parent_type"`
	ChildType  string    `gorm:"size:64;primaryKey" json:"child_type"`
	CreatedAt  time.Time `json:"created_at"`
}

func (MetaNodeChildRule) TableName() string {
	return "meta_node_child_rule"
}

// MetaJSONSchema 存储属性 JSON 的结构定义与版本
type MetaJSONSchema struct {
	ID          uint            `gorm:"primaryKey" json:"id"`
	Target      string          `gorm:"size:32;not null" json:"target"`
	Version     int             `gorm:"not null" json:"version"`
	Definition  json.RawMessage `gorm:"type:jsonb;not null" json:"definition"`
	Description string          `gorm:"type:text" json:"description,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (MetaJSONSchema) TableName() string {
	return "meta_json_schema"
}

// MetaChangeLog 记录元数据同步或手工调整的变更
type MetaChangeLog struct {
	ID           uint            `gorm:"primaryKey" json:"id"`
	TenantID     *uint           `json:"tenant_id,omitempty"`
	ResID        *uint           `json:"res_id,omitempty"`
	NodeID       *uint           `json:"node_id,omitempty"`
	ItemID       *uint           `json:"item_id,omitempty"`
	ChangeType   string          `gorm:"size:64;not null" json:"change_type"`
	ChangeSource string          `gorm:"size:64" json:"change_source,omitempty"`
	Payload      json.RawMessage `gorm:"type:jsonb" json:"payload,omitempty"`
	SyncVersion  *int64          `json:"sync_version,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (MetaChangeLog) TableName() string {
	return "meta_change_log"
}
