package api

import (
	"encoding/json"
	"errors"
	sharedauth "github.com/addp/common/middleware/auth"
	"io"
	"net/http"
	"strings"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// DeclareRuntimeMetrics godoc
// @Summary 续约内置运行时指标来源 | Renew a built-in runtime metrics source
// @Description 平台服务身份只能为自己的内置运行时和当前进程代次续租 | A platform runtime may renew only its own registered engine and process generation
// @Tags 运行时注册 | Runtime Registry
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎 ID | Engine ID"
// @Param request body commonmodels.RuntimeMetricsDeclaration true "指标来源 | Metrics declaration"
// @Success 204
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_metrics.update"]
// @Router /runtime/engines/{id}/metrics [put]
func (h *EngineHandler) DeclareRuntimeMetrics(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	var claim commonmodels.RuntimeMetricsDeclaration
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&claim) != nil || decoder.Decode(new(any)) != io.EOF {
		writeRuntimeMetricsError(c, http.StatusBadRequest, "invalid")
		return
	}
	switch err := h.engineService.DeclareRuntimeMetrics(c.Request.Context(), id, strings.TrimPrefix(sharedauth.GetClientID(c), "addp-"), claim); {
	case err == nil:
		c.Status(http.StatusNoContent)
	case errors.Is(err, service.ErrInvalidRuntimeMetrics):
		writeRuntimeMetricsError(c, http.StatusBadRequest, "invalid")
	case errors.Is(err, service.ErrRuntimeMetricsConflict):
		writeRuntimeMetricsError(c, http.StatusConflict, "conflict")
	default:
		writeRuntimeMetricsError(c, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeRuntimeMetricsError(c *gin.Context, status int, reason string) {
	c.JSON(status, models.ErrorResponse{
		Error: commoni18n.T(c, "system.runtime_metrics."+reason), ErrorCode: "runtime_metrics_" + reason,
	})
}
