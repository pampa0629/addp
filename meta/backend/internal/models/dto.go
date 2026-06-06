package models

import (
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
)

// 直接使用 Common 模块的类型，避免重复定义
type JSONMap = commonModels.JSONMap

// ScanRequest 扫描请求
type ScanRequest struct {
	EngineID     uint           `json:"engine_id"`     // 存储引擎 ID
	CatalogPaths []string       `json:"catalog_paths"` // 要扫描的 catalog 路径列表（空则全部）
	RefGroups    []ScanRefGroup `json:"ref_groups"`    // 内容引用组
	NodeID       uint           `json:"node_id"`       // 要扫描的节点 ID
	ItemID       uint           `json:"item_id"`       // 要扫描的数据项 ID
	Targets      []string       `json:"targets"`       // locator 目标列表
	ScanDepth    string         `json:"scan_depth"`    // basic/deep
	TriggerType  string         `json:"trigger_type"`  // manual；空值按 manual 处理
	Source       string         `json:"source"`        // 扫描来源
	Force        bool           `json:"force"`         // 是否强制重新扫描
}

// ScanRefGroup 一组共同参与数据项识别的内容引用
type ScanRefGroup struct {
	Primary string    `json:"primary"`
	Refs    []ScanRef `json:"refs"`
}

// ScanRef 内容引用
type ScanRef struct {
	Path     string `json:"path"`
	Role     string `json:"role"`
	Required bool   `json:"required"`
}

// ScanResponse 扫描响应
type ScanResponse struct {
	Status              string               `json:"status"` // success/failed
	Message             string               `json:"message"`
	CatalogNodesScanned int                  `json:"catalog_nodes_scanned"`
	ItemsScanned        int                  `json:"items_scanned"`
	FieldsScanned       int                  `json:"fields_scanned"`
	DurationMs          int64                `json:"duration_ms"`
	StartedAt           string               `json:"started_at"`
	Extraction          *ExtractionScanStats `json:"extraction,omitempty"`
}

type ExtractionScanStats struct {
	Documents   int `json:"documents"`
	Extracted   int `json:"extracted"`
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`
	Indexed     int `json:"indexed"`
	IndexFailed int `json:"index_failed"`
}

// ResourceWithStats 存储引擎及其 catalog 扫描统计
type ResourceWithStats struct {
	EngineID              uint   `json:"id"`   // 前端期待 id
	ResourceName          string `json:"name"` // 前端期待 name
	ResourceType          string `json:"resource_type"`
	EngineFamily          string `json:"engine_family,omitempty"`
	CatalogTopTerm        string `json:"catalog_top_term,omitempty"`
	CatalogTopI18nKey     string `json:"catalog_top_i18n_key,omitempty"`
	CatalogLeafTerm       string `json:"catalog_leaf_term,omitempty"`
	CatalogLeafI18nKey    string `json:"catalog_leaf_i18n_key,omitempty"`
	CatalogRootTerm       string `json:"catalog_root_term,omitempty"`
	TotalCatalogNodes     int    `json:"total_catalog_nodes"`
	ScannedCatalogNodes   int    `json:"scanned_catalog_nodes"`
	UnscannedCatalogNodes int    `json:"unscanned_catalog_nodes"`
	ScannedAt             string `json:"scanned_at,omitempty"`
	// 连接状态相关字段
	ConnectionStatus string `json:"connection_status,omitempty"` // online/offline/unknown/checking
	LastCheckAt      string `json:"last_check_at,omitempty"`     // 最后检测时间
	CheckMessage     string `json:"check_message,omitempty"`     // 状态详情
}

// ScanTaskUpsertRequest 创建或更新扫描任务的请求
type ScanTaskUpsertRequest struct {
	Name         string   `json:"name" binding:"required"`
	Description  string   `json:"description"`
	EngineID     uint     `json:"engine_id" binding:"required"`
	CatalogPaths []string `json:"catalog_paths"`
	Scope        JSONMap  `json:"scope"`
	ScanDepth    string   `json:"scan_depth"`
	Force        bool     `json:"force"`
	Schedule     string   `json:"schedule"` // Cron 表达式，空字符串表示手动执行
	Enabled      bool     `json:"enabled"`
}

// EngineScanTaskPolicyRequest 是 Console 为指定 engine 提交的 Meta 扫描计划。
type EngineScanTaskPolicyRequest struct {
	EngineName string            `json:"engine_name"`
	ScanPolicy *EngineScanPolicy `json:"scan_policy"`
}

// EngineScanPolicy 是 Console -> Meta API 使用的 engine 扫描策略载荷。
type EngineScanPolicy struct {
	Enabled        bool                           `json:"enabled"`                   // 是否启用扫描（总开关）
	ImmediateScan  bool                           `json:"immediate_scan"`            // 注册或保存后立即扫描
	ImmediateDepth string                         `json:"immediate_depth,omitempty"` // 立即扫描深度：basic 或 deep
	ScheduledScan  bool                           `json:"scheduled_scan"`            // 是否启用定时扫描
	ScheduleMode   string                         `json:"schedule_mode"`             // daily, weekly, monthly, cron
	CronExpression string                         `json:"cron_expression,omitempty"` // Cron 表达式（schedule_mode=cron）
	ScheduleTime   string                         `json:"schedule_time,omitempty"`   // 执行时间 HH:mm
	ScheduleValue  []int                          `json:"schedule_value,omitempty"`  // 周几（0-6）或月几（1-31）
	ScanDepth      string                         `json:"scan_depth"`                // 默认扫描深度：basic 或 deep
	Preprocessing  *EngineScanPreprocessingPolicy `json:"preprocessing,omitempty"`   // 扫描后预处理策略
}

// EngineScanPreprocessingPolicy 是 engine 扫描后的预处理策略。
type EngineScanPreprocessingPolicy struct {
	Enabled     bool                         `json:"enabled"`
	AutoTrigger bool                         `json:"auto_trigger"`
	Types       []string                     `json:"types"`
	MVTConfig   *EngineScanMVTPreprocessPlan `json:"mvt_config,omitempty"`
}

// EngineScanMVTPreprocessPlan 是 MVT 瓦片预处理策略。
type EngineScanMVTPreprocessPlan struct {
	MaxZoom          int     `json:"max_zoom"`
	Concurrency      int     `json:"concurrency"`
	StopThresholdSec float64 `json:"stop_threshold_sec"`
	StopThresholdKB  float64 `json:"stop_threshold_kb"`
}

// ToCommonScanPolicy 将 Meta API 边界策略转换为内部任务调度复用结构。
func (p *EngineScanPolicy) ToCommonScanPolicy() *commonModels.ScanPolicy {
	if p == nil {
		return nil
	}
	policy := &commonModels.ScanPolicy{
		Enabled:        p.Enabled,
		ImmediateScan:  p.ImmediateScan,
		ImmediateDepth: p.ImmediateDepth,
		ScheduledScan:  p.ScheduledScan,
		ScheduleMode:   p.ScheduleMode,
		CronExpression: p.CronExpression,
		ScheduleTime:   p.ScheduleTime,
		ScheduleValue:  p.ScheduleValue,
		ScanDepth:      p.ScanDepth,
	}
	if p.Preprocessing != nil {
		policy.Preprocessing = &commonModels.ScanPreprocessingPolicy{
			Enabled:     p.Preprocessing.Enabled,
			AutoTrigger: p.Preprocessing.AutoTrigger,
			Types:       p.Preprocessing.Types,
		}
		if p.Preprocessing.MVTConfig != nil {
			policy.Preprocessing.MVTConfig = &commonModels.MVTPreprocessingPolicy{
				MaxZoom:          p.Preprocessing.MVTConfig.MaxZoom,
				Concurrency:      p.Preprocessing.MVTConfig.Concurrency,
				StopThresholdSec: p.Preprocessing.MVTConfig.StopThresholdSec,
				StopThresholdKB:  p.Preprocessing.MVTConfig.StopThresholdKB,
			}
		}
	}
	return policy
}

// MetadataTreeResponse 元数据树响应（用于 Manager 查询）
type MetadataTreeResponse struct {
	TopNodes   []MetaNodeLite `json:"top_nodes"`
	ChildNodes []MetaNodeLite `json:"child_nodes"`
	Items      []MetaItemLite `json:"items"`
}

// MetaNodeLite 元数据节点简化模型（用于返回给 Manager）
type MetaNodeLite struct {
	ID             uint                   `json:"id"`
	TenantID       uint                   `json:"tenant_id"`
	EngineID       uint                   `json:"engine_id"`
	ParentNodeID   *uint                  `json:"parent_node_id,omitempty"`
	NodeType       string                 `json:"node_type"`
	Name           string                 `json:"name"`
	FullName       string                 `json:"full_name"`
	Depth          int                    `json:"depth"`
	Path           string                 `json:"path"`
	ScanStatus     string                 `json:"scan_status"`
	ScannedDepth   string                 `json:"scanned_depth"`
	ScannedAt      *string                `json:"scanned_at,omitempty"`
	ItemCount      int                    `json:"item_count"`
	TotalSizeBytes int64                  `json:"total_size_bytes"`
	Attributes     map[string]interface{} `json:"attributes,omitempty"`
}

// MetaItemLite 元数据项简化模型（用于返回给 Manager）
type MetaItemLite struct {
	ID            uint                   `json:"id"`
	TenantID      uint                   `json:"tenant_id"`
	EngineID      uint                   `json:"engine_id"`
	NodeID        uint                   `json:"node_id"`
	ItemType      string                 `json:"item_type"`
	Name          string                 `json:"name"`
	FullName      string                 `json:"full_name"`
	RowCount      *int64                 `json:"row_count,omitempty"`
	SizeBytes     *int64                 `json:"size_bytes,omitempty"`
	DataUpdatedAt *string                `json:"data_updated_at,omitempty"`
	ScannedAt     *string                `json:"scanned_at,omitempty"`
	ScannedDepth  string                 `json:"scanned_depth"`
	Attributes    map[string]interface{} `json:"attributes,omitempty"`
}

// SpatialMetadataResponse 空间元数据响应（用于 Manager MVT 瓦片生成）
type SpatialMetadataResponse struct {
	GeometryColumn string                  `json:"geometry_column"`
	GeometryTypes  []string                `json:"geometry_types,omitempty"` // 几何类型列表，如 ["ST_MultiPolygon"]
	SRID           int                     `json:"srid"`
	CRSRef         string                  `json:"crs_ref,omitempty"`
	CRSDefinition  *datatype.CRSDefinition `json:"crs_definition,omitempty"`
	ExtentSRID     int                     `json:"extent_srid"`
	Extent         []float64               `json:"extent"` // [minLng, minLat, maxLng, maxLat]
	PrimaryKey     string                  `json:"primary_key"`
	Fields         []datatype.FieldInfo    `json:"fields"`
	RowCount       int64                   `json:"row_count"` // 表记录数（从 meta_item.row_count 获取）
}
