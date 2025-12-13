package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
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
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("数据探查: 获取存储引擎列表成功", "resource_total", len(resources))
	c.JSON(http.StatusOK, gin.H{"data": resources})
}

// GetTree 兼容旧接口，返回所有资源的树
func (h *DataExplorerHandler) GetTree(c *gin.Context) {
	logger.L().Info("数据探查: 旧接口获取资源树")

	tenantID := tenantIDFromContext(c)

	// 提取 JWT token 并放入 context
	ctx := c.Request.Context()
	if token := c.GetHeader("Authorization"); token != "" {
		// 移除 "Bearer " 前缀
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		ctx = context.WithValue(ctx, "jwt_token", token)
	}

	tree, err := h.metadataService.GetLegacyResourceTree(ctx, tenantID)
	if err != nil {
		logger.L().Error("数据探查: 旧接口获取资源树失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("数据探查: 旧接口获取资源树成功", "resource_total", len(tree))
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// GetResourceTree 返回指定资源的 schema/表树
func (h *DataExplorerHandler) GetResourceTree(c *gin.Context) {
	resourceID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	logger.L().Info("数据探查: 开始刷新资源树", "resource_id", resourceID)

	tenantID := tenantIDFromContext(c)

	// 提取 JWT token 并放入 context
	ctx := c.Request.Context()
	if token := c.GetHeader("Authorization"); token != "" {
		// 移除 "Bearer " 前缀
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		ctx = context.WithValue(ctx, "jwt_token", token)
	}

	tree, err := h.metadataService.GetResourceTree(ctx, resourceID, tenantID)
	if err != nil {
		if errors.Is(err, service.ErrResourceAccessDenied) {
			logger.L().Warn("数据探查: 获取资源树被拒绝", "resource_id", resourceID, "tenant_id", tenantIDValue(tenantID))
			commonAPI.ForbiddenError(c, "resource not accessible")
			return
		}
		logger.L().Error("数据探查: 获取资源树失败", "error", err, "resource_id", resourceID)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("数据探查: 刷新资源树成功", "resource_id", resourceID)
	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// RefreshNode 触发 Meta 服务刷新指定节点
func (h *DataExplorerHandler) RefreshNode(c *gin.Context) {
	resourceID, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	var req service.ExplorerNodeRefreshRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}

	tenantID := tenantIDFromContext(c)
	authHeader := c.GetHeader("Authorization")

	if err := h.metadataService.RefreshExplorerNode(c.Request.Context(), resourceID, tenantID, &req, authHeader); err != nil {
		if errors.Is(err, service.ErrResourceAccessDenied) {
			logger.L().Warn("数据探查: 节点刷新被拒绝", "resource_id", resourceID, "tenant_id", tenantIDValue(tenantID))
			commonAPI.ForbiddenError(c, "resource not accessible")
			return
		}
		if strings.HasPrefix(err.Error(), "missing") {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		logger.L().Error("数据探查: 节点刷新失败", "error", err, "resource_id", resourceID)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "refresh requested"})
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
		commonAPI.BadRequestError(c, "missing required parameters")
		return
	}

	resourceID64, err := strconv.ParseUint(resourceIDStr, 10, 64)
	if err != nil {
		commonAPI.BadRequestError(c, "Invalid resource_id")
		return
	}
	resourceID := uint(resourceID64)

	page, pageSize := commonAPI.GetPaginationParams(c)

	tenantID := tenantIDFromContext(c)

	// 将JWT token放入context，供Meta客户端使用
	ctx := c.Request.Context()
	if token := c.GetHeader("Authorization"); token != "" {
		// 移除 "Bearer " 前缀
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		ctx = context.WithValue(ctx, "jwt_token", token)
	}

	preview, err := h.metadataService.PreviewTableWithContext(ctx, resourceID, schemaName, tableName, page, pageSize, tenantID)
	if err != nil {
		if errors.Is(err, service.ErrResourceAccessDenied) {
			logger.L().Warn("数据探查: 预览被拒绝", "resource_id", resourceID, "schema", schemaName, "table", tableName, "tenant_id", tenantIDValue(tenantID))
			commonAPI.ForbiddenError(c, "resource not accessible")
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	// Debug: 如果是对象预览，打印attributes信息
	if preview != nil && preview.Object != nil {
		logger.L().Info("数据探查: 预览对象",
			"resource_id", resourceID,
			"schema", schemaName,
			"table", tableName,
			"has_attributes", len(preview.Object.Attributes) > 0,
			"attributes_keys", getMapKeys(preview.Object.Attributes))
	}

	c.JSON(http.StatusOK, preview)
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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

// VideoStream 视频流式传输API
// 支持Range请求，用于视频播放器的流式加载
func (h *DataExplorerHandler) VideoStream(c *gin.Context) {
	objectKey := c.Query("object_key")
	if objectKey == "" {
		commonAPI.BadRequestError(c, "missing object_key")
		return
	}

	resourceIDStr := c.Query("resource_id")
	if resourceIDStr == "" {
		commonAPI.BadRequestError(c, "missing resource_id")
		return
	}

	resourceID64, err := strconv.ParseUint(resourceIDStr, 10, 64)
	if err != nil {
		commonAPI.BadRequestError(c, "Invalid resource_id")
		return
	}
	resourceID := uint(resourceID64)

	tenantID := tenantIDFromContext(c)

	// 获取视频内容（支持Range请求）
	rangeHeader := c.GetHeader("Range")

	// 调用service获取视频流
	videoReader, contentLength, contentRange, contentType, err := h.metadataService.StreamVideo(
		c.Request.Context(),
		resourceID,
		objectKey,
		rangeHeader,
		tenantID,
	)
	if err != nil {
		if errors.Is(err, service.ErrResourceAccessDenied) {
			commonAPI.ForbiddenError(c, "resource not accessible")
			return
		}
		logger.L().Error("视频流传输失败", "error", err, "resource_id", resourceID, "object_key", objectKey)
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer videoReader.Close()

	// 设置响应头
	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")

	if contentRange != "" {
		// Partial Content (206)
		c.Header("Content-Range", contentRange)
		c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
		c.Status(http.StatusPartialContent)
	} else {
		// Full Content (200)
		c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
		c.Status(http.StatusOK)
	}

	// 流式传输视频内容
	_, err = io.Copy(c.Writer, videoReader)
	if err != nil {
		logger.L().Warn("视频流传输中断", "error", err, "object_key", objectKey)
	}
}
