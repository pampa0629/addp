package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// CreateSession 创建 Notebook 交互会话。
// @Summary 创建 Notebook 交互会话 | Create Notebook interactive session
// @Tags Notebook
// @Produce json
// @Param id path int true "DevTask ID | DevTask ID"
// @Success 201 {object} service.NotebookSession "Notebook 交互会话 | Notebook interactive session"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.update","develop.task.read"]
// @Router /notebooks/{id}/sessions [post]
func (h *NotebookHandler) CreateSession(c *gin.Context) {
	var uri struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookInvalidID)})
		return
	}
	session, secret, err := h.notebookSessionService.Create(c.Request.Context(), tenantIDValue(c), userIDValue(c), uri.ID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotebookNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookNotFound)})
		case errors.Is(err, service.ErrTaskNotNotebook):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, developi18n.MsgTaskNotNotebook)})
		case errors.Is(err, service.ErrNotebookSessionConflict):
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookSessionConflict)})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookSessionOpenFailed, err.Error())})
		}
		return
	}
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{
		Name: service.NotebookSessionCookieName, Value: secret,
		Path:     "/api/v1/develop/notebook-sessions/" + session.ID + "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
		Expires: session.ExpiresAt, MaxAge: max(1, int(time.Until(session.ExpiresAt).Seconds())),
	})
	c.JSON(http.StatusCreated, session)
}

// ListSessionEngineDescriptors 返回当前 Notebook Kernel 可发现的查询引擎。
// @Summary 获取 Notebook Kernel 可用查询引擎 | List query engines available to a Notebook Kernel
// @Description 仅接受 Develop 注入当前隔离 Kernel 的短期 Notebook Kernel Capability Bearer；不接受用户或服务 Token。 | Only accepts the short-lived Notebook Kernel Capability Bearer injected by Develop into the isolated Kernel; user and service tokens are not accepted.
// @Tags Notebook
// @Produce json
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Success 200 {array} models.EngineRuntimeDescriptor "脱敏查询引擎描述列表 | Masked query engine descriptor list"
// @Failure 401 {object} map[string]string "会话能力无效或已过期 | Session capability is invalid or expired"
// @Failure 502 {object} map[string]string "查询引擎发现失败 | Query engine discovery failed"
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/engine-descriptors [get]
func (h *NotebookHandler) ListSessionEngineDescriptors(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "notebook kernel capability is required"})
		return
	}
	descriptors, err := h.listSessionEngineDescriptors(
		c.Request.Context(), c.Param("session_id"), token,
	)
	if err != nil {
		if errors.Is(err, service.ErrNotebookSessionNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "notebook kernel capability is invalid or expired"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.T(c, developi18n.MsgEngineListFailed)})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, descriptors)
}

// CloseSession 关闭并保存 Notebook 交互会话。
// @Summary 关闭 Notebook 交互会话 | Close Notebook interactive session
// @Tags Notebook
// @Param id path int true "DevTask ID | DevTask ID"
// @Param session_id path string true "会话 ID | Session ID"
// @Success 204
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.update"]
// @Router /notebooks/{id}/sessions/{session_id} [delete]
func (h *NotebookHandler) CloseSession(c *gin.Context) {
	var uri struct {
		ID        uint   `uri:"id" binding:"required"`
		SessionID string `uri:"session_id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookInvalidID)})
		return
	}
	if err := h.notebookSessionService.Close(c.Request.Context(), tenantIDValue(c), userIDValue(c), uri.ID, uri.SessionID); err != nil {
		if errors.Is(err, service.ErrNotebookSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookSessionNotFound)})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookSessionCloseFailed, err.Error())})
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: service.NotebookSessionCookieName, Value: "", Path: "/api/v1/develop/notebook-sessions/" + uri.SessionID + "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
	c.Status(http.StatusNoContent)
}

func (h *NotebookHandler) ProxySession(c *gin.Context) {
	secret, err := c.Cookie(service.NotebookSessionCookieName)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	session, err := h.notebookSessionService.Resolve(c.Param("session_id"), secret)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	target, err := url.Parse(session.Endpoint)
	if err != nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		removeCookie(request, service.NotebookSessionCookieName)
		request.Header.Set("Authorization", "token "+session.RuntimeToken)
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		writer.WriteHeader(http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Del("X-Frame-Options")
		response.Header.Set("Content-Security-Policy", notebookFramePolicy(response.Header.Get("Content-Security-Policy")))
		return nil
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func (h *NotebookHandler) ShutdownSessions(ctx context.Context) {
	h.notebookSessionService.Shutdown(ctx)
}

func removeCookie(request *http.Request, name string) {
	cookies := request.Cookies()
	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != name {
			request.AddCookie(cookie)
		}
	}
}

func notebookFramePolicy(existing string) string {
	const frameAncestors = "frame-ancestors 'self' http://localhost:* https://localhost:* http://127.0.0.1:* https://127.0.0.1:*"
	directives := strings.Split(existing, ";")
	found := false
	for index, directive := range directives {
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(directive)), "frame-ancestors ") {
			directives[index] = " " + frameAncestors
			found = true
		}
	}
	if !found {
		directives = append(directives, " "+frameAncestors)
	}
	return strings.TrimSpace(strings.Join(directives, ";"))
}

func notebookKernelBearer(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], service.NotebookKernelCapabilityPrefix) {
		return "", false
	}
	return parts[1], true
}
