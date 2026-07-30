package events

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// 事件类型常量
const (
	// EventCleanupRequest - 资源回收请求事件
	EventCleanupRequest = "cleanup.request"
)

// 资源回收生命周期事件名称。
const (
	CleanupCauseEngineDeleting = "engine.deleting"
	CleanupCauseEngineDeleted  = "engine.deleted"
	CleanupCauseTenantDeleted  = "tenant.deleted"
)

// 资源回收动作类型
const (
	CleanupActionScan    = "scan"    // 评估可回收资源
	CleanupActionExecute = "execute" // 执行资源回收
)

// 资源回收模式
const (
	CleanupModeLogical  = "logical_cleanup"  // 逻辑清理
	CleanupModePhysical = "physical_cleanup" // 物理清理
)

func ValidateCleanupMode(cleanupMode string) error {
	switch cleanupMode {
	case CleanupModeLogical, CleanupModePhysical:
		return nil
	default:
		return fmt.Errorf("cleanup_mode must be %s or %s", CleanupModeLogical, CleanupModePhysical)
	}
}

// 资源回收结果状态
const (
	CleanupResultSuccess        = "success"
	CleanupResultFailed         = "failed"
	CleanupResultPartialSuccess = "partial_success"
	CleanupResultSkipped        = "skipped"
	CleanupResultTimeout        = "timeout"
)

const (
	CleanupImpactRebindable       = "rebindable"
	CleanupImpactWillDisable      = "will_disable"
	CleanupImpactWillDelete       = "will_delete"
	CleanupImpactRunning          = "running"
	CleanupImpactExternalArtifact = "external_artifact"
)

// 模块名称常量
const (
	ModuleMeta     = "meta"
	ModuleManager  = "manager"
	ModuleTransfer = "transfer"
	ModuleDevelop  = "develop"
	ModuleService  = "service"
	ModuleQuality  = "quality"
	ModuleGraph    = "graph"
	ModuleAsset    = "asset"
	ModuleModel    = "model"
	ModuleStandard = "standard"
)

// TenantDeletedEvent - 租户删除事件
type TenantDeletedEvent struct {
	TenantID  uint      `json:"tenant_id"`
	DeletedAt time.Time `json:"deleted_at"`
	DeletedBy uint      `json:"deleted_by"`
}

// CleanupRequestEvent - 资源回收请求事件
type CleanupRequestEvent struct {
	TaskID            string                 `json:"task_id"`                       // 任务ID
	Action            string                 `json:"action"`                        // scan/execute
	TenantID          uint                   `json:"tenant_id"`                     // 租户ID（0=全局）
	CleanupMode       string                 `json:"cleanup_mode,omitempty"`        // logical_cleanup/physical_cleanup
	TriggerType       string                 `json:"trigger_type"`                  // manual/scheduled/event
	CauseEvent        string                 `json:"cause_event,omitempty"`         // 生命周期触发事件
	ExpectedModules   []string               `json:"expected_modules"`              // 期望响应的模块列表
	BasedOnScan       string                 `json:"based_on_scan,omitempty"`       // 基于哪次扫描（execute时使用）
	ParentExecutionID string                 `json:"parent_execution_id,omitempty"` // System 父 execution
	Context           map[string]interface{} `json:"context,omitempty"`             // 中性上下文
	RequestedBy       uint                   `json:"requested_by"`                  // 请求用户ID
	RequestedAt       time.Time              `json:"requested_at"`                  // 请求时间
}

// CleanupResultSummary - 资源回收结果标准摘要
type CleanupResultSummary struct {
	ScannedItems             int    `json:"scanned_items,omitempty"`
	AffectedRecords          int    `json:"affected_records,omitempty"`
	DeletedPhysicalArtifacts int    `json:"deleted_physical_artifacts,omitempty"`
	FreedBytes               int64  `json:"freed_bytes,omitempty"`
	MarkedMissingSource      int    `json:"marked_missing_source,omitempty"`
	MarkedOutdated           int    `json:"marked_outdated,omitempty"`
	DisabledTaskDefinitions  int    `json:"disabled_task_definitions,omitempty"`
	SkippedItems             int    `json:"skipped_items,omitempty"`
	ErrorCount               int    `json:"error_count,omitempty"`
	RiskLevel                string `json:"risk_level,omitempty"`
}

// CleanupImpactSummary 是 Engine 删除评估中跨模块共享的影响分类摘要。
type CleanupImpactSummary struct {
	Rebindable       int `json:"rebindable,omitempty"`
	WillDisable      int `json:"will_disable,omitempty"`
	WillDelete       int `json:"will_delete,omitempty"`
	Running          int `json:"running,omitempty"`
	ExternalArtifact int `json:"external_artifact,omitempty"`
}

func (s CleanupImpactSummary) Total() int {
	return s.Rebindable + s.WillDisable + s.WillDelete + s.Running + s.ExternalArtifact
}

func (s *CleanupImpactSummary) Add(other CleanupImpactSummary) {
	s.Rebindable += other.Rebindable
	s.WillDisable += other.WillDisable
	s.WillDelete += other.WillDelete
	s.Running += other.Running
	s.ExternalArtifact += other.ExternalArtifact
}

// CleanupImpactData 只暴露通用影响摘要和确定性指纹；资源详情仍归 owner 模块。
type CleanupImpactData struct {
	Summary        CleanupImpactSummary `json:"summary"`
	Digest         string               `json:"digest"`
	ManagementPath string               `json:"management_path,omitempty"`
}

// CleanupImpactItem 是 owner 模块计算影响指纹时使用的稳定资源引用。
type CleanupImpactItem struct {
	StableRef   string
	Disposition string
}

func BuildCleanupImpactData(items []CleanupImpactItem, managementPath string) (CleanupImpactData, error) {
	tokens := make([]string, 0, len(items))
	result := CleanupImpactData{ManagementPath: strings.TrimSpace(managementPath)}
	for _, item := range items {
		stableRef := strings.TrimSpace(item.StableRef)
		if stableRef == "" {
			return CleanupImpactData{}, fmt.Errorf("cleanup impact stable_ref is required")
		}
		disposition := strings.TrimSpace(item.Disposition)
		switch disposition {
		case CleanupImpactRebindable:
			result.Summary.Rebindable++
		case CleanupImpactWillDisable:
			result.Summary.WillDisable++
		case CleanupImpactWillDelete:
			result.Summary.WillDelete++
		case CleanupImpactRunning:
			result.Summary.Running++
		case CleanupImpactExternalArtifact:
			result.Summary.ExternalArtifact++
		default:
			return CleanupImpactData{}, fmt.Errorf("unsupported cleanup impact disposition %q", disposition)
		}
		tokens = append(tokens, disposition+":"+stableRef)
	}
	sort.Strings(tokens)
	digest := sha256.Sum256([]byte(strings.Join(tokens, "\n")))
	result.Digest = hex.EncodeToString(digest[:])
	return result, nil
}

// CleanupResultData - 资源回收结果数据（各模块写入 Redis）
type CleanupResultData struct {
	Module      string                 `json:"module"`                 // 模块名称
	Status      string                 `json:"status"`                 // success/failed/partial_success/skipped/timeout
	Action      string                 `json:"action"`                 // scan/execute
	TenantID    uint                   `json:"tenant_id"`              // 租户ID
	TaskID      string                 `json:"task_id"`                // 任务ID
	CleanupMode string                 `json:"cleanup_mode,omitempty"` // logical_cleanup/physical_cleanup
	TriggerType string                 `json:"trigger_type"`           // manual/scheduled/event
	Timestamp   time.Time              `json:"timestamp"`              // 完成时间
	Summary     CleanupResultSummary   `json:"summary"`                // 标准摘要
	Impact      *CleanupImpactData     `json:"impact,omitempty"`       // Engine 删除影响评估
	Statistics  map[string]interface{} `json:"statistics,omitempty"`   // 模块私有统计
	Details     interface{}            `json:"details,omitempty"`      // 详细信息
	Errors      []string               `json:"errors,omitempty"`       // 错误摘要
}

func CleanupExpectedForModule(expected []string, module string) bool {
	if len(expected) == 0 {
		return true
	}
	for _, item := range expected {
		if strings.TrimSpace(item) == module {
			return true
		}
	}
	return false
}
