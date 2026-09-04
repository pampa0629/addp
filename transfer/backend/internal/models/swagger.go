package models

// ErrorResponse 错误响应
type ErrorResponse struct {
	Code    int    `json:"code" example:"400"`
	Message string `json:"message" example:"请求参数错误"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Code    int         `json:"code" example:"200"`
	Message string      `json:"message" example:"操作成功"`
	Data    interface{} `json:"data,omitempty"`
}

// SystemEngineDoc 是 Transfer Swagger 中展示的 System engine 摘要。
type SystemEngineDoc struct {
	ID               uint                   `json:"id" example:"1"`
	TenantID         uint                   `json:"tenant_id,omitempty" example:"1"`
	Name             string                 `json:"name" example:"Business MinIO"`
	EngineType       string                 `json:"engine_type" example:"minio"`
	EngineOrigin     string                 `json:"engine_origin,omitempty" example:"general"`
	ConnectionInfo   map[string]interface{} `json:"connection_info,omitempty"`
	Description      string                 `json:"description,omitempty"`
	IsActive         bool                   `json:"is_active" example:"true"`
	IsBuiltin        bool                   `json:"is_builtin,omitempty" example:"false"`
	ConnectionStatus string                 `json:"connection_status,omitempty" example:"online"`
	CheckMessage     string                 `json:"check_message,omitempty"`
}

// CreateTaskRequestDoc 是 CreateTaskRequest 的 Swagger 展示结构。
type CreateTaskRequestDoc struct {
	Name             string                     `json:"name" example:"导入道路 Shapefile"`
	Description      string                     `json:"description,omitempty"`
	TaskType         string                     `json:"task_type,omitempty" example:"sync"`
	Config           TableTransferTaskConfigDoc `json:"config"`
	Schedule         string                     `json:"schedule,omitempty"`
	Enabled          bool                       `json:"enabled,omitempty"`
	BatchSize        int                        `json:"batch_size,omitempty" example:"1000"`
	AutoScanMetadata bool                       `json:"auto_scan_metadata,omitempty" example:"true"`
}

// UpdateTaskRequestDoc 是 UpdateTaskRequest 的 Swagger 展示结构。
type UpdateTaskRequestDoc struct {
	Name             string                     `json:"name,omitempty"`
	Description      string                     `json:"description,omitempty"`
	TaskType         string                     `json:"task_type,omitempty"`
	Config           TableTransferTaskConfigDoc `json:"config,omitempty"`
	Schedule         string                     `json:"schedule,omitempty"`
	BatchSize        int                        `json:"batch_size,omitempty"`
	Enabled          bool                       `json:"enabled,omitempty"`
	AutoScanMetadata bool                       `json:"auto_scan_metadata,omitempty"`
}

// TableTransferTaskConfigDoc 描述新 Transfer table 任务配置。
// 运行时代码仍使用 JSONMap 存储；该结构只用于 Swagger 展示 source / target endpoint 语义。
type TableTransferTaskConfigDoc struct {
	Runtime    TransferRuntimeDoc        `json:"runtime"`
	Load       TransferLoadDoc           `json:"load"`
	Source     TransferSourceEndpointDoc `json:"source"`
	Target     TransferTargetEndpointDoc `json:"target"`
	Transforms []TransferTransformDoc    `json:"transforms,omitempty"`
	BatchSize  int                       `json:"batch_size,omitempty" example:"1000"`
}

type TransferRuntimeDoc struct {
	Boundary      string                    `json:"boundary" example:"bounded" enums:"bounded,continuous"`
	RecordFailure *TransferRecordFailureDoc `json:"record_failure,omitempty"`
}

type TransferRecordFailureDoc struct {
	Mode string `json:"mode" example:"block" enums:"block,dead_letter"`
}

type TransferLoadDoc struct {
	Mode            string                      `json:"mode" example:"snapshot" enums:"snapshot,incremental"`
	ChangeDetection *TransferChangeDetectionDoc `json:"change_detection,omitempty"`
}

type TransferChangeDetectionDoc struct {
	Type       string   `json:"type" example:"watermark" enums:"watermark,kafka,cdc"`
	Bootstrap  string   `json:"bootstrap,omitempty" example:"initial_snapshot" enums:"initial_snapshot"`
	Field      string   `json:"field" example:"updated_at"`
	TieBreaker []string `json:"tie_breaker" example:"id"`
	Start      string   `json:"start" example:"committed" enums:"committed"`
	End        string   `json:"end" example:"execution_upper_bound" enums:"execution_upper_bound"`
}

// TransferSourceEndpointDoc 描述 Transfer source endpoint；source 必须指向已存在资源。
type TransferSourceEndpointDoc struct {
	Locator        string                   `json:"locator" example:"addp://engine/9/path/manager/a3.shp?type=object" description:"ResourceLocator URI；source 可通过 locator item_id 引用已入库 Meta item。"`
	DataType       string                   `json:"data_type" example:"table"`
	Representation string                   `json:"representation" example:"encoded" enums:"native,encoded"`
	Format         string                   `json:"format,omitempty" example:"shapefile"`
	Options        map[string]interface{}   `json:"options,omitempty"`
	Policy         map[string]interface{}   `json:"policy,omitempty"`
	Query          *TransferQuerySourceDoc  `json:"query,omitempty"`
	ChangeStream   *TransferChangeStreamDoc `json:"change_stream,omitempty"`
}

type TransferQuerySourceDoc struct {
	Language   string                  `json:"language" example:"mql"`
	Statement  string                  `json:"statement" example:"{\"aggregate\":\"orders\",\"pipeline\":[{\"$project\":{\"customer_id\":\"$customer.id\",\"_id\":0}}]}"`
	Parameters map[string]interface{}  `json:"parameters,omitempty"`
	Inputs     []TransferQueryInputDoc `json:"inputs,omitempty"`
}

// TransferQueryInputDoc 是调用方已经解析完成的查询关系输入。
type TransferQueryInputDoc struct {
	Name    string `json:"name" example:"orders"`
	Locator string `json:"locator" example:"addp://engine/9/path/public/orders?type=table&item_id=42"`
}

type TransferChangeStreamDoc struct {
	Envelope      string                       `json:"envelope" example:"record" enums:"record"`
	Encoding      string                       `json:"encoding" example:"json" enums:"json"`
	Key           TransferChangeStreamKeyDoc   `json:"key"`
	Start         TransferChangeStreamStartDoc `json:"start"`
	PollBatchSize int                          `json:"poll_batch_size" example:"1000"`
}

type TransferChangeStreamKeyDoc struct {
	Source string   `json:"source" example:"value" enums:"value"`
	Fields []string `json:"fields" example:"id"`
}

type TransferChangeStreamStartDoc struct {
	Mode    string `json:"mode" example:"committed" enums:"committed"`
	Initial string `json:"initial" example:"earliest" enums:"earliest,latest"`
}

// TransferTargetEndpointDoc 描述 Transfer target endpoint；target 表达待写入资源，使用父 node locator 和目标名。
type TransferTargetEndpointDoc struct {
	ParentLocator  string                  `json:"parent_locator" example:"addp://engine/10/path/public?type=schema" description:"目标父 node 的 ResourceLocator URI。"`
	Name           string                  `json:"name" example:"roads_imported" description:"父 node 下待创建或待覆盖的目标资源名。"`
	DataType       string                  `json:"data_type" example:"table"`
	Representation string                  `json:"representation" example:"native" enums:"native,encoded"`
	Format         string                  `json:"format,omitempty" example:"csv"`
	Options        map[string]interface{}  `json:"options,omitempty"`
	Policy         TransferTargetPolicyDoc `json:"policy"`
}

type TransferTargetPolicyDoc struct {
	ApplyMode string   `json:"apply_mode" example:"replace" enums:"replace,append,upsert,upsert_delete"`
	Keys      []string `json:"keys,omitempty" example:"id"`
}

type TransferTransformDoc struct {
	Type    string                 `json:"type" example:"field_mapping"`
	Version string                 `json:"version,omitempty" example:"v1"`
	Mode    string                 `json:"mode,omitempty" example:"project"`
	Fields  []FieldMappingSpecDoc  `json:"fields,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

type FieldMappingSpecDoc struct {
	Source     string      `json:"source,omitempty" example:"geom"`
	Target     string      `json:"target" example:"geometry"`
	TargetType string      `json:"target_type,omitempty" example:"geometry"`
	Precision  *int        `json:"precision,omitempty" example:"18" description:"十进制目标字段的总有效位数，必须与 scale 同时提供。"`
	Scale      *int        `json:"scale,omitempty" example:"4" description:"十进制目标字段的小数位数，必须与 precision 同时提供。"`
	Nullable   bool        `json:"nullable,omitempty" example:"true"`
	Default    interface{} `json:"default,omitempty"`
	Format     string      `json:"format,omitempty"`
}
