package errors

import (
	"errors"
	"net/http"
)

// 业务错误定义
var (
	// 引擎相关错误
	ErrEngineNotFound     = errors.New("engine not found")
	ErrEngineAccessDenied = errors.New("engine access denied: tenant mismatch")

	// 租户相关错误
	ErrInvalidTenantID = errors.New("invalid tenant ID")
	ErrTenantMismatch  = errors.New("tenant mismatch")

	// 节点相关错误
	ErrNodeNotFound     = errors.New("node not found")
	ErrNodeAccessDenied = errors.New("node access denied: tenant mismatch")

	// 条目相关错误
	ErrItemNotFound     = errors.New("item not found")
	ErrItemAccessDenied = errors.New("item access denied: tenant mismatch")

	// 资源定位相关错误
	ErrInvalidResourceLocator = errors.New("invalid resource locator")
	ErrInvalidChangeCursor    = errors.New("invalid data item change cursor")
	ErrInvalidChangeRequest   = errors.New("invalid data item change request")

	// 扫描相关错误
	ErrScanNotFound     = errors.New("scan task not found")
	ErrScanAccessDenied = errors.New("scan access denied: tenant mismatch")
)

// HTTPStatusCode 根据错误返回对应的 HTTP 状态码
func HTTPStatusCode(err error) int {
	switch {
	case errors.Is(err, ErrEngineNotFound),
		errors.Is(err, ErrNodeNotFound),
		errors.Is(err, ErrItemNotFound),
		errors.Is(err, ErrScanNotFound):
		return http.StatusNotFound

	case errors.Is(err, ErrEngineAccessDenied),
		errors.Is(err, ErrNodeAccessDenied),
		errors.Is(err, ErrItemAccessDenied),
		errors.Is(err, ErrScanAccessDenied),
		errors.Is(err, ErrTenantMismatch):
		return http.StatusForbidden

	case errors.Is(err, ErrInvalidTenantID),
		errors.Is(err, ErrInvalidChangeCursor),
		errors.Is(err, ErrInvalidChangeRequest),
		errors.Is(err, ErrInvalidResourceLocator):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}

// ErrorMessage 返回用户友好的错误消息
func ErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrEngineAccessDenied),
		errors.Is(err, ErrNodeAccessDenied),
		errors.Is(err, ErrItemAccessDenied),
		errors.Is(err, ErrScanAccessDenied):
		return "access denied to this resource"

	case errors.Is(err, ErrEngineNotFound):
		return "engine not found"

	case errors.Is(err, ErrNodeNotFound):
		return "node not found"

	case errors.Is(err, ErrItemNotFound):
		return "item not found"

	case errors.Is(err, ErrScanNotFound):
		return "scan task not found"

	case errors.Is(err, ErrInvalidTenantID):
		return "invalid tenant ID"

	case errors.Is(err, ErrInvalidResourceLocator):
		return err.Error()

	case errors.Is(err, ErrInvalidChangeCursor):
		return err.Error()

	case errors.Is(err, ErrInvalidChangeRequest):
		return err.Error()

	default:
		return err.Error()
	}
}
