package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type DWLayerHandler struct {
	svc *service.DWLayerService
}

func NewDWLayerHandler(svc *service.DWLayerService) *DWLayerHandler {
	return &DWLayerHandler{svc: svc}
}

// ListDWLayers GET /api/model/dw-layers
// @Summary 查询数仓分层列表 | List DW layers
// @Tags Model
// @Produce json
// @Success 200 {array} models.DWLayer "数仓分层列表 | DW layer list"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.dw_layer.read"]
// @Router /dw-layers [get]
// @Security BearerAuth
func (h *DWLayerHandler) ListDWLayers(c *gin.Context) {
	tenantID := getTenantID(c)

	layers, err := h.svc.ListDWLayers(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, operationFailedResponse(c))
		return
	}
	c.JSON(http.StatusOK, layers)
}

// CreateDWLayer POST /api/model/dw-layers
// @Summary 创建数仓分层 | Create DW layer
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.CreateDWLayerRequest true "创建请求 | Create request"
// @Success 201 {object} models.DWLayer "已创建的分层 | Created DW layer"
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 409 {object} models.ErrorResponse "分层编码冲突 | DW layer code conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.dw_layer.create"]
// @Router /dw-layers [post]
// @Security BearerAuth
func (h *DWLayerHandler) CreateDWLayer(c *gin.Context) {
	var req models.CreateDWLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	layer, err := h.svc.CreateDWLayer(&req, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, layer)
}

// GetDWLayer GET /api/model/dw-layers/:id
// @Summary 获取数仓分层详情 | Get DW layer details
// @Tags Model
// @Produce json
// @Param id path int true "分层ID | DW layer ID"
// @Success 200 {object} models.DWLayer "分层详情 | DW layer details"
// @Failure 400 {object} models.ErrorResponse "分层 ID 无效 | Invalid DW layer ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "数仓分层不存在 | DW layer not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.dw_layer.read"]
// @Router /dw-layers/{id} [get]
// @Security BearerAuth
func (h *DWLayerHandler) GetDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	layer, err := h.svc.GetDWLayer(id, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, layer)
}

// UpdateDWLayer PUT /api/model/dw-layers/:id
// @Summary 更新数仓分层 | Update DW layer
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "分层ID | DW layer ID"
// @Param body body models.UpdateDWLayerRequest true "更新请求 | Update request"
// @Success 200 {object} models.DWLayer "已更新的分层 | Updated DW layer"
// @Failure 400 {object} models.ErrorResponse "请求或分层 ID 无效 | Invalid request or DW layer ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "数仓分层不存在 | DW layer not found"
// @Failure 409 {object} models.ErrorResponse "分层编码冲突 | DW layer code conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.dw_layer.update"]
// @Router /dw-layers/{id} [put]
// @Security BearerAuth
func (h *DWLayerHandler) UpdateDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.UpdateDWLayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	layer, err := h.svc.UpdateDWLayer(id, tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, layer)
}

// DeleteDWLayer DELETE /api/model/dw-layers/:id
// @Summary 删除数仓分层 | Delete DW layer
// @Tags Model
// @Produce json
// @Param id path int true "分层ID | DW layer ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.MessageResponse "删除成功 | Deleted successfully"
// @Failure 400 {object} models.ErrorResponse "分层 ID 无效 | Invalid DW layer ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "数仓分层不存在 | DW layer not found"
// @Failure 409 {object} models.ErrorResponse "数仓分层仍被引用 | DW layer is still referenced"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.dw_layer.delete"]
// @Router /dw-layers/{id} [delete]
// @Security BearerAuth
func (h *DWLayerHandler) DeleteDWLayer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteDWLayer(id, tenantID, req.Version); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
