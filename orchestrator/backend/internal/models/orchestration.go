package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
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

	// 任务引用（引用已有的 TaskProvider 任务定义）
	Provider string `json:"provider,omitempty"`  // "meta" | "transfer" | "develop" | "manager"
	TaskType string `json:"task_type,omitempty"` // "scan" | "import" | "workflow" | "mvt_generation" 等
	TaskID   uint   `json:"task_id,omitempty"`   // 具体任务定义 ID

	Parameters map[string]interface{} `json:"parameters"` // 请求参数
	DependsOn  []string               `json:"depends_on"` // 依赖步骤 ID 列表
	Timeout    int                    `json:"timeout"`    // 超时秒数
}

// UnmarshalJSON 拒绝旧的非 TaskProvider Step 字段，避免请求里混入旧模式后被静默忽略。
func (s *Step) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	allowed := map[string]struct{}{
		"id":         {},
		"name":       {},
		"provider":   {},
		"task_type":  {},
		"task_id":    {},
		"parameters": {},
		"depends_on": {},
		"timeout":    {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unsupported orchestration step field %q", key)
		}
	}

	type plainStep Step
	var decoded plainStep
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = Step(decoded)
	return nil
}

// ValidateSteps 校验 Orchestrator Step 只能引用已有 TaskProvider 任务。
func ValidateSteps(steps Steps) error {
	if len(steps) == 0 {
		return fmt.Errorf("steps is required")
	}

	seen := map[string]struct{}{}
	for i, step := range steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("steps[%d].id is required", i)
		}
		if _, exists := seen[step.ID]; exists {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		seen[step.ID] = struct{}{}

		if strings.TrimSpace(step.Name) == "" {
			return fmt.Errorf("steps[%d].name is required", i)
		}
		if strings.TrimSpace(step.Provider) == "" {
			return fmt.Errorf("steps[%d].provider is required", i)
		}
		if strings.TrimSpace(step.TaskType) == "" {
			return fmt.Errorf("steps[%d].task_type is required", i)
		}
		if step.TaskID == 0 {
			return fmt.Errorf("steps[%d].task_id is required", i)
		}
		for _, depID := range step.DependsOn {
			if strings.TrimSpace(depID) == "" {
				return fmt.Errorf("steps[%d].depends_on contains empty step id", i)
			}
		}
	}

	for i, step := range steps {
		for _, depID := range step.DependsOn {
			if _, exists := seen[depID]; !exists {
				return fmt.Errorf("steps[%d].depends_on references unknown step %q", i, depID)
			}
		}
	}

	if err := validateStepDAG(steps); err != nil {
		return err
	}

	return nil
}

func validateStepDAG(steps Steps) error {
	graph := map[string][]string{}
	for _, step := range steps {
		graph[step.ID] = append([]string{}, step.DependsOn...)
	}

	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(stepID string) error {
		if visiting[stepID] {
			return fmt.Errorf("steps contains circular dependency at %q", stepID)
		}
		if visited[stepID] {
			return nil
		}

		visiting[stepID] = true
		for _, depID := range graph[stepID] {
			if err := visit(depID); err != nil {
				return err
			}
		}
		visiting[stepID] = false
		visited[stepID] = true
		return nil
	}

	for stepID := range graph {
		if err := visit(stepID); err != nil {
			return err
		}
	}
	return nil
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
