package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	transferi18n "github.com/addp/transfer/i18n"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

type ContinuousPolicyHandler struct {
	service *service.ContinuousPolicyService
}

func NewContinuousPolicyHandler(value *service.ContinuousPolicyService) *ContinuousPolicyHandler {
	return &ContinuousPolicyHandler{service: value}
}

// Get godoc
// @Summary 获取持续同步策略 | Get continuous transfer policy
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.ContinuousPolicyResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.configuration.read"]
// @Router /settings/continuous-policy [get]
func (h *ContinuousPolicyHandler) Get(c *gin.Context) {
	value, err := h.service.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, transferi18n.MsgConfigurationLoadFailed)})
		return
	}
	c.JSON(http.StatusOK, value)
}

// Update godoc
// @Summary 更新持续同步策略 | Update continuous transfer policy
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.UpdateContinuousPolicyInput true "持续同步策略 | Continuous transfer policy"
// @Success 200 {object} service.ContinuousPolicyResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.configuration.update"]
// @Router /settings/continuous-policy [put]
func (h *ContinuousPolicyHandler) Update(c *gin.Context) {
	var input service.UpdateContinuousPolicyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, transferi18n.MsgConfigurationInvalid)})
		return
	}
	principalID, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, transferi18n.MsgConfigurationAuthentication)})
		return
	}
	value, err := h.service.Update(c.Request.Context(), input, uint(principalID))
	if errors.Is(err, repository.ErrContinuousPolicyVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, transferi18n.MsgConfigurationConflict)})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}
