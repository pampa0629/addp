package api

import (
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type MaterializationGroupHandler struct {
	service *service.MaterializationGroupService
}

func NewMaterializationGroupHandler(groupService *service.MaterializationGroupService) *MaterializationGroupHandler {
	return &MaterializationGroupHandler{service: groupService}
}

type materializationGroupWriteRequest struct {
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	LogicalTableIDs []int64 `json:"logical_table_ids"`
	Version         int64   `json:"version"`
}

type materializationGroupDeleteRequest struct {
	Version int64 `json:"version"`
}

type materializationGroupListResponse struct {
	Data       []models.MaterializationGroup `json:"data"`
	Total      int64                         `json:"total"`
	Page       int                           `json:"page"`
	PageSize   int                           `json:"page_size"`
	TotalPages int                           `json:"total_pages"`
}

// List godoc
// @Summary 列出物化组 | List materialization groups
// @Tags Materialization Groups
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} materializationGroupListResponse
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization_group.read"]
// @Router /materialization-groups [get]
// @Security BearerAuth
func (h *MaterializationGroupHandler) List(c *gin.Context) {
	page := boundedPositiveInt(c.Query("page"), 1, 1_000_000)
	pageSize := boundedPositiveInt(c.Query("page_size"), 20, 100)
	items, total, err := h.service.List(c.Request.Context(), getTenantID(c), page, pageSize)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	c.JSON(http.StatusOK, materializationGroupListResponse{Data: items, Total: total, Page: page, PageSize: pageSize, TotalPages: totalPages})
}

// Create godoc
// @Summary 创建物化组 | Create materialization group
// @Tags Materialization Groups
// @Accept json
// @Produce json
// @Param request body materializationGroupWriteRequest true "创建请求 | Create request"
// @Success 201 {object} models.MaterializationGroup
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization_group.create"]
// @Router /materialization-groups [post]
// @Security BearerAuth
func (h *MaterializationGroupHandler) Create(c *gin.Context) {
	var request materializationGroupWriteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.service.Create(c.Request.Context(), getTenantID(c), getUserID(c), groupWrite(request))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// Get godoc
// @Summary 获取物化组 | Get materialization group
// @Tags Materialization Groups
// @Produce json
// @Param id path int true "物化组 ID | Materialization group ID"
// @Success 200 {object} models.MaterializationGroup
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization_group.read"]
// @Router /materialization-groups/{id} [get]
// @Security BearerAuth
func (h *MaterializationGroupHandler) Get(c *gin.Context) {
	id, ok := materializationGroupID(c)
	if !ok {
		return
	}
	result, err := h.service.Get(c.Request.Context(), getTenantID(c), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Update godoc
// @Summary 更新物化组 | Update materialization group
// @Tags Materialization Groups
// @Accept json
// @Produce json
// @Param id path int true "物化组 ID | Materialization group ID"
// @Param request body materializationGroupWriteRequest true "完整更新请求 | Full update request"
// @Success 200 {object} models.MaterializationGroup
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization_group.update"]
// @Router /materialization-groups/{id} [put]
// @Security BearerAuth
func (h *MaterializationGroupHandler) Update(c *gin.Context) {
	id, ok := materializationGroupID(c)
	if !ok {
		return
	}
	var request materializationGroupWriteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	result, err := h.service.Update(c.Request.Context(), getTenantID(c), getUserID(c), id, groupWrite(request))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Delete godoc
// @Summary 删除物化组 | Delete materialization group
// @Tags Materialization Groups
// @Accept json
// @Produce json
// @Param id path int true "物化组 ID | Materialization group ID"
// @Param request body materializationGroupDeleteRequest true "删除请求 | Delete request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization_group.delete"]
// @Router /materialization-groups/{id} [delete]
// @Security BearerAuth
func (h *MaterializationGroupHandler) Delete(c *gin.Context) {
	id, ok := materializationGroupID(c)
	if !ok {
		return
	}
	var request materializationGroupDeleteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	if err := h.service.Delete(c.Request.Context(), getTenantID(c), id, request.Version); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func groupWrite(request materializationGroupWriteRequest) service.MaterializationGroupWrite {
	return service.MaterializationGroupWrite{Code: request.Code, Name: request.Name, Description: request.Description, LogicalTableIDs: request.LogicalTableIDs, Version: request.Version}
}

func materializationGroupID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return 0, false
	}
	return id, true
}
