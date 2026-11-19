package api

import (
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

// ListAutomaticScopes godoc
// @Summary 查询自动采集范围和实际采集状态 | List automatic collection scopes and observed state
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Produce json
// @Security BearerAuth
// @Success 200 {object} service.AutomaticCollectionList
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.monitoring_target.read","platform.host_node.read"]
// @Router /platform/automatic_collection_scopes [get]
func (h *MonitoringTargetHandler) ListAutomaticScopes(c *gin.Context) {
	if !targetNoQuery(c) || !targetNoBody(c) {
		return
	}
	rows, err := h.targets.AutomaticScopes(c.Request.Context(), targetToken(c))
	if err != nil {
		targetError(c, err)
		return
	}
	c.JSON(200, rows)
}

// SaveAutomaticScope godoc
// @Summary 更新单个自动采集范围 | Update one automatic collection scope
// @Tags 平台运行监控 | Platform Runtime Monitoring
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "自动采集范围 ID | Automatic collection scope ID"
// @Param input body service.AutomaticCollectionInput true "采集开关和版本 | Collection intent and version"
// @Success 200 {object} models.AutomaticCollectionScope
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 422 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["monitor.monitoring_target.read","monitor.monitoring_target.update","platform.host_node.read"]
// @Router /platform/automatic_collection_scopes/{id} [put]
func (h *MonitoringTargetHandler) SaveAutomaticScope(c *gin.Context) {
	var input service.AutomaticCollectionInput
	if !targetNoQuery(c) || !targetBind(c, &input) {
		return
	}
	var result models.AutomaticCollectionScope
	result, err := h.targets.SaveAutomaticScope(c.Request.Context(), targetToken(c), c.Param("id"), input)
	if err != nil {
		targetError(c, err)
		return
	}
	c.Set("automatic_collection_audit_id", result.ID)
	c.Set("automatic_collection_audit_version", result.Version)
	c.Set("automatic_collection_audit_enabled", result.Enabled)
	c.JSON(200, result)
}
