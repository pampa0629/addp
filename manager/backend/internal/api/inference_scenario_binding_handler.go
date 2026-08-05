package api

import (
	"errors"
	"net/http"

	commonauth "github.com/addp/common/middleware/auth"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type InferenceScenarioBindingHandler struct {
	service *service.InferenceScenarioBindingService
}

func NewInferenceScenarioBindingHandler(value *service.InferenceScenarioBindingService) *InferenceScenarioBindingHandler {
	return &InferenceScenarioBindingHandler{service: value}
}

// Get godoc
// @Summary 获取 Manager 推理场景绑定 | Get Manager inference scenario binding
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.InferenceScenarioBindingResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.read"]
// @Router /settings/inference-binding [get]
func (h *InferenceScenarioBindingHandler) Get(c *gin.Context) {
	scopeType, tenantID, ok := bindingScope(c)
	if !ok {
		managerError(c, http.StatusForbidden, manageri18n.MsgUnauthorized)
		return
	}
	response, err := h.service.Get(c.Request.Context(), scopeType, tenantID)
	if err != nil {
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgEmbeddingConfigurationLoadFailed, err.Error())
		return
	}
	c.JSON(http.StatusOK, response)
}

// Update godoc
// @Summary 更新 Manager 推理场景绑定 | Update Manager inference scenario binding
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.UpdateInferenceScenarioBindingInput true "Inference scenario binding"
// @Success 200 {object} service.InferenceScenarioBindingResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.update"]
// @Router /settings/inference-binding [put]
func (h *InferenceScenarioBindingHandler) Update(c *gin.Context) {
	var input service.UpdateInferenceScenarioBindingInput
	if err := c.ShouldBindJSON(&input); err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody, err.Error())
		return
	}
	scopeType, tenantID, ok := bindingScope(c)
	if !ok {
		managerError(c, http.StatusForbidden, manageri18n.MsgUnauthorized)
		return
	}
	principalID, exists := commonauth.PrincipalIDFromGin(c)
	if !exists {
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
		return
	}
	response, err := h.service.Update(c.Request.Context(), scopeType, tenantID, input, uint(principalID))
	if errors.Is(err, repository.ErrInferenceScenarioBindingVersionConflict) {
		managerError(c, http.StatusConflict, manageri18n.MsgEmbeddingConfigurationVersionConflict)
		return
	}
	if err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgEmbeddingConfigurationUpdateFailed, err.Error())
		return
	}
	c.JSON(http.StatusOK, response)
}

func bindingScope(c *gin.Context) (string, *uint, bool) {
	value, ok := commonauth.AuthContextFromGin(c)
	if !ok {
		return "", nil, false
	}
	if value.Context.Type == "platform" {
		return "platform", nil, true
	}
	if value.Context.Type == "tenant" {
		tenantID, exists := commonauth.TenantIDFromGin(c)
		if !exists || tenantID <= 0 {
			return "", nil, false
		}
		converted := uint(tenantID)
		return "tenant", &converted, true
	}
	return "", nil, false
}
