package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

// InstanceOverview godoc
// @Summary 获取原生实例概况 | Read native instance overview
// @Description 使用已保存连接和现有账号按需读取有范围的原生统计，不调用Monitor、不改变连接状态，不接受query或正文；整数值为精确十进制字符串，缺值不返回value | Read scoped native statistics using saved connection credentials without Monitor or health mutations; no query or body; integer values are exact decimal strings and unavailable values are omitted
// @Tags 引擎管理 | Engine Management
// @Produce json
// @Security BearerAuth
// @Param id path int true "引擎ID | Engine ID"
// @Success 200 {object} models.EngineInstanceOverviewResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 422 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.read"]
// @Router /engines/{id}/overview [get]
func (h *EngineHandler) InstanceOverview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		commonapi.RespondError(c, http.StatusBadRequest, commoni18n.T(c, commoni18n.MsgInvalidParams))
		return
	}
	_, tenantID, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var result *models.EngineInstanceOverviewResponse
	result, err = h.engineService.ReadInstanceOverview(c.Request.Context(), id, tenantID)
	switch {
	case errors.Is(err, service.ErrInstanceOverviewUnsupported):
		commonapi.RespondError(c, http.StatusUnprocessableEntity, commoni18n.T(c, sysi18n.MsgInstanceOverviewUnsupported))
	case errors.Is(err, service.ErrInstanceOverviewUnavailable):
		commonapi.RespondError(c, http.StatusServiceUnavailable, commoni18n.T(c, sysi18n.MsgInstanceOverviewUnavailable))
	case err != nil:
		h.respondWithResourceError(c, err)
	default:
		commonapi.RespondSuccess(c, result)
	}
}
