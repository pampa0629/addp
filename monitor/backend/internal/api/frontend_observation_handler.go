package api

import (
	"net/http"
	"net/url"
	"strconv"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FrontendObservation godoc
// @Summary 读取前端采集摘要 | Read frontend observation
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Param node_id query string true "采集节点 ID | Collection node ID"
// @Success 200 {object} service.FrontendObservation
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read","platform.host_node.read"]
// @Router /platform/frontend_observations [get]
func (h *ResourceObservationHandler) FrontendObservation(c *gin.Context) {
	if len(c.Request.URL.RawQuery) > 128 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	q, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(q) != 1 || len(q["node_id"]) != 1 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	raw := q.Get("node_id")
	node, err := uuid.Parse(raw)
	if err != nil || node == uuid.Nil || node.String() != raw {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	var result service.FrontendObservation
	result, err = h.service.FrontendObservation(c.Request.Context(), strconv.FormatUint(uint64(principal), 10), targetToken(c), raw)
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
