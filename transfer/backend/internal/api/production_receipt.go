package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	transferi18n "github.com/addp/transfer/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ProductionReceipt = client.TransferProductionReceipt

// ReadProductionReceipt godoc
// @Summary 读取精确产出回执 | Read an exact production receipt
// @Description 仅供当前 Tenant 的 addp-system 读取 PG Transfer 已成功执行的 v3 产出证据、实际会话来源集合与创建提交事实；false 或 null 不得补造为已证明或长期许可。不代表首次新建资格、Meta 边界或数据权限，不返回私有配置或凭据 | Only the current Tenant addp-system Service may read v3 PG production evidence, actual session sources and the created-and-committed fact. False or null cannot imply proven creation or unbounded permission. Not initial-Grant eligibility, a Meta boundary or data permission; no private configuration or credentials
// @Tags Runtime
// @Produce json
// @Security BearerAuth
// @Param execution_id path string true "原执行 UUID | Original execution UUID"
// @Success 200 {object} ProductionReceipt
// @Failure 400,401,403,404,409,500 {object} map[string]interface{}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.production_receipt.read"]
// @Router /runtime/executions/{execution_id}/production-receipt [get]
func (h *ExecutionHandler) ReadProductionReceipt(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
		return
	}
	if h == nil || h.executionService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, transferi18n.MsgExecutionReadUnavailable)})
		return
	}
	result, err := h.executionService.ReadProductionReceipt(c.Request.Context(), commonAuth.GetTenantID(c), c.Param("execution_id"))
	if err == nil {
		c.JSON(http.StatusOK, result)
		return
	}
	status, key := http.StatusInternalServerError, transferi18n.MsgExecutionReadUnavailable
	if errors.Is(err, commonapi.ErrBadRequest) {
		status, key = http.StatusBadRequest, commoni18n.MsgInvalidParams
	} else if errors.Is(err, commonapi.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		status, key = http.StatusNotFound, transferi18n.MsgExecutionNotFound
	} else if errors.Is(err, commonapi.ErrConflict) {
		status, key = http.StatusConflict, transferi18n.MsgProductionReceiptUnavailable
	}
	c.JSON(status, gin.H{"error": commoni18n.T(c, key)})
}
