package api

import (
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/gin-gonic/gin"
)

// RefreshItem 刷新已知数据项
// @Summary 刷新已知数据项 | Refresh known item
// @Description 创建手动扫描执行并等待刷新完成；单个 CSV/TSV 表格可指定 encoding 和 has_header，成功后读取设置和结构一起生效，失败保留旧摘要 | Create a manual execution and wait for refresh; a single CSV/TSV table may specify encoding and has_header, applied with its schema only on success; failure preserves the prior snapshot
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Param request body models.ScanRequest false "刷新请求 | Refresh request"
// @Success 200 {object} models.ScanResponse "刷新结果 | Refresh result"
// @Failure 409 {object} map[string]interface{} "同范围扫描正在执行：scan_scope_active | Scan scope already active: scan_scope_active"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "缺少当前用户来源 | Current User origin is required"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /items/{item_id}/refresh [post]
// @Security BearerAuth
func (h *Handler) RefreshItem(c *gin.Context) {
	if h.executionService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)
	itemID64, err := strconv.ParseUint(c.Param("item_id"), 10, 32)
	if err != nil || itemID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	var req models.ScanRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": "scan_request_invalid", "error": commoni18n.T(c, metai18n.MsgScanRequestInvalid)})
		return
	}
	req.ItemID = uint(itemID64)
	req.TriggerType = models.TriggerTypeManual
	if strings.TrimSpace(req.ScanDepth) == "" {
		req.ScanDepth = "deep"
	}

	run, err := h.executionService.CreateManualRun(scanUserOriginContext(c), tenantID, userID, &req)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	exec, err := h.executionService.WaitExecution(c.Request.Context(), run.ExecutionID, int(tenantID), 0)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "execution wait timed out") {
			status = http.StatusGatewayTimeout
		}
		c.JSON(status, gin.H{"error": err.Error(), "execution_id": run.ExecutionID})
		return
	}

	result, err := scanflow.ScanResponseFromExecution(exec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "execution_id": run.ExecutionID})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListEngineItems 获取引擎下已扫描的数据项列表。
// @Summary 列出引擎数据项 | List engine items
// @Description 获取指定引擎下已扫描的数据项，可按 catalog 第一层业务分支过滤 | List scanned metadata items for an engine, optionally filtered by catalog first business branch
// @Tags Meta Query
// @Produce json
// @Param engine_id path int true "存储引擎ID | Engine ID"
// @Param branch query string false "catalog 第一层业务分支名称 | Catalog first business branch name"
// @Success 200 {array} models.MetaItemLite "数据项列表 | Items"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Router /engines/{engine_id}/items [get]
// @Security BearerAuth
func (h *Handler) ListEngineItems(c *gin.Context) {
	engineIDStr := c.Param("engine_id")
	engineID, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}
	tenantID, err := h.effectiveTenantIDForEngine(c, uint(engineID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	branch := c.Query("branch")
	var items []models.MetaItemLite
	if branch != "" {
		items, err = h.metadataQueryService.ListItemsByBranch(uint(engineID), tenantID, branch)
	} else {
		items, err = h.metadataQueryService.ListItemsByEngine(uint(engineID), tenantID)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, items)
}

// GetItemFieldsByID 按 item_id 获取数据项字段。
// @Summary 按 ID 获取数据项字段 | Get item fields by ID
// @Description 按数据项 ID 获取字段详情 | Get item field details by item ID
// @Tags Meta Query
// @Produce json
// @Param item_id path int true "数据项ID | Item ID"
// @Success 200 {object} map[string]interface{} "字段详情 | Field details"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "数据项不存在 | Item not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Router /items/{item_id}/fields [get]
// @Security BearerAuth
func (h *Handler) GetItemFieldsByID(c *gin.Context) {
	tenantID := commonAuth.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	fields, err := h.metadataQueryService.GetItemFieldDetailsByID(tenantID, uint(itemID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, fields)
}
