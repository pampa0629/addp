package api

import (
	"errors"
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type BaseMapProviderHandler struct {
	service *service.BaseMapProviderService
}

func NewBaseMapProviderHandler(value *service.BaseMapProviderService) *BaseMapProviderHandler {
	return &BaseMapProviderHandler{service: value}
}

func (h *BaseMapProviderHandler) context(c *gin.Context) (string, *uint, bool) {
	authContext, ok := commonAuth.AuthSessionContextFromGin(c)
	if !ok {
		return "", nil, false
	}
	if authContext.Type == "platform" {
		return "platform", nil, true
	}
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	if !ok {
		return "", nil, false
	}
	id := uint(tenantID)
	return "tenant", &id, true
}

// List godoc
// @Summary 列出底图服务配置 | List basemap provider configuration
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.read"]
// @Router /settings/base-map/providers [get]
func (h *BaseMapProviderHandler) List(c *gin.Context) {
	scope, tenantID, ok := h.context(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid configuration context"})
		return
	}
	values, err := h.service.List(c.Request.Context(), scope, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

// Update godoc
// @Summary 更新底图服务配置 | Update basemap provider configuration
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.UpdateBaseMapProviderInput true "底图服务配置 | Basemap provider configuration"
// @Success 200 {object} service.BaseMapProviderResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.update"]
// @Router /settings/base-map/providers [put]
func (h *BaseMapProviderHandler) Update(c *gin.Context) {
	scope, tenantID, ok := h.context(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid configuration context"})
		return
	}
	var input service.UpdateBaseMapProviderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	principalID, ok := commonAuth.PrincipalIDFromGin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	value, err := h.service.Update(c.Request.Context(), scope, tenantID, input, uint(principalID))
	if errors.Is(err, repository.ErrBaseMapProviderVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "base map provider version conflict"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, value)
}
