package models

import (
	commonModels "github.com/addp/common/models"
)

// 直接使用 Common 模块的类型，避免重复定义
type JSONMap = commonModels.JSONMap

// ScanRequest 扫描请求
type ScanRequest struct {
	EngineID    uint     `json:"engine_id" binding:"required"` // 资源ID
	Namespaces  []string `json:"namespaces"`                   // 要扫描的命名空间列表（空则全部）
	ObjectPaths []string `json:"object_paths"`                 // 对象存储/文件系统选择的路径
	ScanDepth   string   `json:"scan_depth"`                   // basic/deep/full
	ScanType    string   `json:"scan_type"`                    // manual/auto/scheduled
}

// ScanResponse 扫描响应
type ScanResponse struct {
	Status            string `json:"status"` // success/failed
	Message           string `json:"message"`
	NamespacesScanned int    `json:"namespaces_scanned"`
	ItemsScanned      int    `json:"items_scanned"`
	FieldsScanned     int    `json:"fields_scanned"`
	DurationMs        int64  `json:"duration_ms"`
	StartedAt         string `json:"started_at"`
}

// ResourceWithStats 资源及其扫描统计
type ResourceWithStats struct {
	EngineID            uint   `json:"id"`   // 前端期待 id
	ResourceName        string `json:"name"` // 前端期待 name
	ResourceType        string `json:"resource_type"`
	TotalNamespaces     int    `json:"total_namespaces"`
	ScannedNamespaces   int    `json:"scanned_namespaces"`
	UnscannedNamespaces int    `json:"unscanned_namespaces"`
	ScannedAt           string `json:"scanned_at,omitempty"`
	// 连接状态相关字段
	ConnectionStatus string `json:"connection_status,omitempty"` // online/offline/unknown/checking
	LastCheckAt      string `json:"last_check_at,omitempty"`     // 最后检测时间
	CheckMessage     string `json:"check_message,omitempty"`     // 状态详情
}

// ObjectNode 对象存储节点
type ObjectNode struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Type         string `json:"type"` // bucket/prefix/object
	SizeBytes    int64  `json:"size_bytes"`
	FileType     string `json:"file_type,omitempty"`
	ObjectCount  int64  `json:"object_count"`
	LastModified string `json:"last_modified,omitempty"`
}

// ScanTaskUpsertRequest 创建或更新扫描任务的请求
type ScanTaskUpsertRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	EngineID    uint     `json:"engine_id" binding:"required"`
	Namespaces  []string `json:"namespaces"`
	ObjectPaths []string `json:"object_paths"`
	ScanDepth   string   `json:"scan_depth"`
	Schedule    string   `json:"schedule"` // Cron 表达式，空字符串表示手动执行
	Enabled     bool     `json:"enabled"`
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
	DataType     string `json:"data_type"`
	IsPrimaryKey bool   `json:"is_primary_key,omitempty"`
}
