package events

import "time"

// 事件类型常量
const (
	// EventEngineDeleted - 引擎删除事件
	EventEngineDeleted = "engine.deleted"
	// EventTenantDeleted - 租户删除事件
	EventTenantDeleted = "tenant.deleted"
	// EventCleanupRequest - 清理请求事件
	EventCleanupRequest = "cleanup.request"
)

// 清理动作类型
const (
	CleanupActionScan    = "scan"    // 扫描垃圾数据
	CleanupActionExecute = "execute" // 执行清理
)

// 删除类型
const (
	DeleteTypeSoft = "soft_delete" // 软删除（标记deleted_at）
	DeleteTypeHard = "hard_delete" // 物理删除
)

// 模块名称常量
const (
	ModuleMeta     = "meta"
	ModuleManager  = "manager"
	ModuleTransfer = "transfer"
	ModuleDevelop  = "develop"
	ModuleService  = "service"
)

// EngineDeletedEvent - Engine删除事件
type EngineDeletedEvent struct {
	EngineID  uint      `json:"engine_id"`
	TenantID  uint      `json:"tenant_id"`
	DeletedAt time.Time `json:"deleted_at"`
	DeletedBy uint      `json:"deleted_by"`
}

// TenantDeletedEvent - 租户删除事件
type TenantDeletedEvent struct {
	TenantID  uint      `json:"tenant_id"`
	DeletedAt time.Time `json:"deleted_at"`
	DeletedBy uint      `json:"deleted_by"`
}

// CleanupRequestEvent - 清理请求事件
type CleanupRequestEvent struct {
	TaskID          string    `json:"task_id"`            // 任务ID
	Action          string    `json:"action"`             // scan/execute
	TenantID        uint      `json:"tenant_id"`          // 租户ID（0=全局）
	DeleteType      string    `json:"delete_type"`        // soft_delete/hard_delete
	ExpectedModules []string  `json:"expected_modules"`   // 期望响应的模块列表
	BasedOnScan     string    `json:"based_on_scan"`      // 基于哪次扫描（execute时使用）
	RequestedBy     uint      `json:"requested_by"`       // 请求用户ID
	RequestedAt     time.Time `json:"requested_at"`       // 请求时间
}

// CleanupResultData - 清理结果数据（各模块写入Redis）
type CleanupResultData struct {
	Module      string                 `json:"module"`              // 模块名称
	Status      string                 `json:"status"`              // success/failed/timeout
	Timestamp   time.Time              `json:"timestamp"`           // 完成时间
	Statistics  map[string]interface{} `json:"statistics"`          // 统计数据
	Details     interface{}            `json:"details,omitempty"`   // 详细信息
	Error       string                 `json:"error,omitempty"`     // 错误信息
}
