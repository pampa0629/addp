package api

import (
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type LogicalTableHandler struct {
	svc *service.LogicalTableService
}

func NewLogicalTableHandler(svc *service.LogicalTableService) *LogicalTableHandler {
	return &LogicalTableHandler{svc: svc}
}

// ListLogicalTables GET /api/v1/model/logical-tables
// @Summary 查询逻辑表列表 | List logical tables
// @Tags Model
// @Produce json
// @Param layer query string false "数仓分层 | DW layer"
// @Param table_type query string false "表类型 | Table type" Enums(entity, fact, dimension)
// @Param status query string false "状态过滤 | Filter by status" Enums(draft, approved)
// @Param keyword query string false "关键词搜索 | Keyword search"
// @Param domain_id query int false "业务域ID | Domain ID" minimum(1)
// @Param page query int false "页码 | Page number" default(1) minimum(1)
// @Param page_size query int false "每页数量 | Page size" default(20) minimum(1) maximum(100)
// @Success 200 {object} models.LogicalTableListResponse "逻辑表列表 | Logical table list"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "查询参数无效 | Invalid query parameters"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables [get]
// @Security BearerAuth
func (h *LogicalTableHandler) ListLogicalTables(c *gin.Context) {
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
	tableType, err := parseOptionalEnum(c.Request.URL.Query()["table_type"], "entity", "fact", "dimension")
	if err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	layer, err := parseOptionalString(c.Request.URL.Query()["layer"])
	if err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	keyword, err := parseOptionalString(c.Request.URL.Query()["keyword"])
	if err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	opts := repository.ListLogicalTableOptions{
		DomainID:  query.DomainID,
		Layer:     layer,
		TableType: tableType,
		Status:    status,
		Keyword:   keyword,
		Page:      query.Page,
		PageSize:  query.PageSize,
	}

	tables, total, err := h.svc.ListLogicalTables(tenantID, opts)
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
		"data":        tables,
		"total":       total,
		"page":        opts.Page,
		"page_size":   opts.PageSize,
		"total_pages": totalPages,
	})
}

// CreateLogicalTable POST /api/v1/model/logical-tables
// @Summary 创建逻辑表 | Create logical table
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.CreateLogicalTableRequest true "创建请求 | Create request"
// @Success 201 {object} models.LogicalTable "已创建的逻辑表 | Created logical table"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "请求无效 | Invalid request"
// @Failure 404 {object} models.ErrorResponse "引用的业务域不存在 | Referenced business domain not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表编码冲突 | Logical table code conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.create"]
// @Router /logical-tables [post]
// @Security BearerAuth
func (h *LogicalTableHandler) CreateLogicalTable(c *gin.Context) {
	var req models.CreateLogicalTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	table, err := h.svc.CreateLogicalTable(&req, tenantID, userID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, table)
}

// GetLogicalTable GET /api/v1/model/logical-tables/:id
// @Summary 获取逻辑表详情 | Get logical table details
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Success 200 {object} models.LogicalTable "逻辑表详情 | Logical table details"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "逻辑表 ID 无效 | Invalid logical table ID"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id} [get]
// @Security BearerAuth
func (h *LogicalTableHandler) GetLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	table, err := h.svc.GetLogicalTable(id, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, table)
}

// UpdateLogicalTable PUT /api/v1/model/logical-tables/:id
// @Summary 更新逻辑表 | Update logical table
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.UpdateLogicalTableRequest true "更新请求 | Update request"
// @Success 200 {object} models.LogicalTable "已更新的逻辑表 | Updated logical table"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "请求或逻辑表 ID 无效 | Invalid request or logical table ID"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态或字段列名冲突 | Logical table state or field column name conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id} [put]
// @Security BearerAuth
func (h *LogicalTableHandler) UpdateLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.UpdateLogicalTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	table, err := h.svc.UpdateLogicalTable(id, tenantID, userID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, table)
}

// DeleteLogicalTable DELETE /api/v1/model/logical-tables/:id
// @Summary 删除逻辑表 | Delete logical table
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.MessageResponse "删除成功 | Deleted successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "逻辑表 ID 无效 | Invalid logical table ID"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态或关联冲突 | Logical table state or relation conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.delete"]
// @Router /logical-tables/{id} [delete]
// @Security BearerAuth
func (h *LogicalTableHandler) DeleteLogicalTable(c *gin.Context) {
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
	if err := h.svc.DeleteLogicalTable(id, tenantID, req.Version); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ApproveLogicalTable POST /api/v1/model/logical-tables/:id/approve
// @Summary 审批逻辑表 | Approve logical table
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.LogicalTable "审批成功 | Approved successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "逻辑表缺少字段或主键 | Logical table has no fields or primary key"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态冲突 | Logical table state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/approve [post]
// @Security BearerAuth
func (h *LogicalTableHandler) ApproveLogicalTable(c *gin.Context) {
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
	table, err := h.svc.ApproveLogicalTable(id, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, table)
}

// ReopenLogicalTable POST /api/v1/model/logical-tables/:id/reopen
// @Summary 重新打开逻辑表 | Reopen logical table
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.VersionRequest true "资源版本 | Resource version"
// @Success 200 {object} models.LogicalTable "重新打开成功 | Reopened successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "逻辑表 ID 无效 | Invalid logical table ID"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态冲突 | Logical table state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/reopen [post]
// @Security BearerAuth
func (h *LogicalTableHandler) ReopenLogicalTable(c *gin.Context) {
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
	table, err := h.svc.ReopenLogicalTable(id, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, table)
}

// GetFields GET /api/v1/model/logical-tables/:id/fields
// @Summary 获取逻辑表字段列表 | Get logical table fields
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Success 200 {array} models.LogicalField "字段列表 | Field list"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "逻辑表 ID 无效 | Invalid logical table ID"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/fields [get]
// @Security BearerAuth
func (h *LogicalTableHandler) GetFields(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	tenantID := getTenantID(c)
	fields, err := h.svc.GetFields(tableID, tenantID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, fields)
}

// CreateField POST /api/v1/model/logical-tables/:id/fields
// @Summary 创建逻辑表字段 | Create logical table field
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.CreateLogicalFieldRequest true "创建请求 | Create request"
// @Success 201 {object} models.LogicalFieldMutationResponse "已创建的字段 | Created field"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "字段定义无效 | Invalid field definition"
// @Failure 404 {object} models.ErrorResponse "逻辑表或引用资源不存在 | Logical table or referenced resource not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态冲突 | Logical table state conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.create"]
// @Router /logical-tables/{id}/fields [post]
// @Security BearerAuth
func (h *LogicalTableHandler) CreateField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}

	var req models.CreateLogicalFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	field, err := h.svc.CreateField(tableID, tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, field)
}

// UpdateField PUT /api/v1/model/logical-tables/:id/fields/:fid
// @Summary 更新逻辑表字段 | Update logical table field
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param fid path int true "字段ID | Field ID"
// @Param body body models.UpdateLogicalFieldRequest true "更新请求 | Update request"
// @Success 200 {object} models.LogicalFieldMutationResponse "已更新的字段 | Updated field"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "字段定义无效 | Invalid field definition"
// @Failure 404 {object} models.ErrorResponse "逻辑表或字段不存在 | Logical table or field not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态或字段列名冲突 | Logical table state or field column name conflict"
// @Failure 503 {object} models.ErrorResponse "数据标准服务不可用 | Data Standard service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/fields/{fid} [put]
// @Security BearerAuth
func (h *LogicalTableHandler) UpdateField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	fieldID, err := strconv.ParseInt(c.Param("fid"), 10, 64)
	if err != nil || fieldID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidFieldID), "invalid_field_id"))
		return
	}

	var req models.UpdateLogicalFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}

	tenantID := getTenantID(c)
	field, err := h.svc.UpdateField(fieldID, tableID, tenantID, &req)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, field)
}

// DeleteField DELETE /api/v1/model/logical-tables/:id/fields/:fid
// @Summary 删除逻辑表字段 | Delete logical table field
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param fid path int true "字段ID | Field ID"
// @Param body body models.VersionRequest true "父资源版本 | Parent resource version"
// @Success 200 {object} models.VersionResponse "删除成功 | Deleted successfully"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "逻辑表或字段 ID 无效 | Invalid logical table or field ID"
// @Failure 404 {object} models.ErrorResponse "逻辑表或字段不存在 | Logical table or field not found"
// @Failure 409 {object} models.ErrorResponse "逻辑表状态冲突 | Logical table state conflict"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.delete"]
// @Router /logical-tables/{id}/fields/{fid} [delete]
// @Security BearerAuth
func (h *LogicalTableHandler) DeleteField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	fieldID, err := strconv.ParseInt(c.Param("fid"), 10, 64)
	if err != nil || fieldID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidFieldID), "invalid_field_id"))
		return
	}

	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	tenantID := getTenantID(c)
	response, err := h.svc.DeleteField(fieldID, tableID, tenantID, req.Version)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// PreviewDDL POST /api/v1/model/logical-tables/:id/preview-ddl
// @Summary 预览 DDL | Preview DDL
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.PreviewLogicalTableDDLRequest true "当前物化配置 | Current materialization configuration"
// @Success 200 {object} models.DDLPreviewResponse "DDL 预览 | DDL preview"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足 | Permission denied"
// @Failure 400 {object} models.ErrorResponse "请求或物化配置无效 | Invalid request or materialization configuration"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/preview-ddl [post]
// @Security BearerAuth
func (h *LogicalTableHandler) PreviewDDL(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || tableID <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	var req models.PreviewLogicalTableDDLRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil || req.Materialization == nil {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, commoni18n.MsgInvalidParams), "invalid_request"))
		return
	}

	tenantID := getTenantID(c)
	ddl, err := h.svc.PreviewDDL(tableID, tenantID, req.Materialization)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ddl": ddl})
}
