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
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// ExplorerHandler 数据探查 API Handler（新版本）
// 基于 ResourceLocator URI 系统
type ExplorerHandler struct {
	explorerService *service.ExplorerService
	previewResolver *preview.PreviewResolver
	metadataService *service.MetadataService
}

// NewExplorerHandler 创建 Explorer Handler
func NewExplorerHandler(
	explorerService *service.ExplorerService,
	previewResolver *preview.PreviewResolver,
	metadataService *service.MetadataService,
) *ExplorerHandler {
	return &ExplorerHandler{
		explorerService: explorerService,
		previewResolver: previewResolver,
		metadataService: metadataService,
	}
}

func bearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// Preview 数据预览
// GET /api/explorer/preview?locator=addp://engine/1/path/public/users?type=table&page=1&page_size=20
// @Summary 数据预览 | Data preview
// @Description 根据资源定位符预览数据内容，支持表格、消息主题、文件等多种资源 | Preview data content by resource locator, including tables, message topics, files, and more
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符URI | Resource locator URI"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20，最大2000 | Page size, default 20, max 2000"
// @Param child_name query string false "容器内部 child 名称，例如 Excel 工作表 | Container child name, e.g. Excel sheet"
// @Param ref_path query string false "multi child 内的 ref 路径 | Ref path inside a multi child"
// @Param nested_child_path query string false "嵌套容器内部 child 相对路径 | Relative child path inside a nested container"
// @Param graph_sample_kind query string false "图样本类型：node_shape 或 relationship_shape | Graph sample kind: node_shape or relationship_shape"
// @Param graph_node_labels query string false "节点 label set，逗号分隔 | Node label set, comma separated"
// @Param graph_relationship_type query string false "关系类型 | Relationship type"
// @Param graph_from_labels query string false "关系起点 label set，逗号分隔 | Relationship source label set, comma separated"
// @Param graph_to_labels query string false "关系终点 label set，逗号分隔 | Relationship target label set, comma separated"
// @Success 200 {object} map[string]interface{} "预览数据 | Preview data"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Resource not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content.read"]
// @Router /preview [get]
// @Security BearerAuth
func (h *ExplorerHandler) Preview(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析 locator
	locatorURI := c.Query("locator")
	if locatorURI == "" {
		missingLocator(c)
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
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			if ps > 2000 {
				ps = 2000
			}
			pageSize = ps
		}
	}

	childName := c.Query("child_name")
	refPath := c.Query("ref_path")
	nestedChildPath := c.Query("nested_child_path")
	graphSample := graphSampleFilterFromQuery(c)
	logger.L().Info("数据预览", "locator", locatorURI, "page", page, "page_size", pageSize, "child_name", childName, "ref_path", refPath, "nested_child_path", nestedChildPath, "graph_sample", graphSample)

	// 调用 PreviewResolver
	result, err := h.previewResolver.PreviewFromURIWithSelection(c.Request.Context(), locatorURI, page, pageSize, childName, refPath, nestedChildPath, graphSample, tenantID)
	if err != nil {
		if err == service.ErrEngineAccessDenied || err == preview.ErrEngineAccessDenied {
			accessDeniedToEngine(c)
			return
		}
		if err == preview.ErrPreviewRequiresScannedMeta {
			managerError(c, http.StatusNotFound, manageri18n.MsgMetaScanRequired)
			return
		}
		// 检查是否为表不存在错误（使用 errors.As 处理包装后的错误）
		var tableNotFoundErr *preview.TableNotFoundError
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

func graphSampleFilterFromQuery(c *gin.Context) plugin.GraphSampleFilter {
	if c == nil {
		return plugin.GraphSampleFilter{}
	}
	kind := strings.TrimSpace(c.Query("graph_sample_kind"))
	if kind == "" {
		return plugin.GraphSampleFilter{}
	}
	return plugin.GraphSampleFilter{
		Kind:             kind,
		Labels:           queryCSV(c, "graph_node_labels"),
		RelationshipType: strings.TrimSpace(c.Query("graph_relationship_type")),
		FromLabels:       queryCSV(c, "graph_from_labels"),
		ToLabels:         queryCSV(c, "graph_to_labels"),
	}.Clone()
}

func queryCSV(c *gin.Context, key string) []string {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// ListEngines 获取可用引擎列表
// GET /api/explorer/engines
// @Summary 获取引擎列表 | List engines
// @Description 获取当前租户可用的存储引擎列表 | Get available storage engine list for the current tenant
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{} "引擎列表 | Engine list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
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

// StorageStream 存储叶子内容流式传输（支持 Range 请求）
// GET /api/v1/manager/storage-stream?engine_id=1&storage_ref=bucket/path/to/file
// @Summary 存储内容流式传输 | Storage content streaming
// @Description 支持 Range 请求的单存储叶子内容流式传输，用于图片、PDF、视频等在线预览；用户下载请使用 downloads/file。storage_ref 在对象存储中为 bucket/path，在文件系统中为文件路径 | Storage leaf content streaming with Range request support for online previews such as images, PDF, and video; use downloads/file for user downloads. storage_ref is bucket/path for object catalogs and file path for file catalogs
// @Tags Manager
// @Produce octet-stream
// @Param engine_id query int true "存储引擎ID | Engine ID"
// @Param storage_ref query string true "存储内容引用 | Storage content reference"
// @Success 200 "存储内容流 | Storage content stream"
// @Success 206 "部分存储内容流 | Partial storage content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.content.read"]
// @Router /storage-stream [get]
// @Security BearerAuth
func (h *ExplorerHandler) StorageStream(c *gin.Context) {
	tenantID := tenantIDFromContext(c)

	// 解析参数
	engineIDStr := c.Query("engine_id")
	storageRef := c.Query("storage_ref")

	if engineIDStr == "" || storageRef == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgMissingEngineIDOrStorageRef)
		return
	}

	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		invalidEngineID(c)
		return
	}

	// 获取 Range header
	rangeHeader := c.GetHeader("Range")

	logger.L().Info("存储内容流请求", "engine_id", engineID, "storage_ref", storageRef, "range", rangeHeader)

	reader, contentLength, contentRange, contentType, err := h.metadataService.StreamStorageContent(
		c.Request.Context(),
		uint(engineID),
		storageRef,
		rangeHeader,
		tenantID,
	)
	if err != nil {
		if err == service.ErrEngineAccessDenied || err == preview.ErrEngineAccessDenied {
			accessDeniedToEngine(c)
			return
		}
		if strings.Contains(err.Error(), "does not support storage streaming") {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		if errors.Is(err, service.ErrInvalidRange) {
			commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, err.Error())
			return
		}
		logger.L().Error("存储内容流失败", "error", err)
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer reader.Close()

	// 设置响应头
	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", storageStreamContentDisposition(storageRef, contentType))

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
		logger.L().Error("存储内容流传输失败", "error", err)
	}
}

// StorageAsset 传输多文件预览数据集中的单个受控资源，路径型 URL 用于解析 manifest 内的相对引用。
// GET /api/v1/manager/storage-assets/1/path/to/file
// @Summary 多文件预览资源传输 | Multi-file preview asset streaming
// @Description 以路径型 URL 传输 S3M 等多文件预览数据集中的单个资源，支持 manifest 相对路径解析与 Range 请求；资源仍通过存储引擎和租户权限校验。 | Stream one asset from a multi-file preview dataset with path-relative URL resolution and Range support; storage-engine and tenant authorization still apply.
// @Tags Manager
// @Produce octet-stream
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param storage_ref path string true "存储内容引用 | Storage content reference"
// @Success 200 "存储内容流 | Storage content stream"
// @Success 206 "部分存储内容流 | Partial storage content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.content.read"]
// @Router /storage-assets/{engine_id}/{storage_ref} [get]
// @Security BearerAuth
func (h *ExplorerHandler) StorageAsset(c *gin.Context) {
	query := c.Request.URL.Query()
	query.Set("engine_id", c.Param("engine_id"))
	query.Set("storage_ref", strings.TrimPrefix(c.Param("storage_ref"), "/"))
	c.Request.URL.RawQuery = query.Encode()
	h.StorageStream(c)
}

func attachmentContentDisposition(fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" || fileName == "." || fileName == "/" {
		fileName = "download"
	}
	return fmt.Sprintf("attachment; filename=%q", fileName)
}

func storageStreamContentDisposition(storageRef, contentType string) string {
	filename := path.Base(strings.TrimSpace(storageRef))
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
