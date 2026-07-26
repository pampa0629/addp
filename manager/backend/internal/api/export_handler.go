package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type ExportHandler struct {
	exportService *service.ExportService
}

func NewExportHandler(exportService *service.ExportService) *ExportHandler {
	return &ExportHandler{exportService: exportService}
}

// CreateExport 创建数据库 item 导出会话。
// @Summary 导出数据库数据项 | Export database item
// @Description 基于数据库 table item 创建导出会话，内部调用 Transfer sync 写入 Manager infra 暂存区；导出完成后通过会话文件接口下载。| Create an export session for a database table item. Manager calls Transfer sync internally and stores the output in Manager infra staging before download.
// @Tags Manager
// @Accept json
// @Produce json
// @Param request body service.ExportRequest true "导出请求 | Export request"
// @Success 202 {object} service.ExportSessionResponse "导出会话已创建 | Export session created"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /exports [post]
// @Security BearerAuth
func (h *ExportHandler) CreateExport(c *gin.Context) {
	if h == nil || h.exportService == nil {
		commonAPI.InternalServerError(c, "export service is not available")
		return
	}
	var req service.ExportRequest
	if !commonAPI.BindJSON(c, &req) {
		return
	}
	req.TenantID = auth.GetTenantID(c)
	req.UserID = auth.GetUserID(c)
	result, err := h.exportService.CreateExport(c.Request.Context(), &req)
	if err != nil {
		exportError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

// GetExport 获取数据库 item 导出会话状态。
// @Summary 获取导出会话 | Get export session
// @Description 查询导出会话状态；若底层 Transfer 已完成，响应中包含 download_url。| Query export session status. When the underlying Transfer execution succeeds, the response contains download_url.
// @Tags Manager
// @Produce json
// @Param id path int true "导出会话 ID | Export session ID"
// @Success 200 {object} service.ExportSessionResponse "导出会话 | Export session"
// @Failure 404 {object} map[string]interface{} "会话不存在 | Session not found"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /exports/{id} [get]
// @Security BearerAuth
func (h *ExportHandler) GetExport(c *gin.Context) {
	id, ok := parseExportSessionID(c)
	if !ok {
		return
	}
	result, err := h.exportService.GetExport(c.Request.Context(), id, auth.GetTenantID(c))
	if err != nil {
		exportError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DownloadExportFile 下载数据库 item 导出结果。
// @Summary 下载导出结果 | Download export result
// @Description 下载已完成导出会话的结果文件。| Download the result file of a completed export session.
// @Tags Manager
// @Produce octet-stream
// @Param id path int true "导出会话 ID | Export session ID"
// @Success 200 "下载内容流 | Download content stream"
// @Failure 404 {object} map[string]interface{} "会话不存在 | Session not found"
// @Failure 409 {object} map[string]interface{} "导出尚未完成 | Export not ready"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /exports/{id}/file [get]
// @Security BearerAuth
func (h *ExportHandler) DownloadExportFile(c *gin.Context) {
	id, ok := parseExportSessionID(c)
	if !ok {
		return
	}
	file, err := h.exportService.OpenExportFile(c.Request.Context(), id, auth.GetTenantID(c))
	if err != nil {
		exportError(c, err)
		return
	}
	defer file.Reader.Close()

	c.Header("Content-Type", file.ContentType)
	c.Header("Content-Disposition", attachmentContentDisposition(file.FileName))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, file.Reader); err != nil {
		commonAPI.InternalServerError(c, err.Error())
	}
}

func parseExportSessionID(c *gin.Context) (uint, bool) {
	raw := c.Param("id")
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		commonAPI.BadRequestError(c, "invalid export session id")
		return 0, false
	}
	return uint(id), true
}

func exportError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrEngineAccessDenied):
		accessDeniedToEngine(c)
	case errors.Is(err, service.ErrExportSessionNotFound):
		commonAPI.NotFoundError(c, "export session not found")
	case errors.Is(err, service.ErrExportNotReady):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrExportSourceUnsupported),
		errors.Is(err, service.ErrExportFormatUnsupported):
		commonAPI.BadRequestError(c, err.Error())
	default:
		commonAPI.InternalServerError(c, err.Error())
	}
}
