package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	manageri18n "github.com/addp/manager/i18n"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

type PPTXPDFHandler struct {
	service       *service.PPTXPDFTaskService
	minioClient   *minio.Client
	defaultBucket string
}

func NewPPTXPDFHandler(taskService *service.PPTXPDFTaskService, minioClient *minio.Client, defaultBucket string) *PPTXPDFHandler {
	return &PPTXPDFHandler{service: taskService, minioClient: minioClient, defaultBucket: strings.TrimSpace(defaultBucket)}
}

type PPTXPDFPreviewRequest struct {
	Locator string `json:"locator" binding:"required"`
	Retry   bool   `json:"retry,omitempty"`
}

type PPTXPDFPreviewResponse struct {
	Status      string `json:"status"`
	TaskID      uint   `json:"task_id"`
	ExecutionID string `json:"execution_id,omitempty"`
	ResultID    uint   `json:"result_id,omitempty"`
	PreviewURL  string `json:"preview_url,omitempty"`
	PageCount   int    `json:"page_count,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	Error       string `json:"error,omitempty"`
}

// EnsurePreview 创建或复用 PPTX PDF 任务，并在需要时自动触发转换。
// @Summary 获取或生成 PPTX PDF 快显 | Resolve or generate PPTX PDF preview
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body PPTXPDFPreviewRequest true "PPTX source identity"
// @Success 200 {object} PPTXPDFPreviewResponse "快显已就绪 | Preview ready"
// @Success 202 {object} PPTXPDFPreviewResponse "转换已受理或执行中 | Conversion accepted or running"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务执行错误 | Internal server error"
// @Failure 503 {object} map[string]interface{} "服务不可用 | Service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read","manager.derived_artifact.create"]
// @Router /pptx_pdf/preview [post]
// @Security BearerAuth
func (h *PPTXPDFHandler) EnsurePreview(c *gin.Context) {
	if h == nil || h.service == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgPPTXPDFServiceUnavailable)
		return
	}
	var req PPTXPDFPreviewRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody, err.Error())
		return
	}
	tenantID, userID := tenantIDValue(c), userIDValue(c)
	task := &models.PPTXPDFTask{TenantID: tenantID, Enabled: true, Locator: strings.TrimSpace(req.Locator), CreatedBy: &userID}
	if err := h.service.EnsureTask(c.Request.Context(), task); err != nil {
		if errors.Is(err, service.ErrInvalidPPTXPDFSource) {
			managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody, err.Error())
			return
		}
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgPPTXPDFResolveFailed, err.Error())
		return
	}
	current, err := h.service.Current(c.Request.Context(), tenantID, task.ItemFingerprint)
	if err != nil {
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgPPTXPDFResolveFailed, err.Error())
		return
	}
	if current != nil && current.Status == models.PPTXPDFStatusReady && current.SourceVersion == task.SourceVersion {
		c.JSON(http.StatusOK, PPTXPDFPreviewResponse{Status: "ready", TaskID: task.ID, ResultID: current.ID, PreviewURL: current.ContentURL, PageCount: current.PageCount, SizeBytes: current.SizeBytes})
		return
	}
	if task.LastExecutionStatus != nil && (*task.LastExecutionStatus == commonExecution.ExecutionStatusPending || *task.LastExecutionStatus == commonExecution.ExecutionStatusRunning) && task.LastExecutionID != nil {
		c.JSON(http.StatusAccepted, PPTXPDFPreviewResponse{Status: *task.LastExecutionStatus, TaskID: task.ID, ExecutionID: *task.LastExecutionID})
		return
	}
	if !req.Retry && task.LastExecutionStatus != nil && isPPTXPDFTerminalFailure(*task.LastExecutionStatus) && (current == nil || current.SourceVersion == task.SourceVersion) {
		message := ""
		resultID := uint(0)
		if current != nil {
			message = current.ErrorMessage
			resultID = current.ID
		}
		c.JSON(http.StatusOK, PPTXPDFPreviewResponse{Status: "failed", TaskID: task.ID, ResultID: resultID, Error: message})
		return
	}
	executionID, err := h.service.Execute(c.Request.Context(), task.ID, tenantID, commonExecution.TriggerTypeManual, commonExecution.ModuleManager, nil, current != nil)
	if err != nil {
		if errors.Is(err, service.ErrTaskExecutionBusy) {
			latest, getErr := h.service.GetByID(c.Request.Context(), task.ID, tenantID)
			if getErr == nil && latest != nil && latest.LastExecutionID != nil {
				c.JSON(http.StatusAccepted, PPTXPDFPreviewResponse{Status: "running", TaskID: latest.ID, ExecutionID: *latest.LastExecutionID})
				return
			}
		}
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgPPTXPDFExecutionFailed, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, PPTXPDFPreviewResponse{Status: "pending", TaskID: task.ID, ExecutionID: executionID})
}

func isPPTXPDFTerminalFailure(status string) bool {
	switch status {
	case commonExecution.ExecutionStatusFailed, commonExecution.ExecutionStatusTimeout, commonExecution.ExecutionStatusCancelled:
		return true
	default:
		return false
	}
}

// GetContent 返回已就绪的 PPTX PDF 快显内容。
// @Summary 读取 PPTX PDF 快显 | Read PPTX PDF preview
// @Tags Manager
// @Produce application/pdf
// @Param id path int true "PPTX PDF result ID"
// @Success 200 "PDF 内容流 | PDF content stream"
// @Success 206 "部分 PDF 内容流 | Partial PDF content stream"
// @Failure 404 {object} map[string]interface{} "快显不存在或未就绪 | Preview not found or not ready"
// @Failure 500 {object} map[string]interface{} "服务执行错误 | Internal server error"
// @Failure 503 {object} map[string]interface{} "服务不可用 | Service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /pptx_pdf/{id}/content [get]
// @Security BearerAuth
func (h *PPTXPDFHandler) GetContent(c *gin.Context) {
	if h == nil || h.service == nil || h.minioClient == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgPPTXPDFServiceUnavailable)
		return
	}
	id64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id64 == 0 {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidPPTXPDFResultID)
		return
	}
	result, err := h.service.GetResult(c.Request.Context(), uint(id64), tenantIDValue(c))
	if err != nil {
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgPPTXPDFResolveFailed, err.Error())
		return
	}
	if result == nil || result.Status != models.PPTXPDFStatusReady {
		managerError(c, http.StatusNotFound, manageri18n.MsgPPTXPDFResultNotReady)
		return
	}
	bucket, objectName, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgPPTXPDFResolveFailed, err.Error())
		return
	}
	info, err := h.minioClient.StatObject(c.Request.Context(), bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		managerError(c, http.StatusNotFound, manageri18n.MsgPPTXPDFObjectNotFound)
		return
	}
	opts := minio.GetObjectOptions{}
	contentLength, statusCode, contentRange := info.Size, http.StatusOK, ""
	if rangeHeader := c.GetHeader("Range"); rangeHeader != "" {
		start, end, rangeErr := parseHTTPRange(rangeHeader, info.Size)
		if rangeErr != nil {
			commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, rangeErr.Error())
			return
		}
		if err := opts.SetRange(start, end); err != nil {
			commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, err.Error())
			return
		}
		contentLength, statusCode = end-start+1, http.StatusPartialContent
		contentRange = "bytes " + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10) + "/" + strconv.FormatInt(info.Size, 10)
	}
	object, err := h.minioClient.GetObject(c.Request.Context(), bucket, objectName, opts)
	if err != nil {
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgPPTXPDFResolveFailed, err.Error())
		return
	}
	defer object.Close()
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", storageStreamContentDisposition(result.FileName, "application/pdf"))
	c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "private, no-store")
	if contentRange != "" {
		c.Header("Content-Range", contentRange)
	}
	c.Status(statusCode)
	if _, err := io.Copy(c.Writer, object); err != nil {
		logger.L().Error("PPTX PDF stream failed", "result_id", result.ID, "error", err)
	}
}
