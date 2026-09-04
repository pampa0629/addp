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

type DimensionHierarchyHandler struct {
	svc *service.DimensionHierarchyService
}

func NewDimensionHierarchyHandler(svc *service.DimensionHierarchyService) *DimensionHierarchyHandler {
	return &DimensionHierarchyHandler{svc: svc}
}

// List GET /api/v1/model/logical-tables/:id/dimension-hierarchies
// @Summary 查询维度层级 | List dimension hierarchies
// @Description 查询一个维度逻辑表内定义的全部下钻层级 | List all drill-down hierarchies defined in a dimension logical table
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID" minimum(1)
// @Success 200 {array} models.DimensionHierarchy "维度层级列表 | Dimension hierarchy list"
// @Failure 400 {object} models.ErrorResponse "逻辑表ID或类型无效 | Invalid logical table ID or type"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/dimension-hierarchies [get]
// @Security BearerAuth
func (h *DimensionHierarchyHandler) List(c *gin.Context) {
	tableID, ok := positivePathID(c, "id")
	if !ok {
		return
	}
	items, err := h.svc.List(tableID, getTenantID(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// Create POST /api/v1/model/logical-tables/:id/dimension-hierarchies
// @Summary 创建维度层级 | Create dimension hierarchy
// @Description 在维度逻辑表聚合内创建下钻层级并推进父资源版本 | Create a drill-down hierarchy inside a dimension logical-table aggregate and advance the parent version
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID" minimum(1)
// @Param body body models.CreateDimensionHierarchyRequest true "创建请求 | Create request"
// @Success 201 {object} models.DimensionHierarchyMutationResponse "创建结果与父资源新版本 | Created hierarchy and new parent version"
// @Failure 400 {object} models.ErrorResponse "请求或逻辑表类型无效 | Invalid request or logical table type"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @Failure 409 {object} models.ErrorResponse "父资源版本、状态或层级名称冲突 | Parent version, state, or hierarchy name conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-hierarchies [post]
// @Security BearerAuth
func (h *DimensionHierarchyHandler) Create(c *gin.Context) {
	tableID, ok := positivePathID(c, "id")
	if !ok {
		return
	}
	var req models.CreateDimensionHierarchyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.Create(tableID, getTenantID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// Update PUT /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid
// @Summary 更新维度层级 | Update dimension hierarchy
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID" minimum(1)
// @Param hid path int true "维度层级ID | Dimension hierarchy ID" minimum(1)
// @Param body body models.UpdateDimensionHierarchyRequest true "更新请求 | Update request"
// @Success 200 {object} models.DimensionHierarchyMutationResponse "更新结果与父资源新版本 | Updated hierarchy and new parent version"
// @Failure 400 {object} models.ErrorResponse "请求或ID无效 | Invalid request or ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表或维度层级不存在 | Logical table or hierarchy not found"
// @Failure 409 {object} models.ErrorResponse "父资源版本、状态或层级名称冲突 | Parent version, state, or hierarchy name conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-hierarchies/{hid} [put]
// @Security BearerAuth
func (h *DimensionHierarchyHandler) Update(c *gin.Context) {
	tableID, ok := positivePathID(c, "id")
	if !ok {
		return
	}
	hierarchyID, ok := positivePathID(c, "hid")
	if !ok {
		return
	}
	var req models.UpdateDimensionHierarchyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.Update(hierarchyID, tableID, getTenantID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Delete DELETE /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid
// @Summary 删除维度层级 | Delete dimension hierarchy
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID" minimum(1)
// @Param hid path int true "维度层级ID | Dimension hierarchy ID" minimum(1)
// @Param body body models.VersionRequest true "父资源版本 | Parent resource version"
// @Success 200 {object} models.VersionResponse "父资源新版本 | New parent version"
// @Failure 400 {object} models.ErrorResponse "请求或ID无效 | Invalid request or ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表或维度层级不存在 | Logical table or hierarchy not found"
// @Failure 409 {object} models.ErrorResponse "父资源版本或状态冲突 | Parent version or state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-hierarchies/{hid} [delete]
// @Security BearerAuth
func (h *DimensionHierarchyHandler) Delete(c *gin.Context) {
	tableID, ok := positivePathID(c, "id")
	if !ok {
		return
	}
	hierarchyID, ok := positivePathID(c, "hid")
	if !ok {
		return
	}
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.Delete(hierarchyID, tableID, getTenantID(c), req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateLevel POST /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid/levels
// @Summary 创建维度层级成员 | Create dimension hierarchy level
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID" minimum(1)
// @Param hid path int true "维度层级ID | Dimension hierarchy ID" minimum(1)
// @Param body body models.UpsertDimensionHierarchyLevelRequest true "层级成员 | Hierarchy level"
// @Success 201 {object} models.DimensionHierarchyLevelMutationResponse "创建结果与父资源新版本 | Created level and new parent version"
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表、维度层级或字段不存在 | Logical table, hierarchy, or field not found"
// @Failure 409 {object} models.ErrorResponse "父资源版本、状态、层级序号或字段冲突 | Parent version, state, level number, or field conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-hierarchies/{hid}/levels [post]
// @Security BearerAuth
func (h *DimensionHierarchyHandler) CreateLevel(c *gin.Context) {
	tableID, hierarchyID, ok := hierarchyPathIDs(c)
	if !ok {
		return
	}
	var req models.UpsertDimensionHierarchyLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.CreateLevel(hierarchyID, tableID, getTenantID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateLevel PUT /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid/levels/:lid
// @Summary 更新维度层级成员 | Update dimension hierarchy level
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID" minimum(1)
// @Param hid path int true "维度层级ID | Dimension hierarchy ID" minimum(1)
// @Param lid path int true "层级成员ID | Hierarchy level ID" minimum(1)
// @Param body body models.UpsertDimensionHierarchyLevelRequest true "层级成员 | Hierarchy level"
// @Success 200 {object} models.DimensionHierarchyLevelMutationResponse "更新结果与父资源新版本 | Updated level and new parent version"
// @Failure 400 {object} models.ErrorResponse "请求或ID无效 | Invalid request or ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表、维度层级、成员或字段不存在 | Logical table, hierarchy, level, or field not found"
// @Failure 409 {object} models.ErrorResponse "父资源版本、状态、层级序号或字段冲突 | Parent version, state, level number, or field conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-hierarchies/{hid}/levels/{lid} [put]
// @Security BearerAuth
func (h *DimensionHierarchyHandler) UpdateLevel(c *gin.Context) {
	tableID, hierarchyID, ok := hierarchyPathIDs(c)
	if !ok {
		return
	}
	levelID, ok := positivePathID(c, "lid")
	if !ok {
		return
	}
	var req models.UpsertDimensionHierarchyLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.UpdateLevel(levelID, hierarchyID, tableID, getTenantID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteLevel DELETE /api/v1/model/logical-tables/:id/dimension-hierarchies/:hid/levels/:lid
// @Summary 删除维度层级成员 | Delete dimension hierarchy level
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID" minimum(1)
// @Param hid path int true "维度层级ID | Dimension hierarchy ID" minimum(1)
// @Param lid path int true "层级成员ID | Hierarchy level ID" minimum(1)
// @Param body body models.VersionRequest true "父资源版本 | Parent resource version"
// @Success 200 {object} models.VersionResponse "父资源新版本 | New parent version"
// @Failure 400 {object} models.ErrorResponse "请求或ID无效 | Invalid request or ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表、维度层级或成员不存在 | Logical table, hierarchy, or level not found"
// @Failure 409 {object} models.ErrorResponse "父资源版本或状态冲突 | Parent version or state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/dimension-hierarchies/{hid}/levels/{lid} [delete]
// @Security BearerAuth
func (h *DimensionHierarchyHandler) DeleteLevel(c *gin.Context) {
	tableID, hierarchyID, ok := hierarchyPathIDs(c)
	if !ok {
		return
	}
	levelID, ok := positivePathID(c, "lid")
	if !ok {
		return
	}
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.svc.DeleteLevel(levelID, hierarchyID, tableID, getTenantID(c), req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func positivePathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return 0, false
	}
	return id, true
}

func hierarchyPathIDs(c *gin.Context) (int64, int64, bool) {
	tableID, ok := positivePathID(c, "id")
	if !ok {
		return 0, 0, false
	}
	hierarchyID, ok := positivePathID(c, "hid")
	return tableID, hierarchyID, ok
}
