package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
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
// @Summary 获取资源树 | Get resource tree
// @Description 获取指定存储引擎的完整资源树结构 | Get the complete resource tree structure for a specific engine
// @Tags Manager
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param expand_depth query int false "展开深度，默认2 | Expand depth, default 2"
// @Success 200 {object} map[string]interface{} "资源树 | Resource tree"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Router /tree/{engine_id} [get]
// @Security BearerAuth
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
// @Summary 刷新资源节点 | Refresh resource node
// @Description 刷新指定资源节点的元数据信息 | Refresh metadata for a specific resource node
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符URI | Resource locator URI"
// @Success 200 {object} map[string]interface{} "刷新后的节点信息 | Refreshed node info"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Router /tree/{engine_id}/refresh [post]
// @Security BearerAuth
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
// @Summary 数据预览 | Data preview
// @Description 根据资源定位符预览数据内容，支持表格、文件等多种格式 | Preview data content by resource locator, supports tables, files, and more
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符URI | Resource locator URI"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Param child_name query string false "容器内部 child 名称，例如 Excel 工作表 | Container child name, e.g. Excel sheet"
// @Param component_path query string false "multi child 内的组件路径 | Component path inside a multi child"
// @Success 200 {object} map[string]interface{} "预览数据 | Preview data"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @Router /preview [get]
// @Security BearerAuth
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

	componentPath := c.Query("component_path")
	logger.L().Info("数据预览", "locator", locatorURI, "page", page, "page_size", pageSize, "child_name", c.Query("child_name"), "component_path", componentPath)

	// 调用 PreviewResolver
	result, err := h.previewResolver.PreviewFromURIWithComponent(c.Request.Context(), locatorURI, page, pageSize, c.Query("child_name"), componentPath, tenantID)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		if err == service.ErrPreviewRequiresScannedMeta {
			commonAPI.NotFoundError(c, "Resource has not been scanned by meta")
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
// @Summary 获取引擎列表 | List engines
// @Description 获取当前租户可用的存储引擎列表 | Get available storage engine list for the current tenant
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{} "引擎列表 | Engine list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /engines [get]
// @Security BearerAuth
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
// @Summary 获取节点子节点 | Get node children
// @Description 增量加载指定节点的子节点，避免全量重载 | Incrementally load children of a specific node to avoid full reload
// @Tags Manager
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param locator query string true "资源定位符URI | Resource locator URI"
// @Param expand_depth query int false "展开深度，默认1 | Expand depth, default 1"
// @Success 200 {object} map[string]interface{} "子节点列表 | Children list"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Router /tree/{engine_id}/node [get]
// @Security BearerAuth
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
// @Summary 搜索资源节点 | Search resource nodes
// @Description 在资源树中搜索匹配关键词的节点 | Search for nodes matching a keyword in the resource tree
// @Tags Manager
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param q query string true "搜索关键词（至少2个字符）| Search keyword (at least 2 characters)"
// @Param node_types query string false "节点类型过滤，逗号分隔 | Node type filter, comma-separated"
// @Param limit query int false "返回数量限制，默认50 | Result limit, default 50"
// @Success 200 {object} map[string]interface{} "搜索结果 | Search results"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Router /tree/{engine_id}/search [get]
// @Security BearerAuth
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

// GetGraphSchema 获取图数据库的 Schema 结构（节点标签 + 关系类型）
// GET /api/manager/graph-schema/:engine_id?database=neo4j
// @Summary 获取图数据库 Schema | Get graph database schema
// @Description 获取图数据库的节点标签和关系类型 | Get node labels and relationship types from graph database
// @Tags Manager
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param database query string false "数据库名称 | Database name"
// @Success 200 {object} map[string]interface{} "图 Schema | Graph schema"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /graph-schema/{engine_id} [get]
// @Security BearerAuth
func (h *ExplorerHandler) GetGraphSchema(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		commonAPI.BadRequestError(c, "Invalid engine_id")
		return
	}

	database := c.Query("database")

	schema, err := h.explorerService.GetGraphSchema(c.Request.Context(), tenantID, uint(engineID), database)
	if err != nil {
		if err == service.ErrEngineAccessDenied {
			commonAPI.ForbiddenError(c, "Access denied to this engine")
			return
		}
		logger.L().Error("获取图 Schema 失败", "engine_id", engineID, "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, schema)
}

// ObjectStream 对象内容流式传输（支持 Range 请求）
// GET /api/v1/manager/object-stream?engine_id=1&object_key=bucket/path/to/file
// @Summary 对象内容流式传输 | Object content streaming
// @Description 支持 Range 请求的对象内容流式传输，用于图片、视频等媒体在线预览 | Object content streaming with Range request support for media preview
// @Tags Manager
// @Produce octet-stream
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param object_key query string true "对象存储路径 | Object storage path"
// @Success 200 "对象内容流 | Object content stream"
// @Success 206 "部分对象内容流 | Partial object content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Router /object-stream [get]
// @Security BearerAuth
func (h *ExplorerHandler) ObjectStream(c *gin.Context) {
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

	logger.L().Info("对象流请求", "engine_id", engineID, "object_key", objectKey, "range", rangeHeader)

	reader, contentLength, contentRange, contentType, err := h.metadataService.StreamObject(
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
		if strings.Contains(err.Error(), "does not support object streaming") {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		logger.L().Error("对象流失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer reader.Close()

	// 设置响应头
	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", objectStreamContentDisposition(objectKey, contentType))

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
		logger.L().Error("对象流传输失败", "error", err)
	}
}

func objectStreamContentDisposition(objectKey, contentType string) string {
	filename := path.Base(strings.TrimSpace(objectKey))
	if filename == "." || filename == "/" || filename == "" {
		filename = "download"
	}
	disposition := "attachment"
	if streamContentTypeIsInline(contentType) {
		disposition = "inline"
	}
	return fmt.Sprintf("%s; filename=%q", disposition, filename)
}

func streamContentTypeIsInline(contentType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch normalized {
	case "application/pdf":
		return true
	default:
		return strings.HasPrefix(normalized, "image/") ||
			strings.HasPrefix(normalized, "video/") ||
			strings.HasPrefix(normalized, "audio/") ||
			strings.HasPrefix(normalized, "text/")
	}
}
