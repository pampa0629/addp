package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetMosaicScanResult reads only the scan bound to a live Manager mosaic parent.
// @Summary 读取 Manager mosaic 扫描结果 | Read Manager mosaic scan result
// @Description 仅允许 addp-manager，在当前父执行有效 lease/attempt 和精确目标绑定下读取专用子扫描状态及正式 whole item 身份；不返回配置、metadata、授权或原始错误 | Only addp-manager may read the dedicated scan status and whole item identity bound to the live parent lease, attempt and exact target; no configuration, metadata, authorization or raw errors
// @Tags Meta Scan
// @Produce json
// @Param execution_id path string true "扫描执行 UUID | Scan execution UUID"
// @Param parent_execution_id query string true "当前 Manager 父执行 UUID | Current Manager parent execution UUID"
// @Success 200 {object} commonclient.MetaMosaicScanResult "受限扫描结果 | Bound scan result"
// @Failure 400 {object} map[string]interface{} "UUID 参数错误 | Invalid UUID"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthenticated"
// @Failure 403 {object} map[string]interface{} "服务或权限不匹配 | Client or Permission denied"
// @Failure 404 {object} map[string]interface{} "无有效父子绑定或正式结果 | No valid parent binding or formal result"
// @Failure 503 {object} map[string]interface{} "结果依赖不可用 | Result dependency unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /runtime/mosaic-scans/{execution_id}/result [get]
// @Security BearerAuth
func (h *Handler) GetMosaicScanResult(c *gin.Context) {
	executionID, parentID := c.Param("execution_id"), c.Query("parent_execution_id")
	for _, raw := range []string{executionID, parentID} {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || id.String() != raw {
			c.JSON(http.StatusBadRequest, gin.H{"error_code": "scan_request_invalid", "error": commoni18n.T(c, metai18n.MsgScanRequestInvalid)})
			return
		}
	}
	if h.executionService != nil {
		var result *commonclient.MetaMosaicScanResult
		var err error
		result, err = h.executionService.GetMosaicScanResult(c.Request.Context(), int(commonAuth.GetTenantID(c)), executionID, parentID)
		if err == nil {
			c.JSON(http.StatusOK, result)
			return
		}
		if errors.Is(err, commonapi.ErrNotFound) || errors.Is(err, commonapi.ErrBadRequest) {
			c.JSON(http.StatusNotFound, gin.H{"error_code": "scan_provenance_unavailable", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
			return
		}
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error_code": "scan_result_unavailable", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
}
