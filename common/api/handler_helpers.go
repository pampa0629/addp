package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// BindIDParam 解析并绑定 ID 参数
// 消除 10+ 处重复的 strconv.ParseUint + 错误处理
func BindIDParam(c *gin.Context, paramName string) (uint, error) {
	id, err := strconv.ParseUint(c.Param(paramName), 10, 32)
	if err != nil {
		RespondError(c, http.StatusBadRequest, "无效的ID参数")
		return 0, err
	}
	return uint(id), nil
}

// GetCurrentUserID 获取当前用户 ID
// 消除 15+ 处重复的 c.Get("user_id") + 类型断言
func GetCurrentUserID(c *gin.Context) (uint, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	return userID.(uint), true
}

// MustGetCurrentUserID 获取当前用户 ID，如果不存在则返回 401
func MustGetCurrentUserID(c *gin.Context) (uint, error) {
	userID, exists := GetCurrentUserID(c)
	if !exists {
		RespondError(c, http.StatusUnauthorized, "未授权")
		return 0, ErrUnauthorized
	}
	return userID, nil
}

// ParsePagination 解析分页参数
// 消除 5+ 处重复的分页参数解析
func ParsePagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "10"))

	// 限制 pageSize 最大值
	if pageSize > 100 {
		pageSize = 100
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if page < 1 {
		page = 1
	}

	return
}

// RespondSuccess 返回成功响应
func RespondSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, data)
}

// RespondCreated 返回创建成功响应
func RespondCreated(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, data)
}

// RespondError 返回错误响应
func RespondError(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{"error": message})
}

// RespondOrError 统一处理响应或错误
// 如果 err != nil，自动映射错误到 HTTP 状态码
// 如果 err == nil，返回成功响应
func RespondOrError(c *gin.Context, data interface{}, err error) {
	if err != nil {
		statusCode := MapErrorToHTTPStatus(err)
		RespondError(c, statusCode, err.Error())
		return
	}
	RespondSuccess(c, data)
}
