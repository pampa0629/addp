package api

import (
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type EntityHandler struct {
	svc *service.EntityService
}

func NewEntityHandler(svc *service.EntityService) *EntityHandler {
	return &EntityHandler{svc: svc}
}

// ListEntities GET /api/model/entities
// @Summary 查询实体列表 | List entities
// @Tags Model
// @Produce json
// @Param domain_id query int false "业务域ID | Domain ID"
// @Param status query string false "状态过滤 | Filter by status"
// @Param keyword query string false "关键词搜索 | Keyword search"
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} map[string]interface{} "实体列表 | Entity list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read"]
// @Router /entities [get]
// @Security BearerAuth
func (h *EntityHandler) ListEntities(c *gin.Context) {
	tenantID := getTenantID(c)

	opts := repository.ListEntityOptions{
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
	}
	if domainIDStr := c.Query("domain_id"); domainIDStr != "" {
		if id, err := strconv.ParseInt(domainIDStr, 10, 64); err == nil {
			opts.DomainID = &id
		}
	}
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil {
			opts.Page = p
		}
	}
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil {
			opts.PageSize = ps
		}
	}

	entities, total, err := h.svc.ListEntities(tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	totalPages := 0
	if opts.PageSize > 0 {
		totalPages = int(total) / opts.PageSize
		if int(total)%opts.PageSize != 0 {
			totalPages++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":        entities,
		"total":       total,
		"page":        opts.Page,
		"page_size":   opts.PageSize,
		"total_pages": totalPages,
	})
}

// CreateEntity POST /api/model/entities
// @Summary 创建实体 | Create entity
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.CreateEntityRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{} "已创建的实体 | Created entity"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.create"]
// @Router /entities [post]
// @Security BearerAuth
func (h *EntityHandler) CreateEntity(c *gin.Context) {
	var req models.CreateEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	entity, err := h.svc.CreateEntity(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, entity)
}

// GetEntity GET /api/model/entities/:id
// @Summary 获取实体详情 | Get entity details
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Success 200 {object} map[string]interface{} "实体详情 | Entity details"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read"]
// @Router /entities/{id} [get]
// @Security BearerAuth
func (h *EntityHandler) GetEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	entity, err := h.svc.GetEntity(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, modeli18n.MsgEntityNotFound)})
		return
	}
	c.JSON(http.StatusOK, entity)
}

// UpdateEntity PUT /api/model/entities/:id
// @Summary 更新实体 | Update entity
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param body body models.UpdateEntityRequest true "更新请求 | Update request"
// @Success 200 {object} map[string]interface{} "已更新的实体 | Updated entity"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.update"]
// @Router /entities/{id} [put]
// @Security BearerAuth
func (h *EntityHandler) UpdateEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	var req models.UpdateEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	entity, err := h.svc.UpdateEntity(id, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entity)
}

// DeleteEntity DELETE /api/model/entities/:id
// @Summary 删除实体 | Delete entity
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.delete"]
// @Router /entities/{id} [delete]
// @Security BearerAuth
func (h *EntityHandler) DeleteEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteEntity(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ApproveEntity POST /api/model/entities/:id/approve
// @Summary 审批通过实体 | Approve entity
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Success 200 {object} map[string]interface{} "审批成功 | Approved successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.approve"]
// @Router /entities/{id}/approve [post]
// @Security BearerAuth
func (h *EntityHandler) ApproveEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	if err := h.svc.ApproveEntity(id, tenantID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "approved"})
}

// GetAttributes GET /api/model/entities/:id/attributes
// @Summary 获取实体属性列表 | Get entity attributes
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Success 200 {object} map[string]interface{} "属性列表 | Attribute list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read"]
// @Router /entities/{id}/attributes [get]
// @Security BearerAuth
func (h *EntityHandler) GetAttributes(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	attrs, err := h.svc.GetAttributes(entityID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, attrs)
}

// CreateAttribute POST /api/model/entities/:id/attributes
// @Summary 创建实体属性 | Create entity attribute
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param body body models.CreateEntityAttributeRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{} "已创建的属性 | Created attribute"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.create"]
// @Router /entities/{id}/attributes [post]
// @Security BearerAuth
func (h *EntityHandler) CreateAttribute(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	var req models.CreateEntityAttributeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	attr, err := h.svc.CreateAttribute(entityID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, attr)
}

// UpdateAttribute PUT /api/model/entities/:id/attributes/:aid
// @Summary 更新实体属性 | Update entity attribute
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param aid path int true "属性ID | Attribute ID"
// @Param body body models.UpdateEntityAttributeRequest true "更新请求 | Update request"
// @Success 200 {object} map[string]interface{} "已更新的属性 | Updated attribute"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.update"]
// @Router /entities/{id}/attributes/{aid} [put]
// @Security BearerAuth
func (h *EntityHandler) UpdateAttribute(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}
	attrID, err := strconv.ParseInt(c.Param("aid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidAttributeID)})
		return
	}

	var req models.UpdateEntityAttributeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	attr, err := h.svc.UpdateAttribute(attrID, entityID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, attr)
}

// DeleteAttribute DELETE /api/model/entities/:id/attributes/:aid
// @Summary 删除实体属性 | Delete entity attribute
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param aid path int true "属性ID | Attribute ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.delete"]
// @Router /entities/{id}/attributes/{aid} [delete]
// @Security BearerAuth
func (h *EntityHandler) DeleteAttribute(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}
	attrID, err := strconv.ParseInt(c.Param("aid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidAttributeID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteAttribute(attrID, entityID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ImportMermaid POST /api/model/entities/import-mermaid
// @Summary 从 Mermaid ER 图导入实体 | Import entities from Mermaid ER diagram
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.MermaidImportRequest true "导入请求 | Import request"
// @Success 200 {object} map[string]interface{} "导入结果 | Import result"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.create","model.entity_relation.create"]
// @Router /entities/import-mermaid [post]
// @Security BearerAuth
func (h *EntityHandler) ImportMermaid(c *gin.Context) {
	var req models.MermaidImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	result, err := h.svc.ImportFromMermaid(tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ExportMermaid GET /api/model/entities/export-mermaid
// @Summary 导出 Mermaid ER 图 | Export Mermaid ER diagram
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{} "Mermaid ER 图代码 | Mermaid ER diagram code"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read","model.entity_relation.read"]
// @Router /entities/export-mermaid [get]
// @Security BearerAuth
func (h *EntityHandler) ExportMermaid(c *gin.Context) {
	tenantID := getTenantID(c)

	mermaidCode, err := h.svc.ExportToMermaid(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 设置响应头，支持文件下载
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=er-diagram.mmd")
	c.String(http.StatusOK, mermaidCode)
}
