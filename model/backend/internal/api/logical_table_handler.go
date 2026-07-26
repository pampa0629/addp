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

type LogicalTableHandler struct {
	svc *service.LogicalTableService
}

func NewLogicalTableHandler(svc *service.LogicalTableService) *LogicalTableHandler {
	return &LogicalTableHandler{svc: svc}
}

// ListLogicalTables GET /api/model/logical-tables
// @Summary 查询逻辑表列表 | List logical tables
// @Tags Model
// @Produce json
// @Param layer query string false "数仓分层 | DW layer"
// @Param table_type query string false "表类型 | Table type"
// @Param status query string false "状态过滤 | Filter by status"
// @Param keyword query string false "关键词搜索 | Keyword search"
// @Param domain_id query int false "业务域ID | Domain ID"
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} map[string]interface{} "逻辑表列表 | Logical table list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables [get]
// @Security BearerAuth
func (h *LogicalTableHandler) ListLogicalTables(c *gin.Context) {
	tenantID := getTenantID(c)

	opts := repository.ListLogicalTableOptions{
		Layer:     c.Query("layer"),
		TableType: c.Query("table_type"),
		Status:    c.Query("status"),
		Keyword:   c.Query("keyword"),
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

	tables, total, err := h.svc.ListLogicalTables(tenantID, opts)
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
		"data":        tables,
		"total":       total,
		"page":        opts.Page,
		"page_size":   opts.PageSize,
		"total_pages": totalPages,
	})
}

// CreateLogicalTable POST /api/model/logical-tables
// @Summary 创建逻辑表 | Create logical table
// @Tags Model
// @Accept json
// @Produce json
// @Param body body models.CreateLogicalTableRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{} "已创建的逻辑表 | Created logical table"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.create"]
// @Router /logical-tables [post]
// @Security BearerAuth
func (h *LogicalTableHandler) CreateLogicalTable(c *gin.Context) {
	var req models.CreateLogicalTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	table, err := h.svc.CreateLogicalTable(&req, tenantID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, table)
}

// GetLogicalTable GET /api/model/logical-tables/:id
// @Summary 获取逻辑表详情 | Get logical table details
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Success 200 {object} map[string]interface{} "逻辑表详情 | Logical table details"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id} [get]
// @Security BearerAuth
func (h *LogicalTableHandler) GetLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	table, err := h.svc.GetLogicalTable(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, modeli18n.MsgTableNotFound)})
		return
	}
	c.JSON(http.StatusOK, table)
}

// UpdateLogicalTable PUT /api/model/logical-tables/:id
// @Summary 更新逻辑表 | Update logical table
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.UpdateLogicalTableRequest true "更新请求 | Update request"
// @Success 200 {object} map[string]interface{} "已更新的逻辑表 | Updated logical table"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id} [put]
// @Security BearerAuth
func (h *LogicalTableHandler) UpdateLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	var req models.UpdateLogicalTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	userID := getUserID(c)

	table, err := h.svc.UpdateLogicalTable(id, tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, table)
}

// DeleteLogicalTable DELETE /api/model/logical-tables/:id
// @Summary 删除逻辑表 | Delete logical table
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.delete"]
// @Router /logical-tables/{id} [delete]
// @Security BearerAuth
func (h *LogicalTableHandler) DeleteLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteLogicalTable(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// GetFields GET /api/model/logical-tables/:id/fields
// @Summary 获取逻辑表字段列表 | Get logical table fields
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Success 200 {object} map[string]interface{} "字段列表 | Field list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/fields [get]
// @Security BearerAuth
func (h *LogicalTableHandler) GetFields(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	fields, err := h.svc.GetFields(tableID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, fields)
}

// CreateField POST /api/model/logical-tables/:id/fields
// @Summary 创建逻辑表字段 | Create logical table field
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param body body models.CreateLogicalFieldRequest true "创建请求 | Create request"
// @Success 201 {object} map[string]interface{} "已创建的字段 | Created field"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.create"]
// @Router /logical-tables/{id}/fields [post]
// @Security BearerAuth
func (h *LogicalTableHandler) CreateField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	var req models.CreateLogicalFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	field, err := h.svc.CreateField(tableID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, field)
}

// UpdateField PUT /api/model/logical-tables/:id/fields/:fid
// @Summary 更新逻辑表字段 | Update logical table field
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param fid path int true "字段ID | Field ID"
// @Param body body models.UpdateLogicalFieldRequest true "更新请求 | Update request"
// @Success 200 {object} map[string]interface{} "已更新的字段 | Updated field"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.update"]
// @Router /logical-tables/{id}/fields/{fid} [put]
// @Security BearerAuth
func (h *LogicalTableHandler) UpdateField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}
	fieldID, err := strconv.ParseInt(c.Param("fid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidFieldID)})
		return
	}

	var req models.UpdateLogicalFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantID := getTenantID(c)
	field, err := h.svc.UpdateField(fieldID, tableID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, field)
}

// DeleteField DELETE /api/model/logical-tables/:id/fields/:fid
// @Summary 删除逻辑表字段 | Delete logical table field
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Param fid path int true "字段ID | Field ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.delete"]
// @Router /logical-tables/{id}/fields/{fid} [delete]
// @Security BearerAuth
func (h *LogicalTableHandler) DeleteField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}
	fieldID, err := strconv.ParseInt(c.Param("fid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidFieldID)})
		return
	}

	tenantID := getTenantID(c)
	if err := h.svc.DeleteField(fieldID, tableID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// PreviewDDL POST /api/model/logical-tables/:id/preview-ddl
// @Summary 预览 DDL | Preview DDL
// @Tags Model
// @Produce json
// @Param id path int true "逻辑表ID | Logical table ID"
// @Success 200 {object} map[string]interface{} "DDL 预览 | DDL preview"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.logical_model.read"]
// @Router /logical-tables/{id}/preview-ddl [post]
// @Security BearerAuth
func (h *LogicalTableHandler) PreviewDDL(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, modeli18n.MsgInvalidID)})
		return
	}

	tenantID := getTenantID(c)
	ddl, err := h.svc.PreviewDDL(tableID, tenantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ddl": ddl})
}
