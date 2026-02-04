package models

import "time"

// MetaNode 节点模型（Schema/Bucket/Prefix）
type MetaNode struct {
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
	LastScanAt     *time.Time             `json:"scanned_at,omitempty"`
	ItemCount      int                    `json:"item_count"`
	TotalSizeBytes int64                  `json:"total_size_bytes"`
	Attributes     map[string]interface{} `json:"attributes,omitempty"`
}

// MetaItem 项目模型（表/对象）
type MetaItem struct {
	ID              uint                   `json:"id"`
	TenantID        uint                   `json:"tenant_id"`
	EngineID        uint                   `json:"engine_id"`
	NodeID          uint                   `json:"node_id"`
	ItemType        string                 `json:"item_type"`
	Name            string                 `json:"name"`
	FullName        string                 `json:"full_name"`
	RowCount        *int64                 `json:"row_count,omitempty"`
	SizeBytes       *int64                 `json:"size_bytes,omitempty"`
	ObjectSizeBytes *int64                 `json:"object_size_bytes,omitempty"`
	LastModifiedAt  *time.Time             `json:"last_modified_at,omitempty"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
}

// MetadataTree 元数据树（用于 GetMetadataTree 响应）
type MetadataTree struct {
	TopNodes   []MetaNode `json:"top_nodes"`
	ChildNodes []MetaNode `json:"child_nodes"`
	Items      []MetaItem `json:"items"`
}

// SpatialMetadata 空间元数据（用于 MVT 瓦片生成）
type SpatialMetadata struct {
	GeometryColumn string    `json:"geometry_column"`
	GeometryTypes  []string  `json:"geometry_types,omitempty"` // 几何类型列表，如 ["ST_MultiPolygon"]
	SRID           int       `json:"srid"`
	ExtentSRID     int       `json:"extent_srid"`
	Extent         []float64 `json:"extent"`
	PrimaryKey     string    `json:"primary_key"`
	Fields         []Field   `json:"fields"`
	RowCount       int64     `json:"row_count"` // 表记录数（从 Meta 服务获取）
}

// Field 字段信息
type Field struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsPrimaryKey bool   `json:"is_primary_key,omitempty"`
}

// SchemaWithStatus Schema 元数据（带扫描状态）
type SchemaWithStatus struct {
	ID             uint   `json:"id"`
	SchemaName     string `json:"schema_name"`
	ScanStatus     string `json:"scan_status"`
	ScannedAt      string `json:"scanned_at,omitempty"`
	TableCount     int    `json:"table_count"`
	TotalSizeBytes int64  `json:"total_size_bytes"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// TableInfo 表元数据信息
type TableInfo struct {
	Name      string `json:"name"`
	FullName  string `json:"full_name,omitempty"`
	Schema    string `json:"schema,omitempty"`
	RowCount  int64  `json:"row_count"`
	SizeBytes int64  `json:"size_bytes"`
	Comment   string `json:"comment,omitempty"`
}

// FieldInfo 字段详细信息
type FieldInfo struct {
	Name           string `json:"name"`
	DataType       string `json:"data_type"`
	IsPrimaryKey   bool   `json:"is_primary_key,omitempty"`
	IsNullable     bool   `json:"is_nullable,omitempty"`
	DefaultValue   string `json:"default_value,omitempty"`
	Comment        string `json:"comment,omitempty"`
	IsSpatial      bool   `json:"is_spatial,omitempty"`
	GeometryType   string `json:"geometry_type,omitempty"`
	SRID           int    `json:"srid,omitempty"`
}
