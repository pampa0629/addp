package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

const pointCloudCOPCExpectedContentType = "application/vnd.laszip+copc"

type PointCloudCOPCHandler struct {
	repo          *repository.PointCloudCOPCRepository
	minioClient   *minio.Client
	defaultBucket string
}

func NewPointCloudCOPCHandler(repo *repository.PointCloudCOPCRepository, minioClient *minio.Client, defaultBucket string) *PointCloudCOPCHandler {
	return &PointCloudCOPCHandler{repo: repo, minioClient: minioClient, defaultBucket: defaultBucket}
}

// GetPointCloudCOPCContent 返回 Manager 点云 COPC 快显内容。
// @Summary 读取点云 COPC 快显 | Read point cloud COPC quick view result
// @Description 按 point cloud COPC id 返回 Manager infra MinIO 中的 COPC 内容，支持 HTTP Range。该接口只读取 Manager 拥有生命周期且状态为 ready 的 COPC 快显结果。 | Return COPC content from Manager infra MinIO by result id with HTTP Range support. Only ready Manager-owned results are readable.
// @Tags Manager
// @Produce octet-stream
// @Param id path int true "point cloud COPC ID"
// @Success 200 "COPC 内容流 | COPC content stream"
// @Success 206 "部分 COPC 内容流 | Partial COPC content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "COPC 不存在或未就绪 | COPC not found or not ready"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /point_cloud_copc/{id}/content [get]
// @Security BearerAuth
func (h *PointCloudCOPCHandler) GetPointCloudCOPCContent(c *gin.Context) {
	if h == nil || h.repo == nil || h.minioClient == nil {
		commonAPI.InternalServerError(c, "point cloud COPC service is not available")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		commonAPI.BadRequestError(c, "invalid point cloud COPC id")
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
	if result == nil || result.Status != models.PointCloudCOPCStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "point cloud COPC is not ready")
		return
	}
	bucket, objectName, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	objInfo, err := h.minioClient.StatObject(c.Request.Context(), bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "point cloud COPC object not found")
		return
	}
	getOpts := minio.GetObjectOptions{}
	rangeHeader := c.GetHeader("Range")
	contentLength := objInfo.Size
	contentRange := ""
	statusCode := http.StatusOK
	if rangeHeader != "" {
		start, end, err := parseHTTPRange(rangeHeader, objInfo.Size)
		if err != nil {
			if errors.Is(err, errModel3DGLBInvalidRange) {
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

	contentType := pointCloudCOPCExpectedContentType
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
		logger.L().Error("point cloud COPC stream failed", "error", err, "result_id", result.ID)
	}
}
