package api

import (
	"errors"
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type UploadHandler struct {
	uploadService *service.UploadService
}

func NewUploadHandler(uploadService *service.UploadService) *UploadHandler {
	return &UploadHandler{uploadService: uploadService}
}

// UploadFiles 上传文件到存储型节点。
// @Summary 上传文件到存储节点 | Upload files to storage node
// @Description 将用户本地文件原样写入存储型引擎节点，完成后提交 Meta 后台扫描 | Upload local files to a storage engine node and submit a Meta background scan
// @Tags Manager
// @Accept multipart/form-data
// @Produce json
// @Param target_node_locator formData string true "目标存储节点 ResourceLocator | Target storage node ResourceLocator"
// @Param files formData file true "一个或多个文件 | One or more files"
// @Success 201 {object} service.UploadResult "上传结果 | Upload result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /uploads [post]
// @Security BearerAuth
func (h *UploadHandler) UploadFiles(c *gin.Context) {
	if h == nil || h.uploadService == nil {
		commonAPI.InternalServerError(c, "upload service is not available")
		return
	}
	targetNodeLocator := strings.TrimSpace(c.PostForm("target_node_locator"))
	if targetNodeLocator == "" {
		missingLocator(c)
		return
	}
	form, err := c.MultipartForm()
	if err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	headers := form.File["files"]
	if len(headers) == 0 {
		if single := form.File["file"]; len(single) > 0 {
			headers = single
		}
	}
	if len(headers) == 0 {
		commonAPI.BadRequestError(c, service.ErrUploadNoFiles.Error())
		return
	}

	files := make([]service.UploadFile, 0, len(headers))
	opened := make([]interface{ Close() error }, 0, len(headers))
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			closeAll(opened)
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		opened = append(opened, file)
		files = append(files, service.UploadFile{
			FileName:    header.Filename,
			ContentType: header.Header.Get("Content-Type"),
			Reader:      file,
		})
	}
	defer closeAll(opened)

	result, err := h.uploadService.UploadFiles(c.Request.Context(), &service.UploadRequest{
		TargetNodeLocator: targetNodeLocator,
		Files:             files,
		TenantID:          tenantIDValue(c),
	})
	if err != nil {
		uploadError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func closeAll(values []interface{ Close() error }) {
	for _, value := range values {
		if value != nil {
			_ = value.Close()
		}
	}
}

func uploadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrEngineAccessDenied):
		accessDeniedToEngine(c)
	case errors.Is(err, service.ErrUploadNoFiles),
		errors.Is(err, service.ErrUploadTargetUnsupported),
		errors.Is(err, service.ErrUploadEngineUnsupported),
		errors.Is(err, service.ErrUploadFileNameInvalid),
		errors.Is(err, service.ErrUploadFileContentMissing):
		commonAPI.BadRequestError(c, err.Error())
	case strings.Contains(err.Error(), "URI") ||
		strings.Contains(err.Error(), "locator") ||
		strings.Contains(err.Error(), "catalog"):
		commonAPI.BadRequestError(c, err.Error())
	default:
		commonAPI.InternalServerError(c, err.Error())
	}
}
