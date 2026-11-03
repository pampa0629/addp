package api

import (
	"encoding/json"
	"errors"
	commoni18n "github.com/addp/common/middleware/i18n"
	"gorm.io/gorm"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ReadReceipt godoc
// @Summary 读取本租户成功上传写入回执 | Read a committed upload receipt in the current tenant
// @Description 仅固定 Meta、System 运行身份；返回精确成功写入及原人员来源，不返回凭据、文件内容或授权结论；不创建扫描或授权 | Fixed Meta/System Runtime only. Returns precise committed writes and original human provenance, never credentials, content or access verdicts. No scan or Grant side effects
// @Tags Manager Runtime
// @Produce json
// @Security BearerAuth
// @Param upload_id path string true "上传 UUID | Upload UUID"
// @Success 200 {object} client.UploadReceipt
// @Failure 400,401,403,404,409,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.upload_receipt.read"]
// @Router /runtime/uploads/{upload_id}/receipt [get]
func (h *UploadHandler) ReadReceipt(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("upload_id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("upload_id") || c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		uploadReceiptError(c, commonapi.ErrBadRequest)
		return
	}
	var receipt *client.UploadReceipt
	receipt, err = h.uploadService.ReadUploadReceipt(c.Request.Context(), tenantIDValue(c), id.String())
	if err != nil {
		uploadReceiptError(c, err)
		return
	}
	c.JSON(http.StatusOK, receipt)
}

type UploadInitialView struct {
	UploadID        string                              `json:"upload_id"`
	Status          string                              `json:"status"`
	InitialStatus   string                              `json:"initial_status"`
	ScanExecutionID string                              `json:"scan_execution_id,omitempty"`
	Outcomes        []authorization.UploadInitialResult `json:"outcomes"`
}

// GetUpload godoc
// @Summary 查询本人上传的识别及初始授权状态 | Query identification and initial Grant status for one's own upload
// @Description 仅当前租户原上传者，需上传功能；只读、不续办、不重新上传。已签发表示历史结果，不代替当前读取裁决 | Original uploader in current tenant with upload function only. Read-only: no continuation or re-upload. Issuance is historical, not a current read verdict
// @Tags Manager
// @Produce json
// @Security BearerAuth
// @Param upload_id path string true "上传 UUID | Upload UUID"
// @Success 200 {object} UploadInitialView
// @Failure 400,401,403,404,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.create"]
// @Router /uploads/{upload_id} [get]
func (h *UploadHandler) GetUpload(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	principal, ok := commonauth.PrincipalFromGin(c)
	id, err := uuid.Parse(c.Param("upload_id"))
	if !ok || principal.Type != "user" {
		uploadReceiptError(c, commonapi.ErrForbidden)
		return
	}
	if err != nil || id == uuid.Nil || id.String() != c.Param("upload_id") || c.Request.URL.RawQuery != "" {
		uploadReceiptError(c, commonapi.ErrBadRequest)
		return
	}
	actor, err := strconv.ParseInt(principal.ID, 10, 64)
	if err != nil {
		uploadReceiptError(c, commonapi.ErrForbidden)
		return
	}
	run, err := h.uploadService.GetOwnUpload(c.Request.Context(), tenantIDValue(c), actor, id.String())
	if err != nil {
		uploadReceiptError(c, err)
		return
	}
	var initial struct {
		Status          string                              `json:"status"`
		ScanExecutionID string                              `json:"scan_execution_id"`
		Outcomes        []authorization.UploadInitialResult `json:"outcomes"`
	}
	raw, _ := json.Marshal(run.Metadata["upload_initial"])
	_ = json.Unmarshal(raw, &initial)
	if initial.Status == "" {
		initial.Status = "pending_receipt"
	}
	c.JSON(http.StatusOK, UploadInitialView{UploadID: run.ExecutionID, Status: run.Status, InitialStatus: initial.Status, ScanExecutionID: initial.ScanExecutionID, Outcomes: initial.Outcomes})
}

func uploadReceiptError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	key := commoni18n.MsgAuthorizationServiceUnavailable
	code := "upload_state_unavailable"
	if status == http.StatusForbidden {
		key = commoni18n.MsgForbidden
		code = "permission_denied"
	}
	if status == http.StatusBadRequest || status == http.StatusConflict {
		key = commoni18n.MsgInvalidParams
		code = "upload_receipt_invalid"
	}
	if status == http.StatusNotFound {
		code = "upload_not_found"
	}
	if status == http.StatusInternalServerError {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"error_code": code, "error": commoni18n.T(c, key)})
}
