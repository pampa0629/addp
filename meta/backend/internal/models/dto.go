package models

import (
	commonModels "github.com/addp/common/models"
)

// 直接使用 Common 模块的类型，避免重复定义
type JSONMap = commonModels.JSONMap

// ScanRequest 扫描请求
type ScanRequest struct {
	EngineID     uint     `json:"engine_id"`     // 资源ID
	CatalogPaths []string `json:"catalog_paths"` // 要扫描的 catalog 路径列表（空则全部）
	NodeID       uint     `json:"node_id"`       // 要扫描的节点 ID
	ItemID       uint     `json:"item_id"`       // 要扫描的数据项 ID
	Targets      []string `json:"targets"`       // locator 目标列表
	ScanDepth    string   `json:"scan_depth"`    // basic/deep
	TriggerType  string   `json:"trigger_type"`  // manual/scheduled
	Force        bool     `json:"force"`         // 是否强制重新扫描
}

// ScanResponse 扫描响应
type ScanResponse struct {
	Status              string `json:"status"` // success/failed
	Message             string `json:"message"`
	CatalogNodesScanned int    `json:"catalog_nodes_scanned"`
	ItemsScanned        int    `json:"items_scanned"`
	FieldsScanned       int    `json:"fields_scanned"`
	DurationMs          int64  `json:"duration_ms"`
	StartedAt           string `json:"started_at"`
}

// ResourceWithStats 资源及其扫描统计
type ResourceWithStats struct {
	EngineID              uint   `json:"id"`   // 前端期待 id
	ResourceName          string `json:"name"` // 前端期待 name
	ResourceType          string `json:"resource_type"`
	EngineFamily          string `json:"engine_family,omitempty"`
	CatalogTopTerm        string `json:"catalog_top_term,omitempty"`
	CatalogTopI18nKey     string `json:"catalog_top_i18n_key,omitempty"`
	CatalogItemTerm       string `json:"catalog_item_term,omitempty"`
	CatalogItemI18nKey    string `json:"catalog_item_i18n_key,omitempty"`
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
	ScanDepth    string   `json:"scan_depth"`
	Force        bool     `json:"force"`
	Schedule     string   `json:"schedule"` // Cron 表达式，空字符串表示手动执行
	Enabled      bool     `json:"enabled"`
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
	GeometryColumn string      `json:"geometry_column"`
	GeometryTypes  []string    `json:"geometry_types,omitempty"` // 几何类型列表，如 ["ST_MultiPolygon"]
	SRID           int         `json:"srid"`
	ExtentSRID     int         `json:"extent_srid"`
	Extent         []float64   `json:"extent"` // [minLng, minLat, maxLng, maxLat]
	PrimaryKey     string      `json:"primary_key"`
	Fields         []FieldInfo `json:"fields"`
	RowCount       int64       `json:"row_count"` // 表记录数（从 meta_item.row_count 获取）
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	IsPrimaryKey bool   `json:"primary_key,omitempty"`
}
