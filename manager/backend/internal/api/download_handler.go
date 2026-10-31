package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type DownloadHandler struct {
	metadataService *service.MetadataService
	protectionStore managerprotection.UnmanagedDataItemGate
}

func NewDownloadHandler(metadataService *service.MetadataService, protectionStore managerprotection.UnmanagedDataItemGate) *DownloadHandler {
	return &DownloadHandler{metadataService: metadataService, protectionStore: protectionStore}
}

// DownloadFile 按 ResourceLocator 下载存储型数据项。
// @Summary 下载存储数据项 | Download storage item
// @Description 按 ResourceLocator 下载存储型 engine 的 item；单文件直接流式下载，多文件 ZIP 以已确认 refs 的共同父目录为根，保留相对目录和原始名称，不摊平或改名；非法或重复组成拒绝 | Download a storage engine item by ResourceLocator; multi-ref ZIP preserves paths relative to the confirmed refs common parent without flattening or renaming; invalid or duplicate compositions are rejected
// @Description 模型源包仅接受当前普通用户 Bearer；在每个源读取前，以完整组成及同一 Engine 版本核验现行源许可。资源票据或终态执行授权不能代替；原有下载权限与保护检查仍须满足。 | Model source packages require the current User Bearer and current source permission bound to the complete composition and same Engine version before each read. Resource tickets or terminal execution authorizations cannot replace it; download permissions and protection checks still apply.
// @Description NFS FileGDB 内容须核验当前普通用户的整项授权；本接口尚不提供整目录 ZIP 打包。 | NFS FileGDB content requires the current User's whole DataItem authorization; whole-directory ZIP packaging is not provided.
// @Tags Manager
// @Produce octet-stream
// @Param locator query string true "资源定位符 URI | Resource locator URI"
// @Success 200 "下载内容流 | Download content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "需要当前用户凭据 | Current User credential required"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Failure 503 {object} map[string]interface{} "引擎不可用 | Engine unavailable"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.content.read"]
// @Router /downloads/file [get]
// @Security BearerAuth
func (h *DownloadHandler) DownloadFile(c *gin.Context) {
	if h == nil || h.metadataService == nil {
		commonAPI.InternalServerError(c, "download service is not available")
		return
	}
	locator := strings.TrimSpace(c.Query("locator"))
	if locator == "" {
		missingLocator(c)
		return
	}
	tenantID := tenantIDFromContext(c)
	target, plan, err := h.metadataService.ResolveStorageDownloadPlanByLocator(c.Request.Context(), locator, tenantID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidDownloadBundle) {
			invalidDownloadBundle(c)
			return
		}
		if errors.Is(err, engineaccess.ErrUnavailable) {
			engineUnavailable(c)
			return
		}
		if err == service.ErrEngineAccessDenied || err == preview.ErrEngineAccessDenied {
			accessDeniedToEngine(c)
			return
		}
		if strings.Contains(err.Error(), "does not support storage streaming") ||
			errors.Is(err, service.ErrDownloadNotSupported) ||
			strings.Contains(err.Error(), "URI") ||
			strings.Contains(err.Error(), "locator") {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	if tenantID == nil || managerprotection.RequireUnmanagedDataItem(c.Request.Context(), h.protectionStore, *tenantID, target.ItemFingerprint, time.Now().UTC()) != nil {
		protectionRequired(c)
		return
	}
	principal, exists := commonauth.PrincipalFromGin(c)
	reader, err := h.metadataService.OpenStorageDownloadPlan(c.Request.Context(), target, plan, tenantID, commonauth.CanonicalBearerToken(c.GetHeader("Authorization")), exists && principal.Type == "user")
	if err != nil {
		if writePreviewAuthorizationError(c, err) {
			return
		}
		if errors.Is(err, service.ErrInvalidDownloadBundle) {
			invalidDownloadBundle(c)
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
	defer reader.Close()

	contentType := plan.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", attachmentContentDisposition(plan.FileName))
	c.Status(http.StatusOK)

	if _, err := io.Copy(c.Writer, reader); err != nil {
		commonAPI.InternalServerError(c, err.Error())
	}
}
