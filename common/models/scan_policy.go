package models

// ScanPolicy 表示模块间传递的扫描策略载荷，不对应任何持久化表字段。
type ScanPolicy struct {
	Enabled        bool                     `json:"enabled"`                   // 是否启用扫描（总开关）
	ImmediateScan  bool                     `json:"immediate_scan"`            // 注册或保存后立即扫描
	ImmediateDepth string                   `json:"immediate_depth,omitempty"` // 立即扫描深度：basic 或 deep
	ScheduledScan  bool                     `json:"scheduled_scan"`            // 启用定时扫描
	ScheduleMode   string                   `json:"schedule_mode"`             // daily, weekly, monthly, cron
	CronExpression string                   `json:"cron_expression,omitempty"` // Cron 表达式（schedule_mode=cron 时使用）
	ScheduleTime   string                   `json:"schedule_time,omitempty"`   // 执行时间 HH:mm
	ScheduleValue  []int                    `json:"schedule_value,omitempty"`  // 周几（0-6）或月几（1-31）
	ScanDepth      string                   `json:"scan_depth"`                // 默认扫描深度：basic 或 deep
	Preprocessing  *ScanPreprocessingPolicy `json:"preprocessing,omitempty"`   // 预处理策略（可选）
}

// ScanPreprocessingPolicy 表示扫描后的预处理策略载荷。
type ScanPreprocessingPolicy struct {
	Enabled     bool                    `json:"enabled"`
	AutoTrigger bool                    `json:"auto_trigger"`
	Types       []string                `json:"types"`
	MVTConfig   *MVTPreprocessingPolicy `json:"mvt_config,omitempty"`
}

// MVTPreprocessingPolicy 表示 MVT 瓦片预处理策略载荷。
type MVTPreprocessingPolicy struct {
	MaxZoom          int     `json:"max_zoom"`
	Concurrency      int     `json:"concurrency"`
	StopThresholdSec float64 `json:"stop_threshold_sec"`
	StopThresholdKB  float64 `json:"stop_threshold_kb"`
}
