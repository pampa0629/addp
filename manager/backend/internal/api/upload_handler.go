package api

import (
	"errors"
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/engineaccess"
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
// @Description 仅租户用户；写入前同时检查上传、Meta 扫描和读取权限，当前请求以用户身份同步提交 Meta 扫描；只返回扫描 UUID | Tenant users only; check upload, Meta scan and read permissions before writes; submit scan with the current user in the synchronous request and return only its UUID
// @Tags Manager
// @Accept multipart/form-data
// @Produce json
// @Param target_node_locator formData string true "目标存储节点 ResourceLocator | Target storage node ResourceLocator"
// @Param files formData file true "一个或多个文件 | One or more files"
// @Success 201 {object} service.UploadResult "上传结果 | Upload result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 401 {object} map[string]interface{} "认证失效 | Authentication expired"
// @Failure 409 {object} map[string]interface{} "扫描范围正在运行 | Scan scope is active"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Failure 503 {object} map[string]interface{} "引擎或扫描服务不可用 | Engine or scan service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.create","meta.scan_task.execute","meta.scan_task.read"]
// @Router /uploads [post]
// @Security BearerAuth
func (h *UploadHandler) UploadFiles(c *gin.Context) {
	principal, ok := commonauth.PrincipalFromGin(c)
	if !ok || principal.Type != "user" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error_code": "permission_denied", "error": commoni18n.T(c, commoni18n.MsgForbidden)})
		return
	}
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
	}, commonauth.CanonicalBearerToken(c.GetHeader("Authorization")))
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
	case errors.Is(err, service.ErrUploadScanSubmission):
		// Files already exist. A downstream 401 must not trigger the browser
		// auth interceptor to refresh credentials and replay the upload POST.
		status := http.StatusServiceUnavailable
		var submission *commonClient.MetaScanSubmissionError
		if errors.As(err, &submission) && (submission.StatusCode == http.StatusForbidden || submission.StatusCode == http.StatusConflict) {
			status = submission.StatusCode
		}
		c.JSON(status, gin.H{"error_code": "upload_scan_submission_failed", "error": commoni18n.T(c, manageri18n.MsgUploadScanSubmissionFailed)})
	case errors.Is(err, engineaccess.ErrUnavailable):
		engineUnavailable(c)
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
