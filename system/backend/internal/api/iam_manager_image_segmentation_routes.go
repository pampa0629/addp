package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
)

func registerManagerImageSegmentationRoutes(api *gin.RouterGroup, runtime *IAMRuntime, service *engineaccess.Service) error {
	handler := &managerImageSegmentationHandler{service}
	_, access, err := exactSourceAuthorizationHandlers(api, runtime, "addp-manager", handler.Access, handler.Access)
	if err != nil {
		return err
	}
	api.POST("/execution-authorizations/manager-image-segmentation/access", access...)
	return nil
}

type managerImageSegmentationHandler struct {
	service *engineaccess.Service
}

// Access godoc
// @Summary 复核影像分割执行的数据权限 | Verify image segmentation execution access
// @Description 仅 Manager 租户服务可按当前租约核验固定源与目标 | Only the Manager tenant service may verify frozen resources using the current lease
// @Tags Runtime 执行授权 | Runtime Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body client.ImageSegmentationAccessRequest true "当前执行租约 | Current execution lease"
// @Success 200 {object} client.ImageSegmentationAccessObservation
// @Failure 400 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/manager-image-segmentation/access [post]
func (h *managerImageSegmentationHandler) Access(c *gin.Context) {
	var request client.ImageSegmentationAccessRequest
	if err := bindManagerFileRequest(c, &request, "execution_id", "attempt", "lease_token"); err != nil || request.Attempt != 1 {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	if _, err := parseCanonicalExecutionUUID(request.ExecutionID); err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	if _, err := parseCanonicalExecutionUUID(request.LeaseToken); err != nil {
		respondExecutionAuthorizationError(c, commonapi.ErrBadRequest)
		return
	}
	_, tenant, _, err := iamTenantActor(c)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	result, err := h.service.CheckImageSegmentationAccess(c.Request.Context(), int64(tenant), request)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
