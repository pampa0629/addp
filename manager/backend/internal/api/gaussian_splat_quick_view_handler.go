package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

type GaussianSplatQuickViewHandler struct {
	repo          *repository.GaussianSplatQuickViewRepository
	minioClient   *minio.Client
	defaultBucket string
}

func NewGaussianSplatQuickViewHandler(repo *repository.GaussianSplatQuickViewRepository, minioClient *minio.Client, defaultBucket string) *GaussianSplatQuickViewHandler {
	return &GaussianSplatQuickViewHandler{repo: repo, minioClient: minioClient, defaultBucket: defaultBucket}
}

// GetGaussianSplatQuickViewContent 返回 Manager 高斯泼溅 KSplat 快显内容。
// @Summary 读取高斯泼溅 KSplat 快显 | Read Gaussian Splat KSplat quick view result
// @Description 按 gaussian splat quick view id 返回 Manager infra MinIO 中的 KSplat 内容，支持 HTTP Range。该接口只读取 Manager 拥有生命周期且状态为 ready 的 KSplat 快显结果。 | Return KSplat content from Manager infra MinIO by result id with HTTP Range support. Only ready Manager-owned results are readable.
// @Tags Manager
// @Produce octet-stream
// @Param id path int true "gaussian splat quick view ID"
// @Success 200 "KSplat 内容流 | KSplat content stream"
// @Success 206 "部分 KSplat 内容流 | Partial KSplat content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "KSplat 不存在或未就绪 | KSplat not found or not ready"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /gaussian_splat_quick_view/{id}/content [get]
// @Security BearerAuth
func (h *GaussianSplatQuickViewHandler) GetGaussianSplatQuickViewContent(c *gin.Context) {
	if h == nil || h.repo == nil || h.minioClient == nil {
		commonAPI.InternalServerError(c, "gaussian splat quick view service is not available")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		commonAPI.BadRequestError(c, "invalid gaussian splat quick view id")
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
	if result == nil || result.Status != models.GaussianSplatQuickViewStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "gaussian splat quick view is not ready")
		return
	}
	bucket, objectName, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	objInfo, err := h.minioClient.StatObject(c.Request.Context(), bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "gaussian splat quick view object not found")
		return
	}
	getOpts := minio.GetObjectOptions{}
	rangeHeader := c.GetHeader("Range")
	contentLength := objInfo.Size
	contentRange := ""
	statusCode := http.StatusOK
	if rangeHeader != "" {
		start, end, err := parseGaussianSplatHTTPRange(rangeHeader, objInfo.Size)
		if err != nil {
			if errors.Is(err, errGaussianSplatQuickViewInvalidRange) {
				commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, err.Error())
				return
			}
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		if err := getOpts.SetRange(start, end); err != nil {
			commonAPI.ErrorResponse(c, http.StatusRequestedRangeNotSatisfiable, err.Error())
			return
		}
		contentLength = end - start + 1
		contentRange = "bytes " + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10) + "/" + strconv.FormatInt(objInfo.Size, 10)
		statusCode = http.StatusPartialContent
	}
	obj, err := h.minioClient.GetObject(c.Request.Context(), bucket, objectName, getOpts)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer obj.Close()

	contentType := "application/vnd.gaussian-ksplat"
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", storageStreamContentDisposition(result.FileName, contentType))
	c.Header("Content-Length", strconv.FormatInt(contentLength, 10))
	if contentRange != "" {
		c.Header("Content-Range", contentRange)
	}
	c.Status(statusCode)
	if _, err := io.Copy(c.Writer, obj); err != nil {
		logger.L().Error("gaussian splat quick view stream failed", "error", err, "result_id", result.ID)
	}
}

var errGaussianSplatQuickViewInvalidRange = errors.New("invalid range")

func parseGaussianSplatHTTPRange(header string, size int64) (int64, int64, error) {
	if size <= 0 {
		return 0, 0, errGaussianSplatQuickViewInvalidRange
	}
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, errGaussianSplatQuickViewInvalidRange
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if strings.Contains(spec, ",") {
		return 0, 0, errGaussianSplatQuickViewInvalidRange
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errGaussianSplatQuickViewInvalidRange
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, errGaussianSplatQuickViewInvalidRange
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, nil
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, errGaussianSplatQuickViewInvalidRange
	}
	end := size - 1
	if strings.TrimSpace(parts[1]) != "" {
		end, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || end < start {
			return 0, 0, errGaussianSplatQuickViewInvalidRange
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, nil
}
