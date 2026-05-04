package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type ApplicationHandler struct {
	service *service.ApplicationService
}

func NewApplicationHandler(service *service.ApplicationService) *ApplicationHandler {
	return &ApplicationHandler{service: service}
}

// CreateApplication 创建应用
// @Summary      创建应用 | Create application
// @Tags         应用管理 | Application Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.CreateApplicationRequest true "应用信息 | Application info"
// @Success      200 {object} models.Application
// @Failure      400 {object} models.ErrorResponse
// @Router       /applications [post]
func (h *ApplicationHandler) CreateApplication(c *gin.Context) {
	var req models.CreateApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 从上下文获取用户信息
	userID, _ := commonapi.GetCurrentUserID(c)
	tenantID, _ := c.Get("tenant_id")

	app, err := h.service.CreateApplication(&req, tenantID.(uint), userID)
	commonapi.RespondOrError(c, app, err)
}

// GetApplication 获取应用详情
// @Summary      获取应用详情 | Get application detail
// @Tags         应用管理 | Application Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "应用ID | Application ID"
// @Success      200 {object} models.Application
// @Failure      404 {object} models.ErrorResponse
// @Router       /applications/{id} [get]
func (h *ApplicationHandler) GetApplication(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	app, err := h.service.GetApplication(id)
	if err != nil {
		commonapi.RespondError(c, http.StatusNotFound, "Application not found")
		return
	}

	commonapi.RespondSuccess(c, app)
}

// ListApplications 列出应用
// @Summary      获取应用列表 | List applications
// @Tags         应用管理 | Application Management
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @Router       /applications [get]
func (h *ApplicationHandler) ListApplications(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")

	apps, err := h.service.ListApplications(tenantID.(uint))
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{
		"applications": apps,
		"total":        len(apps),
	})
}

// UpdateApplication 更新应用
// @Summary      更新应用 | Update application
// @Tags         应用管理 | Application Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "应用ID | Application ID"
// @Param        request body models.UpdateApplicationRequest true "应用更新信息 | Application update info"
// @Success      200 {object} models.Application
// @Failure      400 {object} models.ErrorResponse
// @Router       /applications/{id} [put]
func (h *ApplicationHandler) UpdateApplication(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.UpdateApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	app, err := h.service.UpdateApplication(id, &req)
	commonapi.RespondOrError(c, app, err)
}

// DeleteApplication 删除应用
// @Summary      删除应用 | Delete application
// @Tags         应用管理 | Application Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "应用ID | Application ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /applications/{id} [delete]
func (h *ApplicationHandler) DeleteApplication(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	err = h.service.DeleteApplication(id)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{"message": "Application deleted successfully"})
}

// GenerateAPIKey 为应用生成 API Key
// @Summary      生成 API Key | Generate API key
// @Tags         应用管理 | Application Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "应用ID | Application ID"
// @Param        request body models.CreateAPIKeyRequest true "API Key 信息 | API key info"
// @Success      200 {object} models.APIKey
// @Failure      400 {object} models.ErrorResponse
// @Router       /applications/{id}/keys [post]
func (h *ApplicationHandler) GenerateAPIKey(c *gin.Context) {
	appID, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	apiKey, err := h.service.GenerateAPIKey(appID, &req, userID)
	commonapi.RespondOrError(c, apiKey, err)
}

// ListAPIKeys 列出应用的 API Keys
// @Summary      获取 API Key 列表 | List API keys
// @Tags         应用管理 | Application Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "应用ID | Application ID"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @Router       /applications/{id}/keys [get]
func (h *ApplicationHandler) ListAPIKeys(c *gin.Context) {
	appID, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	keys, err := h.service.ListAPIKeys(appID)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{
		"keys":  keys,
		"total": len(keys),
	})
}

// RevokeAPIKey 撤销 API Key
// @Summary      撤销 API Key | Revoke API key
// @Tags         应用管理 | Application Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "应用ID | Application ID"
// @Param        key_id path int true "API Key ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /applications/{id}/keys/{key_id} [delete]
func (h *ApplicationHandler) RevokeAPIKey(c *gin.Context) {
	appID, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	keyID, err := commonapi.BindIDParam(c, "key_id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	err = h.service.RevokeAPIKey(appID, keyID, userID)
	if err != nil {
		commonapi.RespondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{"message": "API key revoked successfully"})
}
