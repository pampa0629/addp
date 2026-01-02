package api

import (
	"errors"
	"net/http"

	"gorm.io/gorm"
)

// 通用错误定义
var (
	ErrBadRequest          = errors.New("无效的请求参数")
	ErrUnauthorized        = errors.New("未授权")
	ErrForbidden           = errors.New("没有权限访问该资源")
	ErrNotFound            = errors.New("资源不存在")
	ErrConflict            = errors.New("资源已存在")
	ErrInternalServerError = errors.New("服务器内部错误")
)

// MapErrorToHTTPStatus 映射错误到 HTTP 状态码
func MapErrorToHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	// GORM 错误
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return http.StatusConflict
	}

	// 通用错误
	switch {
	case errors.Is(err, ErrBadRequest):
		return http.StatusBadRequest
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// WrapError 包装错误，添加上下文信息
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return errors.New(message + ": " + err.Error())
}
