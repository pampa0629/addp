package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// DeclareGeoPythonMetrics godoc
// @Summary 续约 GeoPython 指标来源 | Renew the GeoPython metrics source
// @Description 限 GeoPython 的平台服务身份，当前内置引擎注册与进程代次须匹配 | The GeoPython platform service renews a private source for its registered engine and current process
// @Tags 运行时注册 | Runtime Registry
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎 ID | Engine ID"
// @Param request body commonmodels.GeoPythonMetricsDeclaration true "指标来源 | Metrics declaration"
// @Success 204
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router /runtime/engines/{id}/metrics [put]
func (h *EngineHandler) DeclareGeoPythonMetrics(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	var claim commonmodels.GeoPythonMetricsDeclaration
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&claim) != nil || decoder.Decode(new(any)) != io.EOF {
		writeGeoPythonMetricsError(c, http.StatusBadRequest, "invalid")
		return
	}
	switch err := h.engineService.DeclareGeoPythonMetrics(c.Request.Context(), id, claim); {
	case err == nil:
		c.Status(http.StatusNoContent)
	case errors.Is(err, service.ErrInvalidGeoPythonMetrics):
		writeGeoPythonMetricsError(c, http.StatusBadRequest, "invalid")
	case errors.Is(err, service.ErrGeoPythonMetricsConflict):
		writeGeoPythonMetricsError(c, http.StatusConflict, "conflict")
	default:
		writeGeoPythonMetricsError(c, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeGeoPythonMetricsError(c *gin.Context, status int, reason string) {
	c.JSON(status, models.ErrorResponse{
		Error: commoni18n.T(c, "system.geopython_metrics."+reason), ErrorCode: "geopython_metrics_" + reason,
	})
}
