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
	SupportedSources []string `json:"supported_sources,omitempty"` // postgresql, mysql, s3, minio, etc.
	Features         []string `json:"features,omitempty"`          // incremental, scheduled, parallel, async, retry, etc.
	DevModes         []string `json:"dev_modes,omitempty"`         // 开发方式 (query, workflow, notebook) - query 支持 SQL/MQL 等查询语言
	Description      string   `json:"description,omitempty"`       // 能力描述
}

// CapabilityRegistrationRequest 能力注册请求
// 注意：工作流引擎的 API 端点和健康检查配置已标准化，无需在注册时提供
// 标准配置定义在 workflow_standards.go 中
type CapabilityRegistrationRequest struct {
	Name           string                 `json:"name" binding:"required"`       // 显示名称（中文或英文）
	EngineType     string                 `json:"engine_type" binding:"required"` // database, compute_engine, object_storage
	IsBuiltin      bool                   `json:"is_builtin"`                    // 是否为内置引擎
	Capabilities   *Capability            `json:"capabilities,omitempty"`        // 能力声明
	Description    string                 `json:"description,omitempty"`         // 描述
	ConnectionInfo map[string]interface{} `json:"connection_info,omitempty"`     // 连接信息（包含 protocol, host, port）
}
