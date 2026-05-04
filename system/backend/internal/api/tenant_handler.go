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

// Create godoc
// @Summary      创建租户 | Create tenant
// @Tags         租户管理 | Tenant Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.TenantCreateRequest true "租户信息 | Tenant info"
// @Success      200 {object} models.Tenant
// @Failure      400 {object} models.ErrorResponse
// @Router       /tenants [post]
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

// List godoc
// @Summary      获取租户列表 | List tenants
// @Tags         租户管理 | Tenant Management
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number" default(1)
// @Param        page_size query int false "每页数量 | Page size" default(10)
// @Success      200 {object} object{data=[]models.Tenant,total=int,page=int,page_size=int}
// @Failure      500 {object} models.ErrorResponse
// @Router       /tenants [get]
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

// GetByID godoc
// @Summary      获取租户详情 | Get tenant detail
// @Tags         租户管理 | Tenant Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "租户ID | Tenant ID"
// @Success      200 {object} models.Tenant
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /tenants/{id} [get]
func (h *TenantHandler) GetByID(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	tenant, err := h.tenantService.GetByID(id, userID)
	commonapi.RespondOrError(c, tenant, err)
}

// Update godoc
// @Summary      更新租户 | Update tenant
// @Tags         租户管理 | Tenant Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "租户ID | Tenant ID"
// @Param        request body models.TenantUpdateRequest true "租户更新信息 | Tenant update info"
// @Success      200 {object} models.Tenant
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /tenants/{id} [put]
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

// Delete godoc
// @Summary      删除租户 | Delete tenant
// @Tags         租户管理 | Tenant Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "租户ID | Tenant ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /tenants/{id} [delete]
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
