package api

import (
	"errors"
	"io"
	"net/http"

	commonAPI "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/middleware/i18n"
	transferI18n "github.com/addp/transfer/i18n"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// BuildTaskConfigDraft compiles user choices using persisted owner facts.
// @Summary 只读构建传输任务草稿 | Build transfer configuration read-only
// @Description 基于当前租户可见的已登记源结构和目标父节点构建 MongoDB 到 PostgreSQL 的 bounded snapshot。需要四项权限交集。仅返回可交给正式预检的完整候选参数，不读取业务行、不保存任务或启动执行。| Build a MongoDB to PostgreSQL bounded snapshot from tenant-visible persisted structure and target parent facts. Requires all four permissions. Returns complete candidate arguments for formal preflight without business reads, task persistence or execution.
// @Tags 任务管理 | Task Management
// @Accept json
// @Produce json
// @Param request body service.TaskConfigDraftRequest true "资源引用与业务选择 | Resource references and business choices"
// @Success 200 {object} service.TaskConfigDraftResponse "候选创建参数 | Candidate creation arguments"
// @Failure 400 {object} map[string]string "请求无效 | Invalid request"
// @Failure 401 {object} map[string]string "未认证 | Authentication required"
// @Failure 403 {object} map[string]string "无权限 | Access denied"
// @Failure 422 {object} map[string]string "选择无法构建 | Choices cannot be compiled"
// @Failure 502 {object} map[string]string "事实服务不可用 | Fact dependency unavailable"
// @x-addp-auth-mode "delegated_tool"
// @x-addp-required-permissions ["transfer.task.create","meta.catalog.read","system.engine.read","manager.data_item.read"]
// @Router /task-config-drafts [post]
// @Security BearerAuth
func (h *TaskHandler) BuildTaskConfigDraft(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := commonAuth.AuthContextFromGin(c)
	if !ok {
		commonAPI.UnauthorizedError(c, i18n.T(c, i18n.MsgUnauthorized))
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (512<<10)+1))
	if err != nil || c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.T(c, transferI18n.MsgDraftInvalid), "error_code": "draft_input_invalid"})
		return
	}
	result, err := h.taskService.BuildTaskConfigDraft(c.Request.Context(), subject, body)
	var diagnostic interface{ DraftDiagnostic() (string, string) }
	switch {
	case errors.Is(err, service.ErrConfigCheckForbidden):
		commonAPI.ForbiddenError(c, i18n.T(c, i18n.MsgForbidden))
	case errors.Is(err, service.ErrInvalidTaskConfig):
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.T(c, transferI18n.MsgDraftInvalid), "error_code": "draft_input_invalid"})
	case errors.As(err, &diagnostic):
		code, path := diagnostic.DraftDiagnostic()
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": i18n.T(c, transferI18n.MsgDraftInvalid), "error_code": code, "path": path})
	case err != nil:
		c.JSON(http.StatusBadGateway, gin.H{"error": i18n.T(c, transferI18n.MsgConfigCheckUnavailable), "error_code": "draft_facts_unavailable"})
	default:
		c.JSON(http.StatusOK, result)
	}
}
