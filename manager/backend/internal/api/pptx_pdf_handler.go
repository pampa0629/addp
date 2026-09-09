package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
	commoni18n "github.com/addp/common/middleware/i18n"
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

// DeleteResult 删除 PPTX PDF 快显结果及其受管对象。
// @Summary 删除 PPTX PDF 快显结果 | Delete PPTX PDF preview result
// @Description 删除 Manager infra MinIO 中的 PDF 对象并软删除对应结果记录，不删除源 DataItem、任务或 execution 历史。| Delete the PDF object from Manager infra MinIO and soft-delete the result record without deleting the source DataItem, task, or execution history.
// @Tags Manager
// @Produce json
// @Param id path int true "结果 ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "结果 ID 无效 | Invalid result ID"
// @Failure 404 {object} map[string]interface{} "结果不存在 | Result not found"
// @Failure 500 {object} map[string]interface{} "删除失败 | Delete failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /pptx_pdf/{id} [delete]
// @Security BearerAuth
func (h *PPTXPDFHandler) DeleteResult(c *gin.Context) {
	if h == nil || h.service == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgPPTXPDFServiceUnavailable)
		return
	}
	id64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id64 == 0 {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidPPTXPDFResultID)
		return
	}
	if err := h.service.DeleteResult(c.Request.Context(), uint(id64), tenantIDValue(c)); err != nil {
		if errors.Is(err, service.ErrPPTXPDFResultNotFound) {
			managerError(c, http.StatusNotFound, manageri18n.MsgPPTXPDFResultNotFound)
			return
		}
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgPPTXPDFDeleteFailed, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, manageri18n.MsgPPTXPDFResultDeleted)})
}
