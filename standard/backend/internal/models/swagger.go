package models

import "github.com/addp/common/dataquality"

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

// QualityRulesResponse 是 Standard 与 Quality 共享的版本化规则文档。
type QualityRulesResponse dataquality.Document
