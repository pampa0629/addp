package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	metai18n "github.com/addp/meta/i18n"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// EnsureProductionBasicScan godoc
// @Summary 确保 PG 成功新成果的基础识别 | Ensure basic identification of a newly committed PG production
// @Description 仅 addp-transfer；无正文和 query。Meta 自行反查同租户 Transfer v3 成功回执，仅识别精确首次新建表；复用统一扫描创建和执行链。重复请求恢复原扫描，不复活失败执行，不授予数据权限 | Fixed addp-transfer only; no body or query. Meta resolves same-Tenant successful Transfer v3 evidence and identifies only the exact newly created table through the shared scan pipeline. Replay recovers the original scan, never revives failed executions or grants access
// @Tags Meta Scan
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "原 Transfer 执行 UUID | Original Transfer execution UUID"
// @Success 200 {object} commonclient.MetaProductionBasicScan
// @Failure 400,401,403,409,503 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /runtime/transfer-productions/{execution_id}/basic-scan [post]
func (h *Handler) EnsureProductionBasicScan(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("execution_id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("execution_id") || c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error_code": "scan_request_invalid", "error": commoni18n.T(c, metai18n.MsgScanRequestInvalid)})
		return
	}
	if h.executionService != nil {
		var result *commonclient.MetaProductionBasicScan
		result, err = h.executionService.EnsureTransferProductionBasicScan(c.Request.Context(), commonauth.GetTenantID(c), id.String())
		if err == nil {
			c.JSON(http.StatusOK, result)
			return
		}
		if errors.Is(err, commonapi.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error_code": "scan_provenance_unavailable", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
			return
		}
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error_code": "production_scan_pending", "error": commoni18n.T(c, metai18n.MsgScanProvenanceUnavailable)})
}
