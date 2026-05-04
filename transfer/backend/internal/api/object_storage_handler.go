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
	Scope    string `json:"scope" binding:"required,oneof=system local"`
	EngineID uint   `json:"engine_id" binding:"required"`
	Prefix   string `json:"prefix"`
}

// BrowseDirectories 浏览对象存储目录
// @Summary 浏览对象存储目录 | Browse object storage directories
// @Tags 对象存储 | Object Storage
// @Accept json
// @Produce json
// @Param request body objectStorageBrowseRequest true "浏览请求 | Browse request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /object-storage/browse [post]
// @Security BearerAuth
func (h *ObjectStorageHandler) BrowseDirectories(c *gin.Context) {
	var req objectStorageBrowseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")

	result, err := h.service.ListDirectories(c.Request.Context(), tenantID, req.Scope, req.EngineID, req.Prefix)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		case errors.Is(err, service.ErrSystemIntegrationDisabled):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system integration not available"})
		case errors.Is(err, service.ErrEngineAccessDenied):
			c.JSON(http.StatusForbidden, gin.H{"error": "resource not accessible"})
		case errors.Is(err, service.ErrObjectStorageNotSupported):
			c.JSON(http.StatusBadRequest, gin.H{"error": "resource is not object storage"})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// ListFiles 列出对象存储文件
// @Summary 列出对象存储文件 | List object storage files
// @Tags 对象存储 | Object Storage
// @Accept json
// @Produce json
// @Param request body objectStorageBrowseRequest true "文件列表请求 | List files request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /object-storage/list-files [post]
// @Security BearerAuth
func (h *ObjectStorageHandler) ListFiles(c *gin.Context) {
	var req objectStorageBrowseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := c.GetUint("tenant_id")

	result, err := h.service.ListFiles(c.Request.Context(), tenantID, req.Scope, req.EngineID, req.Prefix)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		case errors.Is(err, service.ErrSystemIntegrationDisabled):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system integration not available"})
		case errors.Is(err, service.ErrEngineAccessDenied):
			c.JSON(http.StatusForbidden, gin.H{"error": "resource not accessible"})
		case errors.Is(err, service.ErrObjectStorageNotSupported):
			c.JSON(http.StatusBadRequest, gin.H{"error": "resource is not object storage"})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, result)
}
