package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	servicei18n "github.com/addp/service/i18n"
	"github.com/addp/service/internal/repository"
	"github.com/addp/service/internal/service"
	"github.com/gin-gonic/gin"
)

type RuntimePolicyHandler struct{ service *service.RuntimePolicyService }

func NewRuntimePolicyHandler(value *service.RuntimePolicyService) *RuntimePolicyHandler {
	return &RuntimePolicyHandler{service: value}
}

// Get godoc
// @Summary 获取服务运行策略 | Get service runtime policy
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.RuntimePolicyResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.configuration.read"]
// @Router /settings/runtime-policy [get]
func (h *RuntimePolicyHandler) Get(c *gin.Context) {
	value, err := h.service.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, servicei18n.MsgConfigurationLoadFailed)})
		return
	}
	c.JSON(http.StatusOK, value)
}

// Update godoc
// @Summary 更新服务运行策略 | Update service runtime policy
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.UpdateRuntimePolicyInput true "服务运行策略 | Service runtime policy"
// @Success 200 {object} service.RuntimePolicyResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.configuration.update"]
// @Router /settings/runtime-policy [put]
func (h *RuntimePolicyHandler) Update(c *gin.Context) {
	var input service.UpdateRuntimePolicyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, servicei18n.MsgConfigurationInvalid)})
		return
	}
	principalID, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, servicei18n.MsgConfigurationAuthentication)})
		return
	}
	value, err := h.service.Update(c.Request.Context(), input, uint(principalID))
	if errors.Is(err, repository.ErrRuntimePolicyVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, servicei18n.MsgConfigurationConflict)})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}
