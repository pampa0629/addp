package api

import (
	commonClient "github.com/addp/common/client"
	i18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// ReadCDCRisks reads task observations for the Monitor evaluator.
// @Summary 读取 CDC 任务风险观测 | Read CDC task risk observations
// @Description 仅 addp-monitor 平台服务身份读取当前 CDC 任务风险清单，不含业务数据或私有捕获资源。| Only the addp-monitor platform Service reads the current safe CDC risk inventory, without business data or private capture resources.
// @Tags 运行时 | Runtime
// @Produce json
// @Param after_id query int false "上一页最后一个任务 ID，首批为0，每批最多100项 | Last task ID from the previous page, initially zero; at most 100 items per page"
// @Success 200 {object} commonClient.TransferCDCRisks
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["transfer.cdc_risk_observation.read"]
// @Router /runtime/cdc-risk-observations [get]
// @Security BearerAuth
func (h *TaskHandler) ReadCDCRisks(c *gin.Context) {
	values := c.Request.URL.Query()
	raw := "0"
	if len(values) > 1 || len(values) == 1 && len(values["after_id"]) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.T(c, i18n.MsgInvalidParams), "error_code": "invalid_params"})
		return
	}
	if len(values) == 1 {
		raw = values["after_id"][0]
	}
	after, err := strconv.ParseUint(raw, 10, strconv.IntSize)
	if err != nil || strconv.FormatUint(after, 10) != raw {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.T(c, i18n.MsgInvalidParams), "error_code": "invalid_params"})
		return
	}
	var result *commonClient.TransferCDCRisks
	result, err = h.taskService.ReadCDCRisks(c.Request.Context(), uint(after))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": i18n.T(c, "transfer.cdc.capture_unavailable"), "error_code": "cdc_risk_observation_unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}
