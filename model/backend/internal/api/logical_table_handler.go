package api

import (
	"net/http"
	"strconv"

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
// @Summary ListLogicalTables
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listlogicaltables [get]
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
// @Summary CreateLogicalTable
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createlogicaltable [get]
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
// @Summary GetLogicalTable
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getlogicaltable [get]
// @Security BearerAuth
func (h *LogicalTableHandler) GetLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	tenantID := getTenantID(c)
	table, err := h.svc.GetLogicalTable(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "table not found"})
		return
	}
	c.JSON(http.StatusOK, table)
}

// UpdateLogicalTable PUT /api/model/logical-tables/:id
// @Summary UpdateLogicalTable
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updatelogicaltable [get]
// @Security BearerAuth
func (h *LogicalTableHandler) UpdateLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
// @Summary DeleteLogicalTable
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deletelogicaltable [get]
// @Security BearerAuth
func (h *LogicalTableHandler) DeleteLogicalTable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
// @Summary GetFields
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getfields [get]
// @Security BearerAuth
func (h *LogicalTableHandler) GetFields(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
// @Summary CreateField
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /createfield [get]
// @Security BearerAuth
func (h *LogicalTableHandler) CreateField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
// @Summary UpdateField
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updatefield [get]
// @Security BearerAuth
func (h *LogicalTableHandler) UpdateField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	fieldID, err := strconv.ParseInt(c.Param("fid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid field id"})
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
// @Summary DeleteField
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /deletefield [get]
// @Security BearerAuth
func (h *LogicalTableHandler) DeleteField(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	fieldID, err := strconv.ParseInt(c.Param("fid"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid field id"})
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
// @Summary PreviewDDL
// @Tags Model
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /previewddl [get]
// @Security BearerAuth
func (h *LogicalTableHandler) PreviewDDL(c *gin.Context) {
	tableID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
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
