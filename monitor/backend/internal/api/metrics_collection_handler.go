package api

import (
	"net/http"
	"strconv"

	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// CollectionHealth godoc
// @Summary 读取采集中心健康摘要 | Read metrics collection health
// @Description 返回当前目标覆盖、发现异常与 TSDB 留存占用 | Return current target coverage, discovery failures and TSDB retention footprint
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.CollectionHealthObservation
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 429 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.resource_observation.read"]
// @Router /platform/metrics_collection_health [get]
func (h *ResourceObservationHandler) CollectionHealth(c *gin.Context) {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		resourceError(c, resourcequery.ErrInvalid)
		return
	}
	principal, ok := auth.PrincipalIDFromGin(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	var reading service.CollectionHealthObservation
	reading, err := h.service.CollectionHealth(c.Request.Context(), strconv.FormatUint(uint64(principal), 10))
	if err != nil {
		resourceError(c, err)
		return
	}
	c.JSON(http.StatusOK, reading)
}
