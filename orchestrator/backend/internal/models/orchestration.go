package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Orchestration 编排定义
type Orchestration struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    uint           `gorm:"index;not null" json:"tenant_id"`
	Name        string         `gorm:"size:128;not null" json:"name"`
	Description string         `gorm:"size:512" json:"description"`
	Steps       Steps          `gorm:"type:jsonb;not null" json:"steps"`
	Enabled     bool           `gorm:"default:false" json:"enabled"`
	CronExpr    string         `gorm:"size:128" json:"cron_expr,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Orchestration) TableName() string {
	return "orchestrator.orchestrations"
}

// Steps DAG 步骤列表
type Steps []Step

func (s Steps) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *Steps) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// Step 单个步骤
type Step struct {
	ID         string                 `json:"id"`         // 唯一ID
	Name       string                 `json:"name"`       // 步骤名称

	// 新架构：使用 EngineIdentifier 动态查找引擎
	EngineIdentifier string `json:"engine_identifier,omitempty"` // "meta.scanner.default"

	// 旧架构：硬编码模块名（向后兼容）
	Module     string                 `json:"module"`     // "transfer"/"meta"/"manager"（已废弃）
	Action     string                 `json:"action"`     // "execute"/"scan"/"cache"（已废弃）
	Endpoint   string                 `json:"endpoint"`   // "/api/tasks/:id/execute"（已废弃）
	Method     string                 `json:"method"`     // "POST"/"GET"（已废弃）

	Parameters map[string]interface{} `json:"parameters"` // 请求参数
	DependsOn  []string               `json:"depends_on"` // 依赖步骤 ID 列表
	Timeout    int                    `json:"timeout"`    // 超时秒数
}

// Execution 执行实例
type Execution struct {
	ID              uint        `gorm:"primaryKey" json:"id"`
	OrchestrationID uint        `gorm:"index;not null" json:"orchestration_id"`
	TenantID        uint        `gorm:"index;not null" json:"tenant_id"`
	Status          string      `gorm:"size:32;not null" json:"status"` // "running"/"completed"/"failed"
	CurrentStep     string      `gorm:"size:64" json:"current_step"`
	StepResults     StepResults `gorm:"type:jsonb" json:"step_results"`
	ErrorMessage    string      `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt       *time.Time  `json:"started_at,omitempty"`
	CompletedAt     *time.Time  `json:"completed_at,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
}

func (Execution) TableName() string {
	return "orchestrator.executions"
}

// StepResults 步骤结果
type StepResults map[string]StepResult

func (r StepResults) Value() (driver.Value, error) {
	return json.Marshal(r)
}

func (r *StepResults) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, r)
}

// StepResult 单个步骤结果
type StepResult struct {
	Status    string                 `json:"status"` // "success"/"failed"
	Result    map[string]interface{} `json:"result"`
	Error     string                 `json:"error,omitempty"`
	StartedAt time.Time              `json:"started_at"`
	EndedAt   time.Time              `json:"ended_at"`
	Duration  int64                  `json:"duration"` // 毫秒
}
