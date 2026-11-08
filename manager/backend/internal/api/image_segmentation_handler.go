package api

import (
	"errors"
	"net/http"

	auth "github.com/addp/common/middleware/auth"
	i18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type ImageSegmentationHandler struct {
	Service *service.ImageSegmentationService
}

// Run godoc
// @Summary 交互式影像分割 | Segment a preview image
// @Tags 数据探查 | Data Explorer
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.ImageSegmentInput true "分割操作 | Segmentation operation"
// @Success 200 {object} service.ImageSegmentResult
// @Failure 403 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.content.read","manager.data_item.read"]
// @Router /image-segmentation [post]
func (h *ImageSegmentationHandler) Run(c *gin.Context) {
	a, ok := auth.AuthContextFromGin(c)
	tenant := tenantIDFromContext(c)
	if !ok || tenant == nil || a.Principal.Type != "user" || a.Context.Type != "tenant" || a.Context.TenantMembershipID == nil || a.Delegation != nil {
		accessDeniedToEngine(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var in service.ImageSegmentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		managerError(c, 400, i18n.MsgInvalidParam)
		return
	}
	if h.Service == nil {
		managerError(c, 503, i18n.MsgSegmentUnavailable)
		return
	}
	identity := a.Principal.ID + "/" + *a.Context.TenantMembershipID + "/" + a.Authorization.AuthorizationVersion
	result, err := h.Service.Run(c.Request.Context(), *tenant, identity, auth.CanonicalBearerToken(c.GetHeader("Authorization")), in)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSegmentBusy):
			managerError(c, 429, i18n.MsgSegmentBusy)
		case errors.Is(err, service.ErrSegmentBinding):
			managerError(c, 503, i18n.MsgSegmentBinding)
		case errors.Is(err, service.ErrSegmentUpstream):
			managerError(c, 502, i18n.MsgSegmentUnavailable)
		case errors.Is(err, service.ErrSegmentImageUnsupported):
			managerError(c, 422, i18n.MsgSegmentUnsupported)
		default:
			if !writePreviewAuthorizationError(c, err) {
				accessDeniedToEngine(c)
			}
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, result)
}
