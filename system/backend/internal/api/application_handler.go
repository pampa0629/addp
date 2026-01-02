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
// POST /api/applications
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
// GET /api/applications/:id
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
// GET /api/applications
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
// PUT /api/applications/:id
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
// DELETE /api/applications/:id
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
// POST /api/applications/:id/keys
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
// GET /api/applications/:id/keys
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
// DELETE /api/applications/:id/keys/:key_id
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

