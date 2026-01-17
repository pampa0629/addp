package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// ExplorerHandler 数据探查 API Handler（新版本）
// 基于 ResourceLocator URI 系统
type ExplorerHandler struct {
	explorerService *service.ExplorerService
	previewResolver *service.PreviewResolver
	metadataService *service.MetadataService
}

// NewExplorerHandler 创建 Explorer Handler
func NewExplorerHandler(
	explorerService *service.ExplorerService,
	previewResolver *service.PreviewResolver,
	metadataService *service.MetadataService,
) *ExplorerHandler {
	return &ExplorerHandler{
		explorerService: explorerService,
		previewResolver: previewResolver,
		metadataService: metadataService,
	}
}

// GetTree 获取引擎的资源树
// GET /api/explorer/tree/:engine_id?expand_depth=2
func (h *ExplorerHandler) GetTree(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析 engine_id
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		logger.L().Warn("无效的 engine_id", "engine_id", engineIDStr)
		commonAPI.BadRequestError(c, "Invalid engine_id")
		return
	}

	// 解析 expand_depth（默认 2）
	expandDepth := 2
	if depthStr := c.Query("expand_depth"); depthStr != "" {
		depth, err := strconv.Atoi(depthStr)
		if err == nil {
			expandDepth = depth
		}
	}

	logger.L().Info("获取资源树", "engine_id", engineID, "expand_depth", expandDepth)

	// 调用 ExplorerService
	tree, err := h.explorerService.GetTree(c.Request.Context(), tenantID, uint(engineID), expandDepth)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		logger.L().Error("获取资源树失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("获取资源树成功", "engine_id", engineID, "children_count", len(tree.Children))

	// 根据 API 设计规范：查询单个资源直接返回对象
	c.JSON(http.StatusOK, tree)
}

// RefreshNode 刷新指定节点
// POST /api/explorer/tree/:engine_id/refresh?locator=addp://engine/1/path/public?type=schema
func (h *ExplorerHandler) RefreshNode(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析 locator
	locatorURI := c.Query("locator")
	if locatorURI == "" {
		commonAPI.BadRequestError(c, "Missing locator parameter")
		return
	}

	logger.L().Info("刷新节点", "locator", locatorURI)

	// 调用 ExplorerService
	node, err := h.explorerService.RefreshNode(c.Request.Context(), tenantID, locatorURI)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		logger.L().Error("刷新节点失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("刷新节点成功", "locator", locatorURI)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"node":    node,
			"locator": locatorURI,
		},
	})
}

// Preview 数据预览
// GET /api/explorer/preview?locator=addp://engine/1/path/public/users?type=table&page=1&page_size=20
func (h *ExplorerHandler) Preview(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析 locator
	locatorURI := c.Query("locator")
	if locatorURI == "" {
		commonAPI.BadRequestError(c, "Missing locator parameter")
		return
	}

	// 解析分页参数
	page := 1
	pageSize := 20

	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	logger.L().Info("数据预览", "locator", locatorURI, "page", page, "page_size", pageSize)

	// 调用 PreviewResolver
	result, err := h.previewResolver.PreviewFromURI(c.Request.Context(), locatorURI, page, pageSize, tenantID)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		// 检查是否为表不存在错误（使用 errors.As 处理包装后的错误）
		var tableNotFoundErr *service.TableNotFoundError
		if errors.As(err, &tableNotFoundErr) {
			logger.L().Warn("表不存在", "error", tableNotFoundErr.Error())
			c.JSON(http.StatusNotFound, gin.H{
				"error": tableNotFoundErr.Error(),
			})
			return
		}
		logger.L().Error("数据预览失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("数据预览成功", "locator", locatorURI, "preview_type", result.PreviewType)

	// 根据 API 设计规范：查询单个资源直接返回对象
	c.JSON(http.StatusOK, result)
}

// ListEngines 获取可用引擎列表
// GET /api/explorer/engines
func (h *ExplorerHandler) ListEngines(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	logger.L().Info("获取引擎列表")

	// 调用 ExplorerService
	engines, err := h.explorerService.GetEngineList(tenantID)
	if err != nil {
		logger.L().Error("获取引擎列表失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("获取引擎列表成功", "count", len(engines))

	c.JSON(http.StatusOK, gin.H{
		"data": engines,
	})
}

// GetNodeChildren 获取节点的子节点（增量加载）
// GET /api/manager/tree/:engine_id/node?locator=addp://engine/1/path/bucket1?type=bucket&expand_depth=1
func (h *ExplorerHandler) GetNodeChildren(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析 engine_id
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		logger.L().Warn("无效的 engine_id", "engine_id", engineIDStr)
		commonAPI.BadRequestError(c, "Invalid engine_id")
		return
	}

	// 解析 locator
	locatorURI := c.Query("locator")
	if locatorURI == "" {
		commonAPI.BadRequestError(c, "Missing locator parameter")
		return
	}

	// 解析 expand_depth（默认 1）
	expandDepth := 1
	if depthStr := c.Query("expand_depth"); depthStr != "" {
		depth, err := strconv.Atoi(depthStr)
		if err == nil {
			expandDepth = depth
		}
	}

	logger.L().Info("获取节点子节点", "engine_id", engineID, "locator", locatorURI, "expand_depth", expandDepth)

	// 调用 ExplorerService
	result, err := h.explorerService.GetNodeChildren(c.Request.Context(), tenantID, uint(engineID), locatorURI, expandDepth)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		logger.L().Error("获取节点子节点失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("获取节点子节点成功", "children_count", len(result.Children))

	// 根据 API 设计规范：直接返回对象
	c.JSON(http.StatusOK, map[string]interface{}{
		"parent_locator": locatorURI,
		"children":       result.Children,
	})
}

// SearchNodes 搜索资源树节点
// GET /api/manager/tree/:engine_id/search?q=data&node_types=table,schema&limit=50
func (h *ExplorerHandler) SearchNodes(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析 engine_id
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		logger.L().Warn("无效的 engine_id", "engine_id", engineIDStr)
		commonAPI.BadRequestError(c, "Invalid engine_id")
		return
	}

	// 解析搜索关键词
	keyword := c.Query("q")
	if keyword == "" || len(keyword) < 2 {
		commonAPI.BadRequestError(c, "Search keyword must be at least 2 characters")
		return
	}

	// 解析节点类型过滤（可选）
	nodeTypesStr := c.Query("node_types")
	var nodeTypes []string
	if nodeTypesStr != "" {
		nodeTypes = strings.Split(nodeTypesStr, ",")
	}

	// 解析返回数量限制
	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	logger.L().Info("搜索资源树节点", "engine_id", engineID, "keyword", keyword, "limit", limit)

	// 调用 ExplorerService
	results, total, err := h.explorerService.SearchNodes(c.Request.Context(), tenantID, uint(engineID), keyword, nodeTypes, limit)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		logger.L().Error("搜索节点失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	logger.L().Info("搜索节点成功", "results_count", len(results), "total", total)

	// 返回搜索结果
	c.JSON(http.StatusOK, gin.H{
		"keyword": keyword,
		"total":   total,
		"results": results,
	})
}

// VideoStream 视频流式传输（支持 Range 请求）
// GET /api/explorer/video-stream?engine_id=1&object_key=bucket/path/to/video.mp4
func (h *ExplorerHandler) VideoStream(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析参数
	engineIDStr := c.Query("engine_id")
	objectKey := c.Query("object_key")

	if engineIDStr == "" || objectKey == "" {
		commonAPI.BadRequestError(c, "Missing engine_id or object_key")
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		commonAPI.BadRequestError(c, "Invalid engine_id")
		return
	}

	// 获取 Range header
	rangeHeader := c.GetHeader("Range")

	logger.L().Info("视频流请求", "engine_id", engineID, "object_key", objectKey, "range", rangeHeader)

	// 调用 MetadataService.StreamVideo
	reader, contentLength, contentRange, contentType, err := h.metadataService.StreamVideo(
		c.Request.Context(),
		uint(engineID),
		objectKey,
		rangeHeader,
		tenantID,
	)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		if strings.Contains(err.Error(), "does not support video streaming") {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		logger.L().Error("视频流失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer reader.Close()

	// 设置响应头
	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")

	if contentRange != "" {
		// Range 请求返回 206 Partial Content
		c.Header("Content-Range", contentRange)
		c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
		c.Status(http.StatusPartialContent)
	} else {
		// 完整请求返回 200 OK
		c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
		c.Status(http.StatusOK)
	}

	// 流式传输
	_, err = io.Copy(c.Writer, reader)
	if err != nil {
		logger.L().Error("视频流传输失败", "error", err)
	}
}
