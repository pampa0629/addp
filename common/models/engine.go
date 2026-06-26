package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// ConnectionInfo 定义连接信息类型，支持 GORM JSONB 序列化
type ConnectionInfo map[string]interface{}

// JSONString accepts either a JSON object/array or a JSON string, then stores it
// as a compact JSON string. It is used for JSONB-backed fields whose HTTP shape
// may be structured while older callers still send strings.
type JSONString string

func (j *JSONString) StringPtr() *string {
	if j == nil {
		return nil
	}
	value := string(*j)
	return &value
}

func (j JSONString) MarshalJSON() ([]byte, error) {
	if j == "" {
		return []byte("null"), nil
	}
	if json.Valid([]byte(j)) {
		return []byte(j), nil
	}
	return json.Marshal(string(j))
}

func (j *JSONString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*j = JSONString(asString)
		return nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	compact, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	*j = JSONString(compact)
	return nil
}

func (j JSONString) Value() (driver.Value, error) {
	if j == "" {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSONString) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*j = JSONString(v)
	case string:
		*j = JSONString(v)
	default:
		return fmt.Errorf("unsupported JSONString scan type: %T", value)
	}
	return nil
}

// Value 实现 driver.Valuer 接口，用于 GORM 写入数据库
func (c ConnectionInfo) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan 实现 sql.Scanner 接口，用于 GORM 从数据库读取
func (c *ConnectionInfo) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

// Engine 引擎信息（对应 system.engines 表）
type Engine struct {
	ID             uint           `gorm:"column:id" json:"id"`
	TenantID       *uint          `gorm:"column:tenant_id;index" json:"tenant_id"`                              // 租户ID，SuperAdmin创建的引擎为null
	Name           string         `gorm:"column:name;not null;size:255;index" json:"name"`                      // 显示名称（原 display_name）
	EngineType     string         `gorm:"column:engine_type;not null;index" json:"engine_type"`                 // 引擎类型（postgresql, mysql, acme_geo_workflow 等）
	EngineOrigin   string         `gorm:"column:engine_origin;not null;default:'general'" json:"engine_origin"` // 引擎来源：general 或 extension
	ConnectionInfo ConnectionInfo `gorm:"column:connection_info;type:json;not null" json:"connection_info"`
	Description    string         `gorm:"column:description;type:text" json:"description"`
	IsActive       bool           `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedBy      *uint          `gorm:"column:created_by" json:"created_by,omitempty"`

	// 扩展引擎字段
	IsBuiltin        bool              `gorm:"column:is_builtin;default:false;index" json:"is_builtin"`      // 是否为内置引擎（内置引擎不可删除）
	Capabilities     *JSONString       `gorm:"column:capabilities;type:jsonb" json:"capabilities,omitempty"` // 能力声明（JSONB）
	CapabilitiesView *CapabilitiesView `gorm:"-" json:"capabilities_view,omitempty"`                         // System 后端生成的能力展示模型

	// 连接状态缓存（优化扫描性能）
	ConnectionStatus string     `gorm:"column:connection_status;size:20;default:'unknown';index" json:"connection_status"` // online/offline/unknown/checking
	LastCheckAt      *time.Time `gorm:"column:last_check_at" json:"last_check_at,omitempty"`                               // 上次检测时间
	CheckMessage     string     `gorm:"column:check_message;type:text" json:"check_message,omitempty"`                     // 检测结果消息（错误信息等）

	// 时间戳字段
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}
