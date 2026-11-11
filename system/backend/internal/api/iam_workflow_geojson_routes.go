package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
)

func registerWorkflowGeoJSONRoutes(api *gin.RouterGroup, runtime *IAMRuntime, service *engineaccess.Service) error {
	handler := &workflowGeoJSONHandler{service}
	_, access, err := exactSourceAuthorizationHandlers(api, runtime, "addp-develop", handler.Access, handler.Access)
	if err != nil {
		return err
	}
	api.POST("/execution-authorizations/develop-geojson/access", access...)
	return nil
}

type workflowGeoJSONHandler struct {
	service *engineaccess.Service
}

// Access godoc
// @Summary 复核GeoJSON 读取执行的数据权限 | Verify GeoJSON reading execution access
// @Description 仅 Develop 租户服务可按当前租约核验冻结 load 节点的源 | Only the Develop tenant service may verify the frozen load source using the current lease
// @Tags Runtime 执行授权 | Runtime Execution Authorization
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body client.WorkflowGeoJSONAccessRequest true "当前执行租约 | Current execution lease"
// @Success 200 {object} client.ManagerFileAccessObservation
// @Failure 400 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.execution_authorization.execute"]
// @Router /execution-authorizations/develop-geojson/access [post]
func (h *workflowGeoJSONHandler) Access(c *gin.Context) {
	var request client.WorkflowGeoJSONAccessRequest
	if err := bindManagerFileRequest(c, &request, "execution_id", "attempt", "lease_token", "node_id"); err != nil || request.Attempt != 1 {
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
	result, err := h.service.CheckWorkflowGeoJSONAccess(c.Request.Context(), int64(tenant), request)
	if err != nil {
		respondExecutionAuthorizationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
