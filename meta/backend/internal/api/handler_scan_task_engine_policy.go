package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/models"
	"github.com/gin-gonic/gin"
)

var (
	_ = models.EngineScanTaskPolicyRequest{}
	_ = models.ScanTask{}
)

// UpsertEngineScanTask 维护指定 engine 绑定的扫描计划
// @Summary 维护 engine 扫描计划 | Upsert engine scan task
// @Description 为指定 engine 创建、更新或关闭绑定的 Meta 扫描计划 | Create, update or disable the Meta scan task bound to an engine
// @Tags Meta Scan
// @Accept json
// @Produce json
// @Param engine_id path int true "引擎ID | Engine ID"
// @Param request body models.EngineScanTaskPolicyRequest true "engine 扫描计划 | Engine scan policy"
// @Success 200 {object} models.ScanTask "扫描任务 | Scan task"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/engines/{engine_id} [put]
// @Security BearerAuth
func (h *Handler) UpsertEngineScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	userID := commonAuth.GetUserID(c)

	engineID64, err := strconv.ParseUint(c.Param("engine_id"), 10, 32)
	if err != nil || engineID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	var req models.EngineScanTaskPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.taskService.UpsertEngineScanTaskFromPolicy(tenantID, userID, uint(engineID64), req.EngineName, req.ScanPolicy.ToCommonScanPolicy())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusOK, gin.H{"status": "deleted"})
		return
	}

	c.JSON(http.StatusOK, task)
}

// DeleteEngineScanTask 删除指定 engine 绑定的扫描计划
// @Summary 删除 engine 扫描计划 | Delete engine scan task
// @Description 删除指定 engine 绑定的 Meta 扫描计划 | Delete the Meta scan task bound to an engine
// @Tags Meta Scan
// @Produce json
// @Param engine_id path int true "引擎ID | Engine ID"
// @Success 200 {object} map[string]interface{} "删除结果 | Delete result"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 503 {object} map[string]interface{} "任务服务不可用 | Task service unavailable"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /scan/tasks/engines/{engine_id} [delete]
// @Security BearerAuth
func (h *Handler) DeleteEngineScanTask(c *gin.Context) {
	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task service not available"})
		return
	}

	tenantID := commonAuth.GetTenantID(c)
	engineID64, err := strconv.ParseUint(c.Param("engine_id"), 10, 32)
	if err != nil || engineID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid engine_id"})
		return
	}

	if err := h.taskService.DeleteEngineTaskBinding(tenantID, uint(engineID64)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "task deleted"})
}
