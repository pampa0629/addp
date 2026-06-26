package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type RasterCOGHandler struct {
	repo              *repository.RasterCOGRepository
	spatialPreviewSvc *service.SpatialPreviewService
}

func NewRasterCOGHandler(repo *repository.RasterCOGRepository, spatialPreviewSvc *service.SpatialPreviewService) *RasterCOGHandler {
	return &RasterCOGHandler{repo: repo, spatialPreviewSvc: spatialPreviewSvc}
}

// GetRasterCOGContent 返回 Manager 栅格快显 COG 内容。
// @Summary 读取栅格快显 COG | Read raster COG quick view result
// @Description 按 raster COG id 返回 Manager infra MinIO 中的 COG 内容，支持 HTTP Range。该接口只读取 Manager 拥有生命周期且状态为 ready 的 raster COG。 | Return COG content from Manager infra MinIO by raster COG id with HTTP Range support. Only ready Manager-owned raster COG results are readable.
// @Tags Manager
// @Produce octet-stream
// @Param id path int true "raster COG ID"
// @Success 200 "COG 内容流 | COG content stream"
// @Success 206 "部分 COG 内容流 | Partial COG content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "COG 不存在或未就绪 | COG not found or not ready"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /raster_cog/{id}/content [get]
// @Security BearerAuth
func (h *RasterCOGHandler) GetRasterCOGContent(c *gin.Context) {
	if h == nil || h.repo == nil || h.spatialPreviewSvc == nil {
		commonAPI.InternalServerError(c, "raster COG service is not available")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		commonAPI.BadRequestError(c, "invalid raster COG id")
		return
	}
	tenantID := uint(1)
	if ctxTenantID := tenantIDFromContext(c); ctxTenantID != nil && *ctxTenantID > 0 {
		tenantID = *ctxTenantID
	}
	result, err := h.repo.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	if result == nil || result.Status != models.RasterCOGStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "raster COG is not ready")
		return
	}
	reader, contentLength, contentRange, contentType, err := h.spatialPreviewSvc.OpenRasterCOG(
		c.Request.Context(),
		result.StorageRef,
		c.GetHeader("Range"),
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRange) {
			commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, err.Error())
			return
		}
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer reader.Close()

	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", storageStreamContentDisposition(result.FileName, contentType))
	c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
	if contentRange != "" {
		c.Header("Content-Range", contentRange)
		c.Status(http.StatusPartialContent)
	} else {
		c.Status(http.StatusOK)
	}
	if _, err := io.Copy(c.Writer, reader); err != nil {
		logger.L().Error("raster COG stream failed", "error", err, "result_id", result.ID)
	}
}
