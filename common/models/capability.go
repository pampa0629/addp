package models

import "github.com/addp/common/engine/plugin"

// CapabilityRegistrationRequest 能力注册请求
// 注意：工作流引擎的 API 端点和健康检查配置已标准化，无需在注册时提供
// 标准配置定义在 workflow_standards.go 中
type CapabilityRegistrationRequest struct {
	Name           string                     `json:"name" binding:"required"`        // 显示名称（中文或英文）
	EngineType     string                     `json:"engine_type" binding:"required"` // database, compute_engine, object_storage
	IsBuiltin      bool                       `json:"is_builtin"`                     // 是否为内置引擎
	Capabilities   *plugin.EngineCapabilities `json:"capabilities,omitempty"`         // 结构化能力声明
	Description    string                     `json:"description,omitempty"`          // 描述
	ConnectionInfo map[string]interface{}     `json:"connection_info,omitempty"`      // 连接信息（包含 protocol, host, port）
}
