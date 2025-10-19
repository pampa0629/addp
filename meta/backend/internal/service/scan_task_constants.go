package service

const (
	runStatusPending  = "pending"
	runStatusRunning  = "running"
	runStatusSuccess  = "success"
	runStatusFailed   = "failed"
	runStatusCanceled = "canceled"

	triggerTypeManual    = "manual"
	triggerTypeScheduled = "scheduled"
	triggerTypeSystem    = "system"
)
