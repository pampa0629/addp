package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	commonauth "github.com/addp/common/middleware/auth"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// ExplorerHandler 数据探查 API Handler（新版本）
// 基于 ResourceLocator URI 系统
type ExplorerHandler struct {
	explorerService *service.ExplorerService
	previewResolver *preview.PreviewResolver
	metadataService *service.MetadataService
	protectionStore explorerProtectionStore
}

type explorerProtectionStore interface {
	managerprotection.LocalProjectionGate
	managerprotection.UnmanagedDataItemGate
}

// NewExplorerHandler 创建 Explorer Handler
func NewExplorerHandler(
	explorerService *service.ExplorerService,
	previewResolver *preview.PreviewResolver,
	metadataService *service.MetadataService,
	protectionStore explorerProtectionStore,
) *ExplorerHandler {
	return &ExplorerHandler{
		explorerService: explorerService,
		previewResolver: previewResolver,
		metadataService: metadataService,
		protectionStore: protectionStore,
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
// @Failure 503 {object} map[string]interface{} "引擎不可用 | Engine unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
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

	// 先解析到 Meta DataItem 的稳定指纹，再查 Owner 本地保护索引。
	// 未纳管资源只是一次本地 map miss，不访问 Security。
	req, err := h.previewResolver.ResolveRequestFromURIWithSelection(c.Request.Context(), locatorURI, page, pageSize, childName, refPath, nestedChildPath, graphSample, tenantID)
	var result *preview.PreviewResult
	var protectionRules []dataprotection.Rule
	var protectionSubject dataprotection.SubjectReference
	if principal, ok := commonauth.PrincipalFromGin(c); ok {
		protectionSubject = dataprotection.SubjectReference{Type: principal.Type, ID: principal.ID}
	}
	if err == nil && h.protectionStore != nil && tenantID != nil {
		now := time.Now().UTC()
		gate := managerprotection.DataItemGate(h.protectionStore, *tenantID, req.ItemFingerprint, now)
		protectionRules, err = managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), gate, managerprotection.ActionPreview, now)
		if err != nil {
			protectionRequired(c)
			return
		}
	}
	if err == nil {
		result, err = h.previewResolver.Preview(c.Request.Context(), req)
		if err == nil {
			err = applyPreviewProtection(result, protectionRules, protectionSubject)
			if err != nil {
				protectionRequired(c)
				return
			}
		}
	}
	if err != nil {
		if errors.Is(err, engineaccess.ErrUnavailable) {
			engineUnavailable(c)
			return
		}
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
		logger.L().Error("数据预览失败", "error_type", fmt.Sprintf("%T", err))
		managerError(c, http.StatusInternalServerError, manageri18n.MsgPreviewFailed)
		return
	}

	logger.L().Info("数据预览成功", "locator", locatorURI, "preview_type", result.PreviewType)

	// 根据 API 设计规范：查询单个资源直接返回对象
	c.JSON(http.StatusOK, result)
}

// ResourceFacts 返回不含原始数据行的资源事实。
// @Summary 获取数据项资源事实 | Get data item resource facts
// @Description 按 locator 返回 Source Engine、查询名称、Schema coverage、字段路径及空间事实，不读取原始数据行 | Return source engine, query names, schema coverage, field paths and spatial facts without reading source rows
// @Tags Manager
// @Produce json
// @Param locator query string true "ResourceLocator URI"
// @Success 200 {object} preview.ResourceFacts "资源事实 | Resource facts"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "缺少已扫描的数据项事实 | Scanned item facts not found"
// @Failure 503 {object} map[string]interface{} "引擎不可用 | Engine unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
// @Router /resource-facts [get]
// @Security BearerAuth
func (h *ExplorerHandler) ResourceFacts(c *gin.Context) {
	locatorURI := c.Query("locator")
	if locatorURI == "" {
		missingLocator(c)
		return
	}
	facts, err := h.previewResolver.ResourceFactsFromURI(c.Request.Context(), locatorURI, tenantIDFromContext(c))
	if err != nil {
		if errors.Is(err, engineaccess.ErrUnavailable) {
			engineUnavailable(c)
			return
		}
		if err == service.ErrEngineAccessDenied || err == preview.ErrEngineAccessDenied {
			accessDeniedToEngine(c)
			return
		}
		if err == preview.ErrPreviewRequiresScannedMeta {
			managerError(c, http.StatusNotFound, manageri18n.MsgMetaScanRequired)
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, facts)
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

// ListEngines 获取存储引擎选择项
// GET /api/explorer/engines
// @Summary 获取引擎列表 | List engines
// @Description 获取当前租户 active 且具备存储能力的注册引擎及其连接状态；非 online 项由前端展示并禁选 | Get active registered storage-capable engines with connection status for the current tenant; clients must show but disable non-online options
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
// GET /api/v1/manager/storage-stream?locator=addp://engine/1/path/bucket/item?type=object&item_id=1&storage_ref=bucket/path/to/file
// @Summary 存储内容流式传输 | Storage content streaming
// @Description 支持 Range 请求的单存储叶子内容流式传输，用于图片、PDF、视频等在线预览；用户下载请使用 downloads/file。storage_ref 在对象存储中为 bucket/path，在文件系统中为文件路径 | Storage leaf content streaming with Range request support for online previews such as images, PDF, and video; use downloads/file for user downloads. storage_ref is bucket/path for object catalogs and file path for file catalogs
// @Tags Manager
// @Produce octet-stream
// @Param locator query string true "带 item_id 的存储数据项定位符 | Storage DataItem locator with item_id"
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

	// locator 定位受保护的逻辑 DataItem，storage_ref 只选择其范围内的叶子。
	locatorURI := strings.TrimSpace(c.Query("locator"))
	storageRef := c.Query("storage_ref")

	if locatorURI == "" {
		missingLocator(c)
		return
	}
	if storageRef == "" {
		managerError(c, http.StatusBadRequest, manageri18n.MsgMissingParam)
		return
	}

	target, err := h.metadataService.ResolveStorageStreamTarget(c.Request.Context(), locatorURI, storageRef, tenantID)
	if err != nil {
		if err == service.ErrEngineAccessDenied || err == preview.ErrEngineAccessDenied {
			accessDeniedToEngine(c)
			return
		}
		if errors.Is(err, engineaccess.ErrUnavailable) {
			engineUnavailable(c)
			return
		}
		if errors.Is(err, service.ErrDownloadNotSupported) || strings.Contains(err.Error(), "locator") || strings.Contains(err.Error(), "storage_ref") {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	h.streamStorageTarget(c, target, tenantID)
}

func (h *ExplorerHandler) streamStorageTarget(c *gin.Context, target *service.StorageDataItem, tenantID *uint) {
	if target == nil {
		commonAPI.InternalServerError(c, "storage target is not available")
		return
	}
	if tenantID == nil || managerprotection.RequireUnmanagedDataItem(c.Request.Context(), h.protectionStore, *tenantID, target.ItemFingerprint, time.Now().UTC()) != nil {
		protectionRequired(c)
		return
	}

	// 获取 Range header
	rangeHeader := c.GetHeader("Range")

	logger.L().Info("存储内容流请求", "engine_id", target.EngineID, "storage_ref", target.StorageRef, "range", rangeHeader)

	reader, contentLength, contentRange, contentType, err := h.metadataService.StreamStorageContent(
		c.Request.Context(),
		target.EngineID,
		target.StorageRef,
		rangeHeader,
		tenantID,
	)
	if err != nil {
		if errors.Is(err, engineaccess.ErrUnavailable) {
			engineUnavailable(c)
			return
		}
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
	c.Header("Content-Disposition", storageStreamContentDisposition(target.StorageRef, contentType))

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
// GET /api/v1/manager/storage-assets/1/items/2/path/to/file
// @Summary 多文件预览资源传输 | Multi-file preview asset streaming
// @Description 以路径型 URL 传输 S3M 等多文件预览数据集中的单个资源，支持 manifest 相对路径解析与 Range 请求；资源仍通过存储引擎和租户权限校验。 | Stream one asset from a multi-file preview dataset with path-relative URL resolution and Range support; storage-engine and tenant authorization still apply.
// @Tags Manager
// @Produce octet-stream
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param item_id path int true "Meta DataItem ID"
// @Param storage_ref path string true "存储内容引用 | Storage content reference"
// @Success 200 "存储内容流 | Storage content stream"
// @Success 206 "部分存储内容流 | Partial storage content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.content.read"]
// @Router /storage-assets/{engine_id}/items/{item_id}/{storage_ref} [get]
// @Security BearerAuth
func (h *ExplorerHandler) StorageAsset(c *gin.Context) {
	engineIDValue, err := strconv.ParseUint(c.Param("engine_id"), 10, 32)
	if err != nil || engineIDValue == 0 {
		invalidEngineID(c)
		return
	}
	itemIDValue, err := strconv.ParseUint(c.Param("item_id"), 10, 32)
	if err != nil || itemIDValue == 0 {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidParam)
		return
	}
	tenantID := tenantIDFromContext(c)
	target, err := h.metadataService.ResolveStorageAssetTarget(c.Request.Context(), uint(engineIDValue), uint(itemIDValue), strings.TrimPrefix(c.Param("storage_ref"), "/"), tenantID)
	if err != nil {
		if err == service.ErrEngineAccessDenied || err == preview.ErrEngineAccessDenied {
			accessDeniedToEngine(c)
			return
		}
		if errors.Is(err, engineaccess.ErrUnavailable) {
			engineUnavailable(c)
			return
		}
		if errors.Is(err, service.ErrDownloadNotSupported) {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	h.streamStorageTarget(c, target, tenantID)
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
