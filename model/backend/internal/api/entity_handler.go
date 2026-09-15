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

// ListEntities GET /api/v1/model/entities
// @Summary 查询实体列表 | List entities
// @Tags Model
// @Produce json
// @Param domain_id query int false "业务域ID | Domain ID" minimum(1)
// @Param status query string false "状态过滤 | Filter by status" Enums(draft, approved)
// @Param keyword query string false "关键词搜索 | Keyword search"
// @Param page query int false "页码 | Page number" default(1) minimum(1)
// @Param page_size query int false "每页数量 | Page size" default(20) minimum(1) maximum(100)
// @Success 200 {object} models.EntityListResponse "实体列表 | Entity list"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "查询参数无效 | Invalid query parameters"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read"]
// @Router /entities [get]
// @Security BearerAuth
func (h *EntityHandler) ListEntities(c *gin.Context) {
	tenantID := getTenantID(c)
	query, err := parseModelListQuery(c.Request.URL.Query())
	if err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	status, err := parseOptionalEnum(c.Request.URL.Query()["status"], "draft", "approved")
	if err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	keyword, err := parseOptionalString(c.Request.URL.Query()["keyword"])
	if err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	opts := repository.ListEntityOptions{
		DomainID: query.DomainID,
		Status:   status,
		Keyword:  keyword,
		Page:     query.Page,
		PageSize: query.PageSize,
	}

	entities, total, err := h.svc.ListEntities(tenantID, opts)
	if err != nil {
		writeServiceError(c, err)
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

// CreateEntity POST /api/v1/model/entities
// @Summary 创建实体 | Create entity
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.CreateEntityRequest true "创建请求 | Create request"
// @Success 201 {object} models.Entity "已创建的实体 | Created entity"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 404 {object} models.ErrorResponse "引用的业务域不存在 | Referenced business domain not found"
// @Failure 409 {object} models.ErrorResponse "实体编码冲突 | Entity code conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.create"]
// @Router /entities [post]
// @Security BearerAuth
func (h *EntityHandler) CreateEntity(c *gin.Context) {
	var req models.CreateEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	entity, err := h.svc.CreateEntity(&req, tenantID, userID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, entity)
}

// GetEntity GET /api/v1/model/entities/:id
// @Summary 获取实体详情 | Get entity details
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Success 200 {object} models.Entity "实体详情 | Entity details"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "实体 ID 无效 | Invalid entity ID"
// @Failure 404 {object} models.ErrorResponse "实体不存在 | Entity not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read"]
// @Router /entities/{id} [get]
// @Security BearerAuth
func (h *EntityHandler) GetEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	entity, err := h.svc.GetEntity(id, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, entity)
}

// UpdateEntity PUT /api/v1/model/entities/:id
// @Summary 更新实体 | Update entity
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param body body models.UpdateEntityRequest true "更新请求 | Update request"
// @Success 200 {object} models.Entity "已更新的实体 | Updated entity"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "请求或实体 ID 无效 | Invalid request or entity ID"
// @Failure 404 {object} models.ErrorResponse "实体不存在 | Entity not found"
// @Failure 409 {object} models.ErrorResponse "实体状态或属性列名冲突 | Entity state or attribute column name conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.update"]
// @Router /entities/{id} [put]
// @Security BearerAuth
func (h *EntityHandler) UpdateEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.UpdateEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	entity, err := h.svc.UpdateEntity(id, tenantID, userID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, entity)
}

// DeleteEntity DELETE /api/v1/model/entities/:id
// @Summary 删除实体 | Delete entity
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.MessageResponse "删除成功 | Deleted successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "实体 ID 无效 | Invalid entity ID"
// @Failure 404 {object} models.ErrorResponse "实体不存在 | Entity not found"
// @Failure 409 {object} models.ErrorResponse "实体状态冲突 | Entity state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.delete"]
// @Router /entities/{id} [delete]
// @Security BearerAuth
func (h *EntityHandler) DeleteEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	if err := h.svc.DeleteEntity(id, tenantID, req.Version); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ApproveEntity POST /api/v1/model/entities/:id/approve
// @Summary 审批通过实体 | Approve entity
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.Entity "审批成功 | Approved successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "实体缺少属性、主键或属性定义不完整 | Entity has no attributes, no primary key, or an incomplete attribute definition"
// @Failure 404 {object} models.ErrorResponse "实体不存在 | Entity not found"
// @Failure 409 {object} models.ErrorResponse "实体状态冲突 | Entity state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.approve"]
// @Router /entities/{id}/approve [post]
// @Security BearerAuth
func (h *EntityHandler) ApproveEntity(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	entity, err := h.svc.ApproveEntity(id, tenantID, userID, req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, entity)
}

// ReopenEntity POST /api/v1/model/entities/:id/reopen
// @Summary 将实体退回草稿 | Return entity to draft
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.Entity "退回草稿成功 | Returned to draft successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "实体 ID 无效 | Invalid entity ID"
// @Failure 404 {object} models.ErrorResponse "实体不存在 | Entity not found"
// @Failure 409 {object} models.ErrorResponse "实体状态冲突 | Entity state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.update"]
// @Router /entities/{id}/reopen [post]
// @Security BearerAuth
func (h *EntityHandler) ReopenEntity(c *gin.Context) {
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
	entity, err := h.svc.ReopenEntity(id, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, entity)
}

// GetAttributes GET /api/v1/model/entities/:id/attributes
// @Summary 获取实体属性列表 | Get entity attributes
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Success 200 {array} models.EntityAttribute "属性列表 | Attribute list"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "实体 ID 无效 | Invalid entity ID"
// @Failure 404 {object} models.ErrorResponse "实体不存在 | Entity not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read"]
// @Router /entities/{id}/attributes [get]
// @Security BearerAuth
func (h *EntityHandler) GetAttributes(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || entityID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	attrs, err := h.svc.GetAttributes(entityID, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, attrs)
}

// CreateAttribute POST /api/v1/model/entities/:id/attributes
// @Summary 创建实体属性 | Create entity attribute
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param body body models.CreateEntityAttributeRequest true "创建请求 | Create request"
// @Success 201 {object} models.EntityAttributeMutationResponse "已创建的属性 | Created attribute"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "请求或实体 ID 无效 | Invalid request or entity ID"
// @Failure 404 {object} models.ErrorResponse "实体或引用资源不存在 | Entity or referenced resource not found"
// @Failure 409 {object} models.ErrorResponse "实体状态冲突 | Entity state conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.create"]
// @Router /entities/{id}/attributes [post]
// @Security BearerAuth
func (h *EntityHandler) CreateAttribute(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || entityID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.CreateEntityAttributeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	attr, err := h.svc.CreateAttribute(entityID, tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, attr)
}

// UpdateAttribute PUT /api/v1/model/entities/:id/attributes/:aid
// @Summary 更新实体属性 | Update entity attribute
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param aid path int true "属性ID | Attribute ID"
// @Param body body models.UpdateEntityAttributeRequest true "更新请求 | Update request"
// @Success 200 {object} models.EntityAttributeMutationResponse "已更新的属性 | Updated attribute"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "请求、实体或属性 ID 无效 | Invalid request, entity, or attribute ID"
// @Failure 404 {object} models.ErrorResponse "实体或属性不存在 | Entity or attribute not found"
// @Failure 409 {object} models.ErrorResponse "实体状态或属性列名冲突 | Entity state or attribute column name conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.update"]
// @Router /entities/{id}/attributes/{aid} [put]
// @Security BearerAuth
func (h *EntityHandler) UpdateAttribute(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || entityID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	attrID, err := strconv.ParseInt(c.Param("aid"), 10, 64)
	if err != nil || attrID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidAttributeID), "invalid_attribute_id"))
		return
	}

	var req models.UpdateEntityAttributeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	attr, err := h.svc.UpdateAttribute(attrID, entityID, tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, attr)
}

// DeleteAttribute DELETE /api/v1/model/entities/:id/attributes/:aid
// @Summary 删除实体属性 | Delete entity attribute
// @Tags Model
// @Produce json
// @Param id path int true "实体ID | Entity ID"
// @Param aid path int true "属性ID | Attribute ID"
// @Param body body models.VersionRequest true "父资源版本 | Parent resource version"
// @Success 200 {object} models.VersionResponse "删除成功 | Deleted successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "实体或属性 ID 无效 | Invalid entity or attribute ID"
// @Failure 404 {object} models.ErrorResponse "实体或属性不存在 | Entity or attribute not found"
// @Failure 409 {object} models.ErrorResponse "实体状态冲突 | Entity state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.delete"]
// @Router /entities/{id}/attributes/{aid} [delete]
// @Security BearerAuth
func (h *EntityHandler) DeleteAttribute(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || entityID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	attrID, err := strconv.ParseInt(c.Param("aid"), 10, 64)
	if err != nil || attrID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidAttributeID), "invalid_attribute_id"))
		return
	}

	tenantID := getTenantID(c)
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	response, err := h.svc.DeleteAttribute(attrID, entityID, tenantID, req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// PreviewMermaidImport POST /api/v1/model/entities/import-mermaid/preview
// @Summary 预览 Mermaid Markdown 增量导入 | Preview incremental Mermaid Markdown import
// @Description 解析唯一 addp.model.er/v2 文档，按当前 Tenant 精确解析 domain_code 与 element_code，并返回业务域映射和非破坏性增量计划 | Parse the sole addp.model.er/v2 document, resolve domain_code and element_code exactly in the current tenant, and return domain mappings plus a non-destructive incremental plan
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.MermaidImportPreviewRequest true "导入预览请求 | Import preview request"
// @Success 200 {object} models.MermaidImportPreview "导入预览 | Import preview"
// @Failure 400 {object} models.ErrorResponse "Markdown 或 Mermaid 内容无效 | Invalid Markdown or Mermaid content"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "业务域或数据元不存在 | Domain or data element not found"
// @Failure 503 {object} models.ErrorResponse "标准引用校验服务不可用 | Standard reference validation unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.create","model.entity_relation.create"]
// @Router /entities/import-mermaid/preview [post]
// @Security BearerAuth
func (h *EntityHandler) PreviewMermaidImport(c *gin.Context) {
	var req models.MermaidImportPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	result, err := h.svc.PreviewMermaidImport(getTenantID(c), &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ImportMermaid POST /api/v1/model/entities/import-mermaid
// @Summary 确认 Mermaid Markdown 增量导入 | Apply incremental Mermaid Markdown import
// @Description 按预览 revision 确认 addp.model.er/v2 增量创建；不会更新、覆盖或删除现有模型 | Apply addp.model.er/v2 incremental creation against the preview revision without updating, overwriting, or deleting existing models
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.MermaidImportRequest true "确认导入请求 | Apply import request"
// @Success 200 {object} models.MermaidImportResult "导入结果 | Import result"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "Markdown 或 Mermaid 内容无效 | Invalid Markdown or Mermaid content"
// @Failure 404 {object} models.ErrorResponse "业务域或数据元不存在 | Domain or data element not found"
// @Failure 409 {object} models.ErrorResponse "预览基线已过期或存在导入冲突 | Preview baseline expired or import conflicts exist"
// @Failure 503 {object} models.ErrorResponse "标准引用校验服务不可用 | Standard reference validation unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.create","model.entity_relation.create"]
// @Router /entities/import-mermaid [post]
// @Security BearerAuth
func (h *EntityHandler) ImportMermaid(c *gin.Context) {
	var req models.MermaidImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	result, err := h.svc.ImportFromMermaid(tenantID, userID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ExportMermaid GET /api/v1/model/entities/export-mermaid
// @Summary 导出 Mermaid Markdown ER 图 | Export Mermaid Markdown ER diagram
// @Description domain_id 仅选择当前运行时业务域；响应与 addp.model.er/v2 文档使用稳定 domain_code 和 element_code | domain_id only selects the current runtime domain; the response and addp.model.er/v2 document use stable domain_code and element_code values
// @Tags Model
// @Produce json
// @Param domain_id query int false "业务域 ID；省略时导出全部业务域 | Business domain ID; omit to export all domains" minimum(1)
// @Success 200 {object} models.MermaidExportResponse "Markdown Mermaid ER 图文档 | Markdown Mermaid ER document"
// @Failure 400 {object} models.ErrorResponse "业务域 ID 无效 | Invalid domain ID"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 404 {object} models.ErrorResponse "业务域不存在 | Domain not found"
// @Failure 503 {object} models.ErrorResponse "标准引用校验服务不可用 | Standard reference validation unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.entity.read","model.entity_relation.read"]
// @Router /entities/export-mermaid [get]
// @Security BearerAuth
func (h *EntityHandler) ExportMermaid(c *gin.Context) {
	tenantID := getTenantID(c)
	var domainID *int64
	if raw, exists := c.Request.URL.Query()["domain_id"]; exists {
		value, err := parseSinglePositiveInt64(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
			return
		}
		domainID = &value
	}

	mermaidCode, err := h.svc.ExportToMermaid(tenantID, domainID)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, mermaidCode)
}
