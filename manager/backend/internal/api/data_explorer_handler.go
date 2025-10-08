package api

import (
	"net/http"
	"strconv"

	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type DataExplorerHandler struct {
	metadataService *service.MetadataService
}

func NewDataExplorerHandler(metadataService *service.MetadataService) *DataExplorerHandler {
	return &DataExplorerHandler{
		metadataService: metadataService,
	}
}

// GetTree 返回资源- schema-表树
func (h *DataExplorerHandler) GetTree(c *gin.Context) {
	tree, err := h.metadataService.GetResourceTree()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// PreviewTable 返回表数据预览
func (h *DataExplorerHandler) PreviewTable(c *gin.Context) {
	resourceIDStr := c.Query("resource_id")
	schemaName := c.Query("schema")
	tableName := c.Query("table")

	if resourceIDStr == "" || schemaName == "" || tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required parameters"})
		return
	}

	resourceIDUint, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	preview, err := h.metadataService.PreviewTable(uint(resourceIDUint), schemaName, tableName, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, preview)
}
