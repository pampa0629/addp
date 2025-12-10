package models

// ScanRequest 扫描请求
type ScanRequest struct {
	ResourceID  uint     `json:"resource_id" binding:"required"` // 资源ID
	SchemaNames []string `json:"schema_names"`                   // 要扫描的Schema列表（空则全部）
	ObjectPaths []string `json:"object_paths"`                   // 对象存储选择的路径
	ScanDepth   string   `json:"scan_depth"`                     // basic/deep/full
	ScanType    string   `json:"scan_type"`                      // manual/auto/scheduled
}

// ScanResponse 扫描响应
type ScanResponse struct {
	Status         string `json:"status"` // success/failed
	Message        string `json:"message"`
	SchemasScanned int    `json:"schemas_scanned"`
	TablesScanned  int    `json:"tables_scanned"`
	FieldsScanned  int    `json:"fields_scanned"`
	DurationMs     int64  `json:"duration_ms"`
	StartedAt      string `json:"started_at"`
}

// ResourceWithStats 资源及其扫描统计
type ResourceWithStats struct {
	ResourceID       uint   `json:"id"`   // 前端期待 id
	ResourceName     string `json:"name"` // 前端期待 name
	ResourceType     string `json:"resource_type"`
	TotalSchemas     int    `json:"total_schemas"`
	ScannedSchemas   int    `json:"scanned_schemas"`
	UnscannedSchemas int    `json:"unscanned_schemas"`
	LastScanAt       string `json:"last_scan_at,omitempty"`
}

// SchemaWithStatus Schema及其状态
type SchemaWithStatus struct {
	ID              uint   `json:"id"`
	SchemaName      string `json:"schema_name"`
	ScanStatus      string `json:"scan_status"`
	LastScanAt      string `json:"last_scan_at,omitempty"`
	TableCount      int    `json:"table_count"`
	TotalSizeBytes  int64  `json:"total_size_bytes"`
	AutoScanEnabled bool   `json:"auto_scan_enabled"`
	AutoScanCron    string `json:"auto_scan_cron,omitempty"`
	NextScanAt      string `json:"next_scan_at,omitempty"`
}

// ScheduleRequest 定时扫描配置请求
type ScheduleRequest struct {
	SchemaID        uint   `json:"schema_id" binding:"required"`
	AutoScanEnabled bool   `json:"auto_scan_enabled"`
	AutoScanCron    string `json:"auto_scan_cron"` // 如 "0 0 * * *"
}

// SchemaInfo Schema信息（用于获取Schema列表）
type SchemaInfo struct {
	Name   string   `json:"name"`
	Tables []string `json:"tables,omitempty"`
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
	Name           string   `json:"name" binding:"required"`
	Description    string   `json:"description"`
	ResourceID     uint     `json:"resource_id" binding:"required"`
	SchemaNames    []string `json:"schema_names"`
	ObjectPaths    []string `json:"object_paths"`
	ScanDepth      string   `json:"scan_depth"`
	ScheduleType   string   `json:"schedule_type" binding:"required"` // manual/daily/weekly/monthly/cron
	ScheduleTime   string   `json:"schedule_time"`                    // HH:MM
	ScheduleValue  []int    `json:"schedule_value"`                   // weekly: weekday(0-6), monthly: day(1-31)
	CronExpression string   `json:"cron_expression"`                  // schedule_type=cron 时必填
	Enabled        bool     `json:"enabled"`
}

// ScanTaskRunView 任务运行记录（包含任务与资源额外信息）
type ScanTaskRunView struct {
	ScanTaskRun
	TaskName     string `json:"task_name"`
	TaskPlanName string `json:"task_plan_name,omitempty"`
	ResourceName string `json:"resource_name,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
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
	ResID          uint                   `json:"res_id"`
	ParentNodeID   *uint                  `json:"parent_node_id,omitempty"`
	NodeType       string                 `json:"node_type"`
	Name           string                 `json:"name"`
	FullName       string                 `json:"full_name"`
	Depth          int                    `json:"depth"`
	Path           string                 `json:"path"`
	ScanStatus     string                 `json:"scan_status"`
	LastScanAt     *string                `json:"last_scan_at,omitempty"`
	ItemCount      int                    `json:"item_count"`
	TotalSizeBytes int64                  `json:"total_size_bytes"`
	Attributes     map[string]interface{} `json:"attributes,omitempty"`
}

// MetaItemLite 元数据项简化模型（用于返回给 Manager）
type MetaItemLite struct {
	ID              uint                   `json:"id"`
	TenantID        uint                   `json:"tenant_id"`
	ResID           uint                   `json:"res_id"`
	NodeID          uint                   `json:"node_id"`
	ItemType        string                 `json:"item_type"`
	Name            string                 `json:"name"`
	FullName        string                 `json:"full_name"`
	RowCount        *int64                 `json:"row_count,omitempty"`
	SizeBytes       *int64                 `json:"size_bytes,omitempty"`
	ObjectSizeBytes *int64                 `json:"object_size_bytes,omitempty"`
	LastModifiedAt  *string                `json:"last_modified_at,omitempty"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
}

// SpatialMetadataResponse 空间元数据响应（用于 Manager MVT 瓦片生成）
type SpatialMetadataResponse struct {
	GeometryColumn string                   `json:"geometry_column"`
	SRID           int                      `json:"srid"`
	ExtentSRID     int                      `json:"extent_srid"`
	Extent         []float64                `json:"extent"` // [minLng, minLat, maxLng, maxLat]
	PrimaryKey     string                   `json:"primary_key"`
	Fields         []FieldInfo              `json:"fields"`
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsPrimaryKey bool   `json:"is_primary_key,omitempty"`
}
