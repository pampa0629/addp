package models

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error string `json:"error" example:"invalid credentials"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Message string `json:"message" example:"操作成功"`
}
