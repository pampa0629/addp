package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/common/catalogview"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type MetadataHandler struct {
	metadataService *service.MetadataService
}

func NewMetadataHandler(metadataService *service.MetadataService) *MetadataHandler {
	return &MetadataHandler{
		metadataService: metadataService,
	}
}

// RefreshItem 强制刷新指定 item 的元数据
// @Summary 刷新数据项元数据 | Refresh item metadata
// @Description 强制触发一次 item 对应的元数据重扫，并等待扫描完成 | Force a deep metadata rescan for an item and wait until completion
// @Tags Manager
// @Router /engines/{id}/items/refresh [post]
// @Accept json
// @Produce json
// @Param id path int true "存储引擎ID | Engine ID"
// @Param locator query string false "资源定位符 URI | Resource locator URI"
// @Param body body models.MetaManualScanRequest false "扫描定位（可选，仅支持 item_id）| Scan target (optional, item_id only)"
// @Success 200 {object} models.MetaScanResponse "同步扫描结果 | Synchronous scan result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Security BearerAuth
func (h *MetadataHandler) RefreshItem(c *gin.Context) {
	engineID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}

	var req models.MetaManualScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if locatorURI := strings.TrimSpace(c.Query("locator")); locatorURI != "" {
		loc, err := catalogview.ParseURI(locatorURI)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if loc.EngineID != engineID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "locator engine_id does not match path engine_id"})
			return
		}
		if loc.ItemID != nil && *loc.ItemID > 0 {
			req.ItemID = *loc.ItemID
		} else if loc.NodeID != nil && *loc.NodeID > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "item refresh requires item locator"})
			return
		}
	}
	if req.ItemID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "locator with item_id or body item_id is required"})
		return
	}
	req.ScanDepth = "deep"
	req.Force = true

	tenantID := commonAuth.GetTenantID(c)
	result, err := h.metadataService.RefreshItem(c.Request.Context(), &tenantID, engineID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func parseUintParam(c *gin.Context, key string) (uint, bool) {
	value := c.Param(key)
	if strings.TrimSpace(value) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing " + key})
		return 0, false
	}

	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + key})
		return 0, false
	}

	return uint(parsed), true
}
