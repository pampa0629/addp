package api

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"net/http"
)

type uploadInitialGrantService interface {
	IssueInitialUploadGrant(context.Context, engineaccess.UploadRuntimeActor, string, string) (*shared.UploadInitialResult, error)
}
type UploadInitialGrantHandler struct{ service uploadInitialGrantService }

func RegisterUploadInitialGrantRoute(api *gin.RouterGroup, runtime *IAMRuntime, h *UploadInitialGrantHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil || h == nil || h.service == nil {
		return errors.New("upload initial Grant dependencies required")
	}
	tenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessInitialGrantExecute)
	if err != nil {
		return err
	}
	group := api.Group("/runtime/initial-upload-grants/:upload_id/items/:fingerprint")
	group.Use(runtime.Authentication, runtime.ServiceCredential, tenant, sharedauth.MustNewServiceClientGuard("addp-manager"), permission)
	group.POST("", h.Issue)
	return nil
}

// Issue godoc
// @Summary 续办上传新数据初始使用权 | Continue initial access for a newly uploaded DataItem
// @Description 仅固定 Manager 运行身份；System 反查真实上传回执、Meta 首次识别与当前边界、原上传者和历史。成功与审计原子提交，重放不恢复已撤销授权，暂时故障不记录拒绝终态 | Fixed Manager Runtime only. System independently verifies committed receipt, Meta first identification/current boundary, uploader and history. Grant and audit commit atomically; replay never restores revoked Grants; temporary failures create no terminal denial
// @Tags 源授权办理 | Source Access Fulfillment
// @Produce json
// @Security BearerAuth
// @Param upload_id path string true "上传 UUID | Upload UUID"
// @Param fingerprint path string true "数据项指纹 | DataItem fingerprint"
// @Success 200 {object} shared.UploadInitialResult
// @Failure 400,401,403,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_initial_grant.execute"]
// @Router /runtime/initial-upload-grants/{upload_id}/items/{fingerprint} [post]
func (h *UploadInitialGrantHandler) Issue(c *gin.Context) {
	actor, ok := initialSourceRuntimeInput(c, "addp-manager", "upload_id")
	if !ok {
		return
	}
	fp := c.Param("fingerprint")
	if !authorizationfacts.ValidItemFingerprint(fp) {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.service.IssueInitialUploadGrant(c.Request.Context(), engineaccess.UploadRuntimeActor{Actor: actor.Actor, ClientID: actor.ClientID}, c.Param("upload_id"), fp)
	if err != nil {
		respondInitialGrantError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
