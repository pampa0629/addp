package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// PreparedQueryInfo 记录准备检查后瓦片生成实际使用的查询对象。
type PreparedQueryInfo struct {
	MaterializedViewExists bool   `json:"materialized_view_exists"`
	QueryTable             string `json:"query_table"`
	QueryGeomColumn        string `json:"query_geom_column"`
	QuerySRID              int    `json:"query_srid"`
}

// PreparationExecution 记录准备动作执行信息，用于任务执行 metadata 和故障排查。
type PreparationExecution struct {
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	DurationSec float64   `json:"duration_sec"`
	ExecutorID  string    `json:"executor_id"`
	TaskID      string    `json:"task_id"`
	RetryCount  int       `json:"retry_count"`
	LastError   string    `json:"last_error,omitempty"`
}

// PreparationStatus 表达瓦片缓存生成任务内部的准备检查结果。
type PreparationStatus struct {
	Version       string                `json:"version"`
	Checks        []PreparationCheck    `json:"checks"`
	OverallStatus string                `json:"overall_status"`
	Summary       string                `json:"summary"`
	CompletedAt   time.Time             `json:"completed_at"`
	QueryInfo     *PreparedQueryInfo    `json:"query_info,omitempty"`
	ExecutionInfo *PreparationExecution `json:"execution_info,omitempty"`
}

func (ps *PreparationStatus) Scan(value interface{}) error {
	if value == nil {
		*ps = PreparationStatus{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, ps)
}

func (ps PreparationStatus) Value() (driver.Value, error) {
	return json.Marshal(ps)
}

// PreparationCheck 是单个准备检查项。
type PreparationCheck struct {
	Name      string                 `json:"name"`
	Status    string                 `json:"status"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details"`
	CheckedAt time.Time              `json:"checked_at"`
}
