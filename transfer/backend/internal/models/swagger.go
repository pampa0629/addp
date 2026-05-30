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

// CreateTaskRequestDoc 是 CreateTaskRequest 的 Swagger 展示结构。
type CreateTaskRequestDoc struct {
	Name             string                     `json:"name" example:"导入道路 Shapefile"`
	Description      string                     `json:"description,omitempty"`
	TaskType         string                     `json:"task_type,omitempty" example:"import"`
	Config           TableTransferTaskConfigDoc `json:"config"`
	Schedule         string                     `json:"schedule,omitempty"`
	BatchSize        int                        `json:"batch_size,omitempty" example:"1000"`
	AutoScanMetadata bool                       `json:"auto_scan_metadata,omitempty" example:"true"`
	Mappings         []FieldMapping             `json:"mappings,omitempty"`
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
	Mode       string                 `json:"mode" example:"batch"`
	Source     TransferEndpointDoc    `json:"source"`
	Target     TransferEndpointDoc    `json:"target"`
	Transforms []TransferTransformDoc `json:"transforms,omitempty"`
	BatchSize  int                    `json:"batch_size,omitempty" example:"1000"`
}

// TransferEndpointDoc 描述 Transfer source / target endpoint。
type TransferEndpointDoc struct {
	Engine         TransferEngineRefDoc        `json:"engine"`
	Resource       TransferEndpointResourceDoc `json:"resource"`
	DataType       string                      `json:"data_type" example:"table"`
	Representation string                      `json:"representation" example:"encoded" enums:"native,encoded"`
	MetaItemID     uint                        `json:"meta_item_id,omitempty" example:"12" description:"Meta item ID；source 指向已入库 Meta item 时由 Transfer 后端通过 MetaClient 读取标准 attributes。"`
	Format         string                      `json:"format,omitempty" example:"shapefile"`
	Options        map[string]interface{}      `json:"options,omitempty"`
	Policy         map[string]interface{}      `json:"policy,omitempty"`
}

type TransferEngineRefDoc struct {
	Scope string `json:"scope" example:"system"`
	ID    uint   `json:"id" example:"1"`
	Type  string `json:"type,omitempty" example:"nfs"`
}

type TransferEndpointResourceDoc struct {
	Kind string      `json:"kind" example:"file" enums:"native_table,file,object"`
	Path interface{} `json:"path" swaggertype:"object"`
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
	Nullable   bool        `json:"nullable,omitempty" example:"true"`
	Default    interface{} `json:"default,omitempty"`
	Format     string      `json:"format,omitempty"`
}
