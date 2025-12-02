package service

const (
	runStatusPending  = "pending"
	runStatusRunning  = "running"
	runStatusSuccess  = "success"
	runStatusFailed   = "failed"
	runStatusCanceled = "canceled"

	triggerTypeManual    = "manual"
	triggerTypeScheduled = "scheduled"
	triggerTypeAuto      = "auto"      // 自动触发（Transfer 完成后等）
	triggerTypeSystem    = "system"    // 系统触发（保留兼容）
)
