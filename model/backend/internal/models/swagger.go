package models

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error     string `json:"error" example:"请求参数错误"`
	ErrorCode string `json:"error_code" example:"model_operation_failed"`
}

type MessageResponse struct {
	Message string `json:"message" example:"操作成功"`
}

type EntityListResponse struct {
	Data       []Entity `json:"data"`
	Total      int64    `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalPages int      `json:"total_pages"`
}

type LogicalTableListResponse struct {
	Data       []LogicalTable `json:"data"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

type DDLPreviewResponse struct {
	DDL string `json:"ddl"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Code    int         `json:"code" example:"200"`
	Message string      `json:"message" example:"操作成功"`
	Data    interface{} `json:"data,omitempty"`
}
