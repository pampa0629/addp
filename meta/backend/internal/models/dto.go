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
