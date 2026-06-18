package models

import (
	"time"

	"github.com/addp/common/datatype"
)

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
	ScannedDepth   string                 `json:"scanned_depth"`
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
	DataUpdatedAt   *time.Time             `json:"data_updated_at,omitempty"`
	ScannedAt       *time.Time             `json:"scanned_at,omitempty"`
	ScannedDepth    string                 `json:"scanned_depth"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
}

// MetadataTree 元数据树（用于 GetMetadataTree 响应）
type MetadataTree struct {
	TopNodes   []MetaNode `json:"top_nodes"`
	ChildNodes []MetaNode `json:"child_nodes"`
	Items      []MetaItem `json:"items"`
}

// MetaItemAncestors 表示数据项及其父节点祖先链。
type MetaItemAncestors struct {
	Item      MetaItem   `json:"item"`
	Ancestors []MetaNode `json:"ancestors"`
}

// SpatialMetadata 空间元数据（用于 MVT 瓦片生成）
type SpatialMetadata struct {
	GeometryColumn string                  `json:"geometry_column"`
	GeometryTypes  []string                `json:"geometry_types,omitempty"` // 几何类型列表，如 ["ST_MultiPolygon"]
	SRID           int                     `json:"srid"`
	CRSRef         string                  `json:"crs_ref,omitempty"`
	CRSDefinition  *datatype.CRSDefinition `json:"crs_definition,omitempty"`
	ExtentSRID     int                     `json:"extent_srid"`
	Extent         []float64               `json:"extent"`
	PrimaryKey     string                  `json:"primary_key"`
	Fields         []datatype.FieldInfo    `json:"fields"`
	RowCount       int64                   `json:"row_count"` // 表记录数（从 Meta 服务获取）
}
