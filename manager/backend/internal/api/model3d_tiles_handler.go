package api

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

// Model3DTilesHandler 提供 Manager infra 中 3D Tiles / S3M 目录 artifact 的受控读取。
type Model3DTilesHandler struct {
	repo          *repository.Model3DTilesRepository
	minioClient   *minio.Client
	defaultBucket string
}

func NewModel3DTilesHandler(repo *repository.Model3DTilesRepository, client *minio.Client, bucket string) *Model3DTilesHandler {
	return &Model3DTilesHandler{repo: repo, minioClient: client, defaultBucket: strings.TrimSpace(bucket)}
}

// GetAsset 读取分块三维模型瓦片结果中的单个资源。
// @Summary 读取分块三维模型瓦片资源 | Read model3d tiles asset
// @Description 读取 Manager infra MinIO 中 ready 的 3D Tiles 或 S3M 结果资源，保留目录相对路径和 HTTP Range 语义。 | Read one asset from a ready Manager-owned 3D Tiles or S3M result while preserving relative paths and HTTP Range semantics.
// @Tags Manager
// @Produce application/octet-stream
// @Param id path int true "结果 ID | Result ID"
// @Param asset_path path string true "结果内相对资源路径 | Relative asset path"
// @Success 200 {file} binary
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model3d_tiles/{id}/assets/{asset_path} [get]
// @Security BearerAuth
func (h *Model3DTilesHandler) GetAsset(c *gin.Context) {
	id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		commonAPI.BadRequestError(c, "invalid model3d tiles result id")
		return
	}
	result, err := h.repo.GetResult(c.Request.Context(), uint(id64), tenantIDValue(c))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	if result == nil || result.Status != models.Model3DTilesStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "model3d tiles result not found")
		return
	}
	assetPath := strings.TrimPrefix(strings.ReplaceAll(c.Param("asset_path"), "\\", "/"), "/")
	cleaned := path.Clean(assetPath)
	if assetPath == "" || cleaned != assetPath || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		commonAPI.BadRequestError(c, "invalid model3d tiles asset path")
		return
	}
	bucket, prefix, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	objectName := path.Join(strings.Trim(prefix, "/"), cleaned)
	object, err := h.minioClient.GetObject(c.Request.Context(), bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "model3d tiles asset not found")
		return
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "model3d tiles asset not found")
		return
	}
	contentType := strings.TrimSpace(info.ContentType)
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = model3DTilesAssetContentType(cleaned)
	}
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Content-Type", contentType)
	http.ServeContent(c.Writer, c.Request, path.Base(cleaned), info.LastModified, io.NewSectionReader(object, 0, info.Size))
}

func model3DTilesAssetContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".json":
		return "application/json"
	case ".scp":
		return "application/vnd.supermap.s3m-config"
	case ".s3m", ".s3mb":
		return "application/vnd.supermap.s3m"
	case ".b3dm":
		return "application/vnd.cesium.b3dm"
	case ".i3dm":
		return "application/vnd.cesium.i3dm"
	case ".pnts":
		return "application/vnd.cesium.pnts"
	case ".cmpt":
		return "application/vnd.cesium.cmpt"
	case ".glb":
		return "model/gltf-binary"
	}
	if value := mime.TypeByExtension(path.Ext(name)); value != "" {
		return value
	}
	return "application/octet-stream"
}
