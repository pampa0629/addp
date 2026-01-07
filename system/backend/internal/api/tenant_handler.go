package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type TenantHandler struct {
	tenantService *service.TenantService
}

func NewTenantHandler(tenantService *service.TenantService) *TenantHandler {
	return &TenantHandler{tenantService: tenantService}
}

func (h *TenantHandler) Create(c *gin.Context) {
	var req models.TenantCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	tenant, err := h.tenantService.Create(&req, userID)
	commonapi.RespondOrError(c, tenant, err)
}

func (h *TenantHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	userID, _ := commonapi.GetCurrentUserID(c)

	tenants, total, err := h.tenantService.List(page, pageSize, userID)
	if err != nil {
		commonapi.RespondError(c, 500, err.Error())
		return
	}
	commonapi.RespondPaginated(c, tenants, total, page, pageSize)
}

func (h *TenantHandler) GetByID(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	tenant, err := h.tenantService.GetByID(id, userID)
	commonapi.RespondOrError(c, tenant, err)
}

func (h *TenantHandler) Update(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.TenantUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	tenant, err := h.tenantService.Update(id, &req, userID)
	commonapi.RespondOrError(c, tenant, err)
}

func (h *TenantHandler) Delete(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	err = h.tenantService.Delete(id, userID)
	if err != nil {
		commonapi.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	commonapi.RespondSuccess(c, gin.H{"message": "删除成功"})
}

