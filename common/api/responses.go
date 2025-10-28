package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ErrorResponse sends a standardized error response
func ErrorResponse(c *gin.Context, code int, message string) {
	c.JSON(code, gin.H{"error": message})
}

// SuccessResponse sends a standardized success response
func SuccessResponse(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, data)
}

// CreatedResponse sends a 201 Created response
func CreatedResponse(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, data)
}

// NoContentResponse sends a 204 No Content response
func NoContentResponse(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// BadRequestError sends a 400 Bad Request error
func BadRequestError(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusBadRequest, message)
}

// UnauthorizedError sends a 401 Unauthorized error
func UnauthorizedError(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusUnauthorized, message)
}

// ForbiddenError sends a 403 Forbidden error
func ForbiddenError(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusForbidden, message)
}

// NotFoundError sends a 404 Not Found error
func NotFoundError(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusNotFound, message)
}

// InternalServerError sends a 500 Internal Server Error
func InternalServerError(c *gin.Context, message string) {
	ErrorResponse(c, http.StatusInternalServerError, message)
}

// ParseUintParam parses a uint parameter from URL path
// Returns the value and true if parsing succeeds, or sends error response and returns 0, false
func ParseUintParam(c *gin.Context, param string) (uint, bool) {
	valueStr := c.Param(param)
	value, err := strconv.ParseUint(valueStr, 10, 32)
	if err != nil {
		BadRequestError(c, "Invalid "+param)
		return 0, false
	}
	return uint(value), true
}

// ParseIntParam parses an int parameter from URL path
// Returns the value and true if parsing succeeds, or sends error response and returns 0, false
func ParseIntParam(c *gin.Context, param string) (int, bool) {
	valueStr := c.Param(param)
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		BadRequestError(c, "Invalid "+param)
		return 0, false
	}
	return value, true
}

// ParseIntQuery parses an int query parameter with a default value
func ParseIntQuery(c *gin.Context, key string, defaultVal int) int {
	valueStr := c.DefaultQuery(key, "")
	if valueStr == "" {
		return defaultVal
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultVal
	}
	return value
}

// GetPaginationParams extracts pagination parameters from query string
// Returns page and pageSize with defaults (page=1, pageSize=10)
func GetPaginationParams(c *gin.Context) (page int, pageSize int) {
	page = ParseIntQuery(c, "page", 1)
	pageSize = ParseIntQuery(c, "page_size", 10)

	// Ensure minimum values
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100 // Max limit
	}

	return page, pageSize
}

// BindJSON is a helper to bind JSON request body
// Returns true if binding succeeds, or sends error response and returns false
func BindJSON(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		BadRequestError(c, "Invalid request: "+err.Error())
		return false
	}
	return true
}

// BindQuery is a helper to bind query parameters
// Returns true if binding succeeds, or sends error response and returns false
func BindQuery(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBindQuery(obj); err != nil {
		BadRequestError(c, "Invalid query parameters: "+err.Error())
		return false
	}
	return true
}

// PaginatedResponse represents a paginated API response
type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// SendPaginatedResponse sends a paginated response
func SendPaginatedResponse(c *gin.Context, data interface{}, total int64, page int, pageSize int) {
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Data:       data,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}
