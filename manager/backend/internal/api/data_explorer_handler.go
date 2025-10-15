package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type DataExplorerHandler struct {
	metadataService *service.MetadataService
}

func NewDataExplorerHandler(metadataService *service.MetadataService) *DataExplorerHandler {
	return &DataExplorerHandler{
		metadataService: metadataService,
	}
}

// ListResources 返回可用于数据探查的存储引擎列表
func (h *DataExplorerHandler) ListResources(c *gin.Context) {
	logger.L().Info("数据探查: 获取存储引擎列表")

	tenantID := tenantIDFromContext(c)

	resources, err := h.metadataService.ListExplorerResources(tenantID)
	if err != nil {
		logger.L().Error("数据探查: 获取存储引擎列表失败", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.L().Info("数据探查: 获取存储引擎列表成功", "resource_total", len(resources))
	c.JSON(http.StatusOK, gin.H{"data": resources})
}

// GetTree 兼容旧接口，返回所有资源的树
func (h *DataExplorerHandler) GetTree(c *gin.Context) {
	logger.L().Info("数据探查: 旧接口获取资源树")

	tenantID := tenantIDFromContext(c)

	tree, err := h.metadataService.GetLegacyResourceTree(tenantID)
	if err != nil {
		logger.L().Error("数据探查: 旧接口获取资源树失败", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.L().Info("数据探查: 旧接口获取资源树成功", "resource_total", len(tree))
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// GetResourceTree 返回指定资源的 schema/表树
func (h *DataExplorerHandler) GetResourceTree(c *gin.Context) {
	resourceIDStr := c.Param("id")
	if resourceIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing resource id"})
		return
	}

	resourceIDUint, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	logger.L().Info("数据探查: 开始刷新资源树", "resource_id", resourceIDUint)

	tenantID := tenantIDFromContext(c)

	tree, err := h.metadataService.GetResourceTree(uint(resourceIDUint), tenantID)
	if err != nil {
		if errors.Is(err, service.ErrResourceAccessDenied) {
			logger.L().Warn("数据探查: 获取资源树被拒绝", "resource_id", resourceIDUint, "tenant_id", tenantIDValue(tenantID))
			c.JSON(http.StatusForbidden, gin.H{"error": "resource not accessible"})
			return
		}
		logger.L().Error("数据探查: 获取资源树失败bk", "error", err, "resource_id", resourceIDUint)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.L().Info("数据探查: 刷新资源树成功", "resource_id", resourceIDUint)
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// PreviewTable 返回表数据预览
// 支持三种情况:
// 1. table 有值: 预览具体的表或对象
// 2. table 为空: 预览 schema/bucket 节点，显示统计信息和子节点列表
func (h *DataExplorerHandler) PreviewTable(c *gin.Context) {
	resourceIDStr := c.Query("resource_id")
	schemaName := c.Query("schema")
	tableName := c.Query("table")

	// resource_id 和 schema 是必需的，table 可以为空（用于查看 schema/bucket 信息）
	if resourceIDStr == "" || schemaName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required parameters"})
		return
	}

	resourceIDUint, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	tenantID := tenantIDFromContext(c)

	preview, err := h.metadataService.PreviewTable(uint(resourceIDUint), schemaName, tableName, page, pageSize, tenantID)
	if err != nil {
		if errors.Is(err, service.ErrResourceAccessDenied) {
			logger.L().Warn("数据探查: 预览被拒绝", "resource_id", resourceIDUint, "schema", schemaName, "table", tableName, "tenant_id", tenantIDValue(tenantID))
			c.JSON(http.StatusForbidden, gin.H{"error": "resource not accessible"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, preview)
}

func tenantIDFromContext(c *gin.Context) *uint {
	value, exists := c.Get(auth.ContextTenantIDKey)
	if !exists {
		return nil
	}

	switch v := value.(type) {
	case uint:
		if v == 0 {
			return nil
		}
		id := v
		return &id
	case int:
		if v <= 0 {
			return nil
		}
		id := uint(v)
		return &id
	default:
		return nil
	}
}

func tenantIDValue(id *uint) interface{} {
	if id == nil {
		return nil
	}
	return *id
}
