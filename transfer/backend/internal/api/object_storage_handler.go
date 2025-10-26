package api

import (
	"errors"
	"net/http"

	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ObjectStorageHandler 提供对象存储浏览能力
type ObjectStorageHandler struct {
	service *service.ObjectStorageService
}

// NewObjectStorageHandler 构造函数
func NewObjectStorageHandler(service *service.ObjectStorageService) *ObjectStorageHandler {
	return &ObjectStorageHandler{service: service}
}

// objectStorageBrowseRequest 请求体
type objectStorageBrowseRequest struct {
	Scope      string `json:"scope" binding:"required,oneof=system local"`
	ResourceID uint   `json:"resource_id" binding:"required"`
	Prefix     string `json:"prefix"`
}

// BrowseDirectories 浏览对象存储目录
func (h *ObjectStorageHandler) BrowseDirectories(c *gin.Context) {
	var req objectStorageBrowseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")

	result, err := h.service.ListDirectories(c.Request.Context(), tenantID, req.Scope, req.ResourceID, req.Prefix)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		case errors.Is(err, service.ErrSystemIntegrationDisabled):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system integration not available"})
		case errors.Is(err, service.ErrResourceAccessDenied):
			c.JSON(http.StatusForbidden, gin.H{"error": "resource not accessible"})
		case errors.Is(err, service.ErrObjectStorageNotSupported):
			c.JSON(http.StatusBadRequest, gin.H{"error": "resource is not object storage"})
		case errors.Is(err, service.ErrObjectStorageBucketRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "bucket is required"})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}
