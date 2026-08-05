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

type EmbeddingConfigurationHandler struct {
	service *service.EmbeddingConfigurationService
}

func NewEmbeddingConfigurationHandler(service *service.EmbeddingConfigurationService) *EmbeddingConfigurationHandler {
	return &EmbeddingConfigurationHandler{service: service}
}

// Get godoc
// @Summary 获取平台向量化配置 | Get platform embedding configuration
// @Tags 配置管理 | Configuration Management
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.EmbeddingConfigurationResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.read"]
// @Router /settings/embedding [get]
func (h *EmbeddingConfigurationHandler) Get(c *gin.Context) {
	response, err := h.service.Get(c.Request.Context())
	if err != nil {
		managerError(c, http.StatusInternalServerError, manageri18n.MsgEmbeddingConfigurationLoadFailed)
		return
	}
	c.JSON(http.StatusOK, response)
}

// Update godoc
// @Summary 更新平台向量化配置 | Update platform embedding configuration
// @Tags 配置管理 | Configuration Management
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body service.UpdateEmbeddingConfigurationInput true "向量化配置 | Embedding configuration"
// @Success 200 {object} service.EmbeddingConfigurationResponse
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.configuration.update"]
// @Router /settings/embedding [put]
func (h *EmbeddingConfigurationHandler) Update(c *gin.Context) {
	var input service.UpdateEmbeddingConfigurationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody, err.Error())
		return
	}
	principalID, exists := commonauth.PrincipalIDFromGin(c)
	if !exists {
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
		return
	}
	response, err := h.service.Update(c.Request.Context(), input, uint(principalID))
	if errors.Is(err, repository.ErrEmbeddingConfigurationVersionConflict) {
		managerError(c, http.StatusConflict, manageri18n.MsgEmbeddingConfigurationVersionConflict)
		return
	}
	if err != nil {
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgEmbeddingConfigurationUpdateFailed, err.Error())
		return
	}
	c.JSON(http.StatusOK, response)
}
