package models

// 统一任务状态枚举（所有计算引擎遵循）
const (
	TaskStatusCreated   = "created"    // 已创建，未执行
	TaskStatusPending   = "pending"    // 等待执行（队列中）
	TaskStatusRunning   = "running"    // 执行中
	TaskStatusCompleted = "completed"  // 成功完成
	TaskStatusFailed    = "failed"     // 执行失败
	TaskStatusCancelled = "cancelled"  // 已取消
)

// TaskStatus 任务状态信息（统一返回格式）
type TaskStatus struct {
	TaskID   string                 `json:"task_id"`             // 任务 ID
	Status   string                 `json:"status"`              // 任务状态（使用上述常量）
	Progress float64                `json:"progress,omitempty"`  // 进度（0-100）
	Message  string                 `json:"message,omitempty"`   // 状态消息
	Data     map[string]interface{} `json:"data,omitempty"`      // 额外数据
}
