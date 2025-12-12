package models

// Capability 能力声明
type Capability struct {
	Storage []StorageCapability `json:"storage,omitempty"` // 存储能力
	Compute []ComputeCapability `json:"compute,omitempty"` // 计算能力
}

// StorageCapability 存储能力
type StorageCapability struct {
	Type    string   `json:"type"`              // read, write
	Formats []string `json:"formats,omitempty"` // parquet, csv, json, etc.
}

// ComputeCapability 计算能力
type ComputeCapability struct {
	Type             string   `json:"type"`                         // scan, transfer.batch, cache.tile, transform, etc.
	SupportedSources []string `json:"supported_sources,omitempty"`  // postgresql, mysql, s3, minio, etc.
	Features         []string `json:"features,omitempty"`           // incremental, scheduled, parallel, async, retry, etc.
}

// TaskAPIConfig 任务 API 配置（用于动态调用）
type TaskAPIConfig struct {
	BaseURL   string                `json:"base_url"`             // 服务基础 URL（如 "http://meta-backend:8082"）
	Endpoints map[string]APIEndpoint `json:"endpoints"`            // API 端点配置
	Timeout   map[string]int         `json:"timeout,omitempty"`    // 超时配置（秒）
}

// APIEndpoint API 端点配置
type APIEndpoint struct {
	Method           string                 `json:"method"`                       // HTTP 方法（GET, POST, PUT, DELETE）
	Path             string                 `json:"path"`                         // 路径模板（支持 {{.TaskID}} 占位符）
	BodyTemplate     map[string]interface{} `json:"body_template,omitempty"`      // 请求体模板
	QueryParams      map[string]string      `json:"query_params,omitempty"`       // 查询参数模板
	ResponseMapping  *ResponseMapping       `json:"response_mapping,omitempty"`   // 响应字段映射
}

// ResponseMapping 响应字段映射（用于统一不同模块的响应格式）
type ResponseMapping struct {
	StatusField   string `json:"status_field"`              // 状态字段名（如 "status"）
	MessageField  string `json:"message_field,omitempty"`   // 消息字段名（如 "message"）
	ProgressField string `json:"progress_field,omitempty"`  // 进度字段名（如 "progress"）
	TaskIDField   string `json:"task_id_field,omitempty"`   // 任务 ID 字段名（如 "task_id"）
	DataField     string `json:"data_field,omitempty"`      // 数据字段名（如 "data"）
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Endpoint string `json:"endpoint"`          // 健康检查端点（如 "/health"）
	Timeout  int    `json:"timeout,omitempty"` // 超时时间（秒，默认 5）
	Interval int    `json:"interval,omitempty"` // 检查间隔（秒，默认 60）
}

// CapabilityRegistrationRequest 能力注册请求
type CapabilityRegistrationRequest struct {
	UniqueIdentifier  string             `json:"unique_identifier" binding:"required"` // 唯一标识符（如 "meta.scanner.default"）
	Name              string             `json:"name" binding:"required"`              // 显示名称（英文标识）
	DisplayName       string             `json:"display_name"`                         // 中文显示名称（可选，未提供时默认等于 name）
	ResourceType      string             `json:"resource_type" binding:"required"`     // database, compute_engine, object_storage
	IsBuiltin         bool               `json:"is_builtin"`                           // 是否为内置引擎
	Capabilities      *Capability        `json:"capabilities,omitempty"`               // 能力声明
	TaskAPIConfig     *TaskAPIConfig     `json:"task_api_config,omitempty"`            // 任务 API 配置（仅计算引擎）
	HealthCheckConfig *HealthCheckConfig `json:"health_check_config,omitempty"`        // 健康检查配置
	Description       string             `json:"description,omitempty"`                // 描述
	ConnectionInfo    map[string]interface{} `json:"connection_info,omitempty"`        // 连接信息（可选）
}
