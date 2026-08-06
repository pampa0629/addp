package api

import (
	"errors"
	"net/http"

	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type QuickViewPolicyHandler struct {
	service *service.QuickViewPolicyService
}

func NewQuickViewPolicyHandler(s *service.QuickViewPolicyService) *QuickViewPolicyHandler {
	return &QuickViewPolicyHandler{service: s}
}

// Get godoc
// @Summary 获取快显策略 | Get quick-view policy
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.QuickViewPolicyResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.read"]
// @Router /settings/quick-view-policy [get]
func (h *QuickViewPolicyHandler) Get(c *gin.Context) {
	value, err := h.service.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}

// Update godoc
// @Summary 更新快显策略 | Update quick-view policy
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.UpdateQuickViewPolicyInput true "快显策略 | Quick-view policy"
// @Success 200 {object} service.QuickViewPolicyResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.update"]
// @Router /settings/quick-view-policy [put]
func (h *QuickViewPolicyHandler) Update(c *gin.Context) {
	var input service.UpdateQuickViewPolicyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	principalID, ok := commonauth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	value, err := h.service.Update(c.Request.Context(), input, uint(principalID))
	if errors.Is(err, repository.ErrQuickViewPolicyVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "quick_view_policy_version_conflict"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}
