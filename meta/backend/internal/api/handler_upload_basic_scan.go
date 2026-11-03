package api

import (
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

// EnsureUploadBasicScan godoc
// @Summary 识别成功上传的精确文件 | Identify exact committed upload files
// @Description 仅固定 Manager 运行身份；反查同租户真实上传回执，不扩大父目录、不签发权限。重复调用返回原扫描 | Fixed Manager Runtime only. Resolves real same-tenant upload receipt; no parent enumeration or Grant. Replays return the original scan
// @Tags Meta Scan
// @Produce json
// @Security BearerAuth
// @Param upload_id path string true "上传 UUID | Upload UUID"
// @Success 200 {object} client.MetaUploadIdentification
// @Failure 400,401,403,409,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /runtime/manager-uploads/{upload_id}/basic-scan [post]
func (h *Handler) EnsureUploadBasicScan(c *gin.Context) { h.uploadBasicScan(c, true) }

// ReadUploadIdentification godoc
// @Summary 读取上传首次识别事实 | Read first identification facts for an upload
// @Description 仅固定 System 运行身份；只读 Meta 既有扫描结果，不回源、不创建任务或授权 | Fixed System Runtime only. Reads existing Meta results without source reads, scan creation or Grants
// @Tags Meta Scan
// @Produce json
// @Security BearerAuth
// @Param upload_id path string true "上传 UUID | Upload UUID"
// @Success 200 {object} client.MetaUploadIdentification
// @Failure 400,401,403,409,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.task_provider.read"]
// @Router /runtime/manager-uploads/{upload_id}/basic-scan [get]
func (h *Handler) ReadUploadIdentification(c *gin.Context) { h.uploadBasicScan(c, false) }
func (h *Handler) uploadBasicScan(c *gin.Context, ensure bool) {
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("upload_id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("upload_id") || c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		c.JSON(400, gin.H{"error_code": "scan_request_invalid", "error": commoni18n.T(c, metai18n.MsgScanRequestInvalid)})
		return
	}
	var result *client.MetaUploadIdentification
	if h.executionService != nil {
		if ensure {
			result, err = h.executionService.EnsureUploadBasicScan(c.Request.Context(), commonauth.GetTenantID(c), id.String())
		} else {
			result, err = h.executionService.ReadUploadIdentification(c.Request.Context(), commonauth.GetTenantID(c), id.String())
		}
		if err == nil {
			c.JSON(http.StatusOK, result)
			return
		}
	}
	status := 503
	if errors.Is(err, commonapi.ErrConflict) {
		status = 409
	}
	if errors.Is(err, commonapi.ErrForbidden) {
		status = 403
	}
	c.JSON(status, gin.H{"error_code": "upload_scan_unavailable", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
}
