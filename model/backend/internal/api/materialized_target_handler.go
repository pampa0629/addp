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
// @Summary 删除逻辑表的目标物理表 | Delete logical-table physical target
// @Description 校验逻辑表版本和精确目标确认后，仅删除由当前逻辑表管理标记拥有的 PostgreSQL 物理表；保留逻辑表及物理目标配置。| After validating the logical-table version and exact target confirmation, delete only the PostgreSQL table owned by the current logical-table marker; preserve the logical table and its physical-target configuration.
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Param request body models.PhysicalTargetDeleteRequest true "目标物理表精确确认 | Exact physical target confirmation"
// @Success 200 {object} models.MessageResponse "删除成功或目标已不存在 | Deleted or target already absent"
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
	var request models.PhysicalTargetDeleteRequest
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
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func bearerCredential(header string) string {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

// Create enqueues approved physical table creation in the unified execution queue.
// @Summary 创建或校验目标物理表 | Create or verify physical target table
// @Description 提交统一执行队列，根据已审批模型创建目标表；目标已存在且归属及结构一致时校验成功并保留数据，结构不一致时拒绝；不执行数据加工。| Enqueue creation of an approved physical target table; verify and preserve an existing table when ownership and structure match, and reject structural drift; do not process data.
// @Tags Model
// @Accept json
// @Produce json
// @Param id path int true "逻辑表 ID | Logical table ID"
// @Param request body models.VersionRequest true "模型并发版本 | Model version"
// @Success 202 {object} materializationExecuteResponse
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
	if getUserID(c) <= 0 {
		c.JSON(http.StatusForbidden, localizedErrorResponse(c, "common.auth.permission_denied", "permission_denied"))
		return
	}
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
	item, err := h.materialization.EnqueueMaterialization(c.Request.Context(), id, getTenantID(c), request.Version, token, "", "manual")
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, materializationExecuteResponse{ExecutionID: item.ExecutionID, Status: item.Status})
}
