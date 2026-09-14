package api

import (
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/service"
	"github.com/gin-gonic/gin"
)

type MaterializedTargetHandler struct {
	materialization *service.MaterializationService
}

func NewMaterializedTargetHandler(materialization *service.MaterializationService) *MaterializedTargetHandler {
	return &MaterializedTargetHandler{materialization: materialization}
}

// Decommission deletes the exact physical target currently registered by a logical table.
// @Summary 退役逻辑表物化目标 | Decommission logical-table materialized target
// @Description 校验逻辑表版本和精确目标确认后，仅删除由当前逻辑表管理标记拥有的 PostgreSQL 物理表；不修改逻辑表配置。| After validating the logical-table version, exact target confirmation, delete only the PostgreSQL table owned by the current logical-table marker; the logical-table definition is unchanged.
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Param request body models.MaterializedTargetDecommissionRequest true "物化目标精确确认 | Exact materialized target confirmation"
// @Success 200 {object} models.MessageResponse "退役成功或目标已不存在 | Decommissioned or target already absent"
// @Failure 400 {object} models.ErrorResponse "请求或目标确认无效 | Invalid request or target confirmation"
// @Failure 401 {object} models.ErrorResponse "未认证 | Authentication required"
// @Failure 403 {object} models.ErrorResponse "权限不足或没有目标引擎 DDL 权限 | Permission denied or target engine DDL access denied"
// @Failure 404 {object} models.ErrorResponse "逻辑表不存在 | Logical table not found"
// @Failure 409 {object} models.ErrorResponse "版本、目标确认或所有权冲突 | Version, target confirmation, or ownership conflict"
// @Failure 503 {object} models.ErrorResponse "System 或目标引擎暂时不可用 | System or target engine unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialized_target.delete"]
// @Router /logical-tables/{id}/materialized-target [delete]
// @Security BearerAuth
func (h *MaterializedTargetHandler) Decommission(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, errorResponseWithCode(commoni18n.T(c, modeli18n.MsgInvalidID), "invalid_id"))
		return
	}
	var request models.MaterializedTargetDecommissionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || request.Version <= 0 ||
		strings.TrimSpace(request.TargetParentLocator) == "" || strings.TrimSpace(request.TargetName) == "" {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	token := bearerCredential(c.GetHeader("Authorization"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, localizedErrorResponse(c, "common.auth.authentication_required", "authentication_required"))
		return
	}
	if err := h.materialization.DecommissionMaterializedTarget(c.Request.Context(), id, getTenantID(c), request, token); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "decommissioned"})
}

func bearerCredential(header string) string {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

// Create creates the approved physical table without modifying existing data.
// @Summary 创建正式表 | Create physical target
// @Description 根据已审批模型创建正式表；同归属且结构一致时幂等成功，结构不一致拒绝；不执行数据加工。| Create an approved physical target, preserving existing data when ownership and structure match; reject structural drift.
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Param request body models.VersionRequest true "模型并发版本 | Model version"
// @Success 200 {object} map[string]string
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Failure 403 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 409 {object} models.ErrorResponse
// @Failure 503 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["model.materialization.execute"]
// @Router /logical-tables/{id}/materialized-target [post]
// @Security BearerAuth
func (h *MaterializedTargetHandler) Create(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var request models.VersionRequest
	if err != nil || id <= 0 || commonapi.BindOptionalJSONStrict(c, &request) != nil || request.Version <= 0 {
		c.JSON(http.StatusBadRequest, invalidParamsResponse(c))
		return
	}
	token := bearerCredential(c.GetHeader("Authorization"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, localizedErrorResponse(c, "common.auth.authentication_required", "authentication_required"))
		return
	}
	locator, err := h.materialization.CreateMaterializedTarget(c.Request.Context(), id, getTenantID(c), request.Version, token)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"target_locator": locator})
}
