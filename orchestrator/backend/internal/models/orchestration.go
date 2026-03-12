package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Orchestration 编排定义
type Orchestration struct {
	ID                  uint           `gorm:"primaryKey" json:"id"`
	TenantID            uint           `gorm:"index;not null" json:"tenant_id"`
	Name                string         `gorm:"size:128;not null" json:"name"`
	Description         string         `gorm:"size:512" json:"description"`
	Steps               Steps          `gorm:"type:jsonb;not null" json:"steps"`
	Enabled             bool           `gorm:"default:false" json:"enabled"`
	Schedule            string         `gorm:"size:128;column:schedule" json:"schedule,omitempty"`
	LastRunAt           *time.Time     `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time     `json:"next_run_at,omitempty"`
	LastExecutionID     *string        `gorm:"size:36" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string        `gorm:"size:20" json:"last_execution_status,omitempty"`
	CreatedBy           *uint          `json:"created_by,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
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
	ID   string `json:"id"`   // 唯一ID
	Name string `json:"name"` // 步骤名称

	// 模式一：引擎调用（工作流引擎，如 Spark、Python）
	EngineIdentifier string `json:"engine_identifier,omitempty"` // "meta.scanner.default"

	// 模式二：任务引用（引用已有的 TaskProvider 任务定义）
	Provider string `json:"provider,omitempty"`  // "meta" | "transfer" | "develop" | "manager"
	TaskType string `json:"task_type,omitempty"` // "scan" | "import" | "mvt_generation" 等
	TaskID   uint   `json:"task_id,omitempty"`   // 具体任务定义 ID

	Parameters map[string]interface{} `json:"parameters"` // 请求参数
	DependsOn  []string               `json:"depends_on"` // 依赖步骤 ID 列表
	Timeout    int                    `json:"timeout"`    // 超时秒数
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
