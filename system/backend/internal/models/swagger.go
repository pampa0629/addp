package models

import "encoding/json"

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error     string `json:"error" example:"invalid credentials"`
	ErrorCode string `json:"error_code,omitempty" example:"invalid_request"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Message string `json:"message" example:"操作成功"`
}

// EngineConnectionProbe 引擎连接测试的协议探测结果
type EngineConnectionProbe struct {
	RuntimeProtocol string `json:"runtime_protocol,omitempty" example:"addp.workflow/v1"`
	OperatorsCount  int    `json:"operators_count,omitempty" example:"8"`
}

// EngineConnectionTestResponse 引擎连接测试响应
type EngineConnectionTestResponse struct {
	Success bool                   `json:"success" example:"true"`
	Message string                 `json:"message" example:"连接成功"`
	Error   string                 `json:"error,omitempty" example:"workflow runtime health check failed"`
	Probe   *EngineConnectionProbe `json:"probe,omitempty"`
}

// EngineResponse 引擎响应
type EngineResponse struct {
	Engine
	Capabilities     map[string]interface{} `json:"capabilities,omitempty"`
	CapabilitiesView *CapabilitiesView      `json:"capabilities_view,omitempty"`
}

// EngineCatalogSelector is the minimum engine projection needed by tenant business selectors.
type EngineCatalogSelector struct {
	ID               uint            `json:"id"`
	Name             string          `json:"name"`
	EngineType       string          `json:"engine_type"`
	EngineOrigin     string          `json:"engine_origin"`
	LifecycleState   string          `json:"lifecycle_state"`
	ConnectionStatus string          `json:"connection_status"`
	Capabilities     json.RawMessage `json:"capabilities,omitempty" swaggertype:"object"`
}
