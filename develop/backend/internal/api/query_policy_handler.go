package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/repository"
	developService "github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

type QueryPolicyHandler struct {
	service *developService.QueryPolicyService
}

func NewQueryPolicyHandler(s *developService.QueryPolicyService) *QueryPolicyHandler {
	return &QueryPolicyHandler{service: s}
}

// Get godoc
// @Summary 获取查询策略 | Get query policy
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} developService.QueryPolicyResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.configuration.read"]
// @Router /settings/query-policy [get]
func (h *QueryPolicyHandler) Get(c *gin.Context) {
	scope, tenantID, ok := queryPolicyScope(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "unsupported authorization context"})
		return
	}
	value, err := h.service.Get(c.Request.Context(), scope, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, modulei18n.MsgQueryPolicyInvalid, err.Error())})
		return
	}
	c.JSON(http.StatusOK, value)
}

// Update godoc
// @Summary 更新查询策略 | Update query policy
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Description 平台维护并发、超时和预览策略；租户仅可覆盖默认超时。保存后并发热更新，新执行冻结超时和预览上限。 | Platform manages concurrency, timeout and preview limits; tenants override only the default timeout. Concurrency reloads without restart; new executions freeze their timeout and preview limits.
// @Param request body developService.UpdateQueryPolicyInput true "查询策略 | Query policy"
// @Success 200 {object} developService.QueryPolicyResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.configuration.update"]
// @Router /settings/query-policy [put]
func (h *QueryPolicyHandler) Update(c *gin.Context) {
	var input developService.UpdateQueryPolicyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.TWithDetail(c, modulei18n.MsgQueryPolicyInvalid, err.Error())})
		return
	}
	scope, tenantID, ok := queryPolicyScope(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "unsupported authorization context"})
		return
	}
	principalID, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	value, err := h.service.Update(c.Request.Context(), scope, tenantID, input, uint(principalID))
	if errors.Is(err, repository.ErrQueryPolicyVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgQueryPolicyConflict), "error_code": "query_policy_version_conflict"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.TWithDetail(c, modulei18n.MsgQueryPolicyInvalid, err.Error())})
		return
	}
	c.JSON(http.StatusOK, value)
}

func queryPolicyScope(c *gin.Context) (string, *uint, bool) {
	ctx, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		return "", nil, false
	}
	if ctx.Context.Type == "platform" {
		return "platform", nil, true
	}
	if ctx.Context.Type != "tenant" {
		return "", nil, false
	}
	id, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		return "", nil, false
	}
	v := uint(id)
	return "tenant", &v, true
}
