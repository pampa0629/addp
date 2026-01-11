package models

// CleanupTask - 清理任务元数据
type CleanupTask struct {
	TaskID          string     `json:"task_id"`
	Action          string     `json:"action"`                    // scan/execute
	TenantID        uint       `json:"tenant_id"`
	DeleteType      string     `json:"delete_type,omitempty"`    // soft_delete/hard_delete
	Status          string     `json:"status"`                   // pending/running/completed/timeout/failed
	ExpectedModules []string   `json:"expected_modules"`
	RequestedBy     uint       `json:"requested_by"`
	StartedAt       string     `json:"started_at"`
	CompletedAt     *string    `json:"completed_at,omitempty"`
	TimeoutAt       string     `json:"timeout_at"`
	BasedOnScan     string     `json:"based_on_scan,omitempty"`
}

// TaskStatusResponse - 任务状态响应
type TaskStatusResponse struct {
	TaskID   string                 `json:"task_id"`
	Action   string                 `json:"action"`
	Status   string                 `json:"status"`
	Progress TaskProgress           `json:"progress"`
	Results  map[string]interface{} `json:"results"` // key=module, value=CleanupResultData
	Summary  interface{}            `json:"summary"`
	Task     CleanupTask            `json:"task"`
}

// TaskProgress - 任务进度
type TaskProgress struct {
	Total     int               `json:"total"`     // 期望模块数
	Completed int               `json:"completed"` // 已完成模块数
	Modules   map[string]string `json:"modules"`   // key=module, value=status
}

// TaskSummary - 任务汇总（仅scan任务）
type TaskSummary struct {
	TotalItemsToClean int     `json:"total_items_to_clean"`
	TotalSizeMB       float64 `json:"total_size_mb"`
	RiskLevel         string  `json:"risk_level"` // low/medium/high
}

// ExecuteSummary - 执行汇总（execute任务）
type ExecuteSummary struct {
	TotalDeleted int     `json:"total_deleted"`
	FreedSpaceMB float64 `json:"freed_space_mb"`
	HasErrors    bool    `json:"has_errors"`
}
