package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SpatialTask GIS 工作流任务定义
type SpatialTask struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
	Name        string         `gorm:"size:128;not null" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	WorkflowDef WorkflowDef    `gorm:"type:jsonb;not null" json:"workflow_def"`
	InputSchema *InputSchema   `gorm:"type:jsonb" json:"input_schema,omitempty"`
	OutputSchema *OutputSchema `gorm:"type:jsonb" json:"output_schema,omitempty"`
	Schedule    string         `gorm:"size:100" json:"schedule,omitempty"` // Cron 表达式
	Status      string         `gorm:"size:20;default:'active'" json:"status"`

	// 最后执行信息
	LastExecutionID         *uuid.UUID `gorm:"type:uuid" json:"last_execution_id,omitempty"`
	LastExecutionStatus     string     `gorm:"size:20" json:"last_execution_status,omitempty"`
	LastExecutionStartedAt  *time.Time `json:"last_execution_started_at,omitempty"`
	LastExecutionFinishedAt *time.Time `json:"last_execution_finished_at,omitempty"`

	// 审计字段
	CreatedBy uint           `gorm:"index" json:"created_by"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (SpatialTask) TableName() string {
	return "develop.spatial_tasks"
}

// WorkflowDef 工作流定义
type WorkflowDef struct {
	Tasks []WorkflowTask `json:"tasks"`
}

// Value 实现 driver.Valuer 接口
func (w WorkflowDef) Value() (driver.Value, error) {
	return json.Marshal(w)
}

// Scan 实现 sql.Scanner 接口
func (w *WorkflowDef) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, w)
}

// WorkflowTask 工作流任务节点
type WorkflowTask struct {
	ID        string                 `json:"id"`
	Operator  string                 `json:"operator"`
	Params    map[string]interface{} `json:"params"`
	DependsOn []string               `json:"depends_on"`
}

// InputSchema 输入参数定义
type InputSchema struct {
	Fields []InputField `json:"fields"`
}

// Value 实现 driver.Valuer 接口
func (i InputSchema) Value() (driver.Value, error) {
	return json.Marshal(i)
}

// Scan 实现 sql.Scanner 接口
func (i *InputSchema) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, i)
}

// InputField 输入字段定义
type InputField struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"` // "geojson", "float", "int", "string"
	Required     bool        `json:"required"`
	DefaultValue interface{} `json:"default_value,omitempty"`
	Description  string      `json:"description,omitempty"`
}

// OutputSchema 输出定义
type OutputSchema struct {
	Type        string            `json:"type"` // "geojson", "table"
	Description string            `json:"description,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
}

// Value 实现 driver.Valuer 接口
func (o OutputSchema) Value() (driver.Value, error) {
	return json.Marshal(o)
}

// Scan 实现 sql.Scanner 接口
func (o *OutputSchema) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, o)
}

// SpatialExecutionResult 空间执行结果（存储到 PostGIS）
type SpatialExecutionResult struct {
	ID          uint                   `gorm:"primaryKey" json:"id"`
	ExecutionID uuid.UUID              `gorm:"type:uuid;not null;index" json:"execution_id"`
	TaskID      *uint                  `gorm:"index" json:"task_id,omitempty"`
	TenantID    uint                   `gorm:"not null;index" json:"tenant_id"`
	Geom        string                 `gorm:"type:geometry(GEOMETRY,4326)" json:"geom"` // PostGIS 几何字段
	Properties  map[string]interface{} `gorm:"type:jsonb" json:"properties,omitempty"`
	CreatedBy   uint                   `json:"created_by"`
	CreatedAt   time.Time              `json:"created_at"`
}

func (SpatialExecutionResult) TableName() string {
	return "develop.spatial_execution_results"
}
