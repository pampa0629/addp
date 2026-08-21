package models

import "github.com/addp/common/engine/plugin"

// CapabilityRegistrationRequest 能力注册请求。
// 注意：计算运行时通过 common/engine provider 调用，
// 注册请求只提交连接信息和 engine.capabilities/v1 能力声明，不提交端点配置。
// 内置引擎注册时以插件 Capabilities() 为事实源，服务端会忽略请求中的 Capabilities。
type CapabilityRegistrationRequest struct {
	Name           string                     `json:"name" binding:"required"`        // 显示名称（中文或英文）
	EngineType     string                     `json:"engine_type" binding:"required"` // 具体引擎类型，如 postgresql、acme_geo_workflow
	IsBuiltin      bool                       `json:"is_builtin"`                     // 是否为内置引擎
	Capabilities   *plugin.EngineCapabilities `json:"capabilities,omitempty"`         // 结构化能力声明
	Description    string                     `json:"description,omitempty"`          // 描述
	ConnectionInfo map[string]interface{}     `json:"connection_info,omitempty"`      // 连接信息（包含 protocol, host, port）
}
