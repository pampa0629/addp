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

type Model3DGLBHandler struct {
	repo          *repository.Model3DGLBRepository
	minioClient   *minio.Client
	defaultBucket string
}

func NewModel3DGLBHandler(repo *repository.Model3DGLBRepository, minioClient *minio.Client, defaultBucket string) *Model3DGLBHandler {
	return &Model3DGLBHandler{repo: repo, minioClient: minioClient, defaultBucket: defaultBucket}
}

// GetModel3DGLBContent 返回 Manager 三维模型 GLB 快显内容。
// @Summary 读取三维模型 GLB 快显 | Read model 3D GLB GLB result
// @Description 按 model 3D GLB id 返回 Manager infra MinIO 中的 GLB 内容，支持 HTTP Range。该接口只读取 Manager 拥有生命周期且状态为 ready 的 GLB 快显结果。 | Return GLB content from Manager infra MinIO by result id with HTTP Range support. Only ready Manager-owned results are readable.
// @Tags Manager
// @Produce octet-stream
// @Param id path int true "model 3D GLB ID"
// @Success 200 "GLB 内容流 | GLB content stream"
// @Success 206 "部分 GLB 内容流 | Partial GLB content stream"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "GLB 不存在或未就绪 | GLB not found or not ready"
// @Failure 416 {object} map[string]interface{} "Range 不可满足 | Range not satisfiable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model_3d_glb/{id}/content [get]
// @Security BearerAuth
func (h *Model3DGLBHandler) GetModel3DGLBContent(c *gin.Context) {
	if h == nil || h.repo == nil || h.minioClient == nil {
		commonAPI.InternalServerError(c, "model 3d GLB service is not available")
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		commonAPI.BadRequestError(c, "invalid model 3d GLB id")
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
	if result == nil || result.Status != models.Model3DGLBStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "model 3d GLB is not ready")
		return
	}
	bucket, objectName, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	objInfo, err := h.minioClient.StatObject(c.Request.Context(), bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "model 3d GLB object not found")
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

	contentType := "model/gltf-binary"
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
		logger.L().Error("model 3d GLB stream failed", "error", err, "result_id", result.ID)
	}
}

var errModel3DGLBInvalidRange = errors.New("invalid range")

func parseHTTPRange(header string, size int64) (int64, int64, error) {
	if size <= 0 {
		return 0, 0, errModel3DGLBInvalidRange
	}
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, errModel3DGLBInvalidRange
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if strings.Contains(spec, ",") {
		return 0, 0, errModel3DGLBInvalidRange
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errModel3DGLBInvalidRange
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, errModel3DGLBInvalidRange
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, nil
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, errModel3DGLBInvalidRange
	}
	end := size - 1
	if strings.TrimSpace(parts[1]) != "" {
		end, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil || end < start {
			return 0, 0, errModel3DGLBInvalidRange
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, nil
}
