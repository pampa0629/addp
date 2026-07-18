package models

import (
	"database/sql/driver"
	"encoding/json"
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

// OrchestrationDefinitionRequest contains only user-editable orchestration fields.
type OrchestrationDefinitionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       Steps  `json:"steps"`
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule"`
}

func (r OrchestrationDefinitionRequest) ApplyTo(orch *Orchestration) {
	orch.Name = r.Name
	orch.Description = r.Description
	orch.Steps = r.Steps
	orch.Enabled = r.Enabled
	orch.Schedule = r.Schedule
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
	Provider string `json:"provider,omitempty"`  // TaskProvider module name, e.g. "meta" | "develop" | "orchestrator"
	TaskType string `json:"task_type,omitempty"` // TaskProvider task type, e.g. "scan" | "workflow" | "orchestration"
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
			return &StepDecodeError{Code: StepDecodeUnsupportedField, Field: key}
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
		return &StepValidationError{Code: StepValidationStepsRequired, StepIndex: -1}
	}

	seen := map[string]struct{}{}
	for i, step := range steps {
		if strings.TrimSpace(step.ID) == "" {
			return &StepValidationError{Code: StepValidationIDRequired, StepIndex: i}
		}
		if _, exists := seen[step.ID]; exists {
			return &StepValidationError{Code: StepValidationDuplicateID, StepIndex: i, StepID: step.ID}
		}
		seen[step.ID] = struct{}{}

		if strings.TrimSpace(step.Name) == "" {
			return &StepValidationError{Code: StepValidationNameRequired, StepIndex: i, StepID: step.ID}
		}
		if strings.TrimSpace(step.Provider) == "" {
			return &StepValidationError{Code: StepValidationProviderRequired, StepIndex: i, StepID: step.ID}
		}
		if strings.TrimSpace(step.TaskType) == "" {
			return &StepValidationError{Code: StepValidationTaskTypeRequired, StepIndex: i, StepID: step.ID}
		}
		if step.TaskID == 0 {
			return &StepValidationError{Code: StepValidationTaskIDRequired, StepIndex: i, StepID: step.ID}
		}
		for _, depID := range step.DependsOn {
			if strings.TrimSpace(depID) == "" {
				return &StepValidationError{Code: StepValidationDependencyEmpty, StepIndex: i, StepID: step.ID}
			}
		}
	}

	for i, step := range steps {
		for _, depID := range step.DependsOn {
			if _, exists := seen[depID]; !exists {
				return &StepValidationError{Code: StepValidationDependencyUnknown, StepIndex: i, StepID: step.ID, Reference: depID}
			}
		}
		if err := validateStepTemplateReferences(i, step, seen); err != nil {
			return err
		}
	}

	if err := validateStepDAG(steps); err != nil {
		return err
	}

	return nil
}

func validateStepTemplateReferences(index int, step Step, knownSteps map[string]struct{}) error {
	dependencies := map[string]struct{}{}
	for _, depID := range step.DependsOn {
		dependencies[depID] = struct{}{}
	}

	for _, ref := range collectTemplateStepReferences(step.Parameters) {
		if _, exists := knownSteps[ref]; !exists {
			return &StepValidationError{Code: StepValidationTemplateUnknownStep, StepIndex: index, StepID: step.ID, Reference: ref}
		}
		if ref == step.ID {
			return &StepValidationError{Code: StepValidationTemplateSelfReference, StepIndex: index, StepID: step.ID, Reference: ref}
		}
		if _, exists := dependencies[ref]; !exists {
			return &StepValidationError{Code: StepValidationTemplateMissingDependency, StepIndex: index, StepID: step.ID, Reference: ref}
		}
	}
	return nil
}

func collectTemplateStepReferences(value interface{}) []string {
	seen := map[string]struct{}{}
	refs := make([]string, 0)
	var walk func(interface{})
	walk = func(current interface{}) {
		switch typed := current.(type) {
		case string:
			ref, ok := parseTemplateStepReference(typed)
			if !ok {
				return
			}
			if _, exists := seen[ref]; !exists {
				seen[ref] = struct{}{}
				refs = append(refs, ref)
			}
		case map[string]interface{}:
			for _, nested := range typed {
				walk(nested)
			}
		case []interface{}:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(value)
	return refs
}

func parseTemplateStepReference(template string) (string, bool) {
	trimmed := strings.TrimSpace(template)
	if len(trimmed) < 5 || !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return "", false
	}
	path := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	parts := strings.Split(path, ".")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", false
	}
	return strings.TrimSpace(parts[0]), true
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
			return &StepValidationError{Code: StepValidationCircularDependency, StepIndex: -1, StepID: stepID}
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
