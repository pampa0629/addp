package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// ConnectionInfo 定义连接信息类型，支持 GORM JSONB 序列化
type ConnectionInfo map[string]interface{}

const (
	EngineLifecycleActive   = "active"
	EngineLifecycleDisabled = "disabled"
	EngineLifecycleDeleting = "deleting"

	ExternalArtifactPolicyDelete  = "delete"
	ExternalArtifactPolicyAbandon = "abandon"
)

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
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	case nil:
		return nil
	default:
		return fmt.Errorf("unsupported ConnectionInfo scan type: %T", value)
	}
}

// Engine 引擎信息（对应 system.engines 表）
type Engine struct {
	ID             uint           `gorm:"column:id" json:"id"`
	TenantID       *uint          `gorm:"column:tenant_id;index" json:"tenant_id"`                              // 租户ID，平台共享引擎为 null
	Name           string         `gorm:"column:name;not null;size:255;index" json:"name"`                      // 显示名称（原 display_name）
	EngineType     string         `gorm:"column:engine_type;not null;index" json:"engine_type"`                 // 引擎类型（postgresql, mysql, acme_geo_workflow 等）
	EngineOrigin   string         `gorm:"column:engine_origin;not null;default:'general'" json:"engine_origin"` // 引擎来源：general 或 extension
	ConnectionInfo ConnectionInfo `gorm:"column:connection_info;type:json;not null" json:"connection_info"`
	Description    string         `gorm:"column:description;type:text" json:"description"`
	LifecycleState string         `gorm:"column:lifecycle_state;size:20;not null;default:'active';index" json:"lifecycle_state"`
	CreatedBy      *uint          `gorm:"column:created_by" json:"created_by,omitempty"`

	DeletionScanTaskID     *string    `gorm:"column:deletion_scan_task_id;size:64" json:"deletion_scan_task_id,omitempty"`
	DeletionExecuteTaskID  *string    `gorm:"column:deletion_execute_task_id;size:64" json:"deletion_execute_task_id,omitempty"`
	DeletionError          string     `gorm:"column:deletion_error;type:text" json:"deletion_error,omitempty"`
	DeletionRequestedAt    *time.Time `gorm:"column:deletion_requested_at" json:"deletion_requested_at,omitempty"`
	DeletionRequestedBy    *uint      `gorm:"column:deletion_requested_by" json:"deletion_requested_by,omitempty"`
	ExternalArtifactPolicy string     `gorm:"column:external_artifact_policy;size:20;not null;default:'delete'" json:"external_artifact_policy"`

	// 扩展引擎字段
	IsBuiltin        bool              `gorm:"column:is_builtin;default:false;index" json:"is_builtin"`      // 是否为内置引擎（内置引擎不可删除）
	Capabilities     *JSONString       `gorm:"column:capabilities;type:jsonb" json:"capabilities,omitempty"` // 能力声明（JSONB）
	CapabilitiesView *CapabilitiesView `gorm:"-" json:"capabilities_view,omitempty"`                         // System 后端生成的能力展示模型

	// 最近连接检测结果缓存（优化扫描性能，不代表持续保持的物理连接）
	ConnectionStatus string     `gorm:"column:connection_status;size:20;default:'unknown';index" json:"connection_status"` // online/offline/unknown/checking
	LastCheckAt      *time.Time `gorm:"column:last_check_at" json:"last_check_at,omitempty"`                               // 上次检测时间
	CheckMessage     string     `gorm:"column:check_message;type:text" json:"check_message,omitempty"`                     // 检测结果消息（错误信息等）

	// 时间戳字段
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// EngineRuntimeEndpoint is the non-secret control-plane endpoint of a
// workflow, script, or federated query runtime. Data-engine connection details never belong here.
type EngineRuntimeEndpoint struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

// EngineRuntimeDescriptor is System's masked control-plane projection for
// runtime discovery. It intentionally has no connection_info field.
type EngineRuntimeDescriptor struct {
	ID               uint                   `json:"id"`
	Name             string                 `json:"name"`
	EngineType       string                 `json:"engine_type"`
	EngineOrigin     string                 `json:"engine_origin"`
	Description      string                 `json:"description"`
	LifecycleState   string                 `json:"lifecycle_state"`
	IsBuiltin        bool                   `json:"is_builtin"`
	Capabilities     *JSONString            `json:"capabilities,omitempty"`
	ConnectionStatus string                 `json:"connection_status"`
	RuntimeEndpoint  *EngineRuntimeEndpoint `json:"runtime_endpoint,omitempty"`
}

func (d *EngineRuntimeDescriptor) AsEngine() *Engine {
	if d == nil {
		return nil
	}
	connectionInfo := ConnectionInfo{}
	if d.RuntimeEndpoint != nil {
		connectionInfo["protocol"] = d.RuntimeEndpoint.Protocol
		connectionInfo["host"] = d.RuntimeEndpoint.Host
		connectionInfo["port"] = d.RuntimeEndpoint.Port
	}
	return &Engine{
		ID: d.ID, Name: d.Name, EngineType: d.EngineType, EngineOrigin: d.EngineOrigin,
		Description: d.Description, LifecycleState: d.LifecycleState, IsBuiltin: d.IsBuiltin,
		Capabilities: d.Capabilities, ConnectionStatus: d.ConnectionStatus,
		ConnectionInfo: connectionInfo,
	}
}

func (e *Engine) IsUsable() bool {
	return e != nil && e.LifecycleState == EngineLifecycleActive
}
