package models

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error string `json:"error" example:"invalid credentials"`
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
