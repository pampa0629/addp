package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type DownloadHandler struct {
	metadataService *service.MetadataService
}

func NewDownloadHandler(metadataService *service.MetadataService) *DownloadHandler {
	return &DownloadHandler{metadataService: metadataService}
}

// DownloadFile 按 ResourceLocator 下载存储型数据项。
// @Summary 下载存储数据项 | Download storage item
// @Description 按 ResourceLocator 下载存储型 engine 的 item；单文件直接流式下载，多文件组合自动打包 ZIP | Download a storage engine item by ResourceLocator; bundles multi-ref items as ZIP
// @Tags Manager
// @Produce octet-stream
// @Param locator query string true "资源定位符 URI | Resource locator URI"
// @Success 200 "下载内容流 | Download content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
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
	engineID, plan, err := h.metadataService.ResolveStorageDownloadPlanByLocator(c.Request.Context(), locator, tenantIDFromContext(c))
	if err != nil {
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
	reader, err := h.metadataService.OpenStorageDownloadPlan(c.Request.Context(), engineID, plan, tenantIDFromContext(c))
	if err != nil {
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
