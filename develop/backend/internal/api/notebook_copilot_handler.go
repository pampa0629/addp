package api

import (
	"errors"
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// GenerateSessionNotebookCell 仅使用当前 Session 授权资源生成候选 Python 单元。
// @Summary 生成 Notebook Python 单元 | Generate Notebook Python cell
// @Description 未提交 selections 时返回当前 Session 内的数据源候选；提交每个角色确认的 selection 后重新验证 Catalog 与字段事实并生成代码。只返回代码，不执行。 | Without selections, returns data-source candidates inside the current Session. With one confirmed selection per role, revalidates Catalog and field facts before generating code. Returns code without executing it.
// @Tags Notebook
// @Accept json
// @Produce json
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body service.NotebookCopilotRequest true "自然语言与已确认数据源 | Natural language and confirmed data sources"
// @Success 200 {object} service.NotebookCopilotResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 502 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.update","develop.task.read"]
// @Router /notebook-copilot-sessions/{session_id}/generate [post]
func (h *NotebookHandler) GenerateSessionNotebookCell(c *gin.Context) {
	var request service.NotebookCopilotRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookCopilotInvalidRequest)})
		return
	}
	secret, err := c.Cookie(service.NotebookCopilotSessionCookieName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookSessionUnavailable)})
		return
	}
	userToken, err := requestUserAccessToken(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired)})
		return
	}
	response, err := h.notebookCopilotService.Generate(
		c.Request.Context(), userToken, c.Param("session_id"), secret,
		tenantIDValue(c), userIDValue(c), request,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotebookCopilotInvalidRequest):
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookCopilotInvalidRequest)})
		case errors.Is(err, service.ErrNotebookSessionNotFound):
			c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookSessionUnavailable)})
		case errors.Is(err, service.ErrNotebookCopilotForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookCatalogForbidden)})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookCopilotFailed, err.Error())})
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}
