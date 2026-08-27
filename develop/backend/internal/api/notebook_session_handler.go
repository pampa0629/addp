package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
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
	userAccessToken, tokenErr := requestUserAccessToken(c)
	if tokenErr != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, developi18n.MsgAuthenticationRequired), "error_code": "authentication_required"})
		return
	}
	session, secret, err := h.notebookSessionService.Create(c.Request.Context(), userAccessToken, tenantIDValue(c), userIDValue(c), uri.ID)
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
	http.SetCookie(c.Writer, &http.Cookie{
		Name: service.NotebookCopilotSessionCookieName, Value: secret,
		Path:     "/api/v1/develop/notebook-copilot-sessions/" + session.ID + "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
		Expires: session.ExpiresAt, MaxAge: max(1, int(time.Until(session.ExpiresAt).Seconds())),
	})
	c.JSON(http.StatusCreated, session)
}

type notebookEngineCatalogChildrenRequest struct {
	EngineID uint                                  `json:"engine_id"`
	Path     commonClient.EngineCatalogPath        `json:"path"`
	Options  commonClient.EngineCatalogListOptions `json:"options,omitempty"`
}

type notebookTableScanRequest struct {
	EngineID  uint                           `json:"engine_id"`
	Path      commonClient.EngineCatalogPath `json:"path"`
	BatchSize int                            `json:"batch_size"`
	MaxRows   int64                          `json:"max_rows,omitempty"`
}

type notebookQueryRequest struct {
	EngineID uint          `json:"engine_id"`
	Language string        `json:"language"`
	Query    string        `json:"query"`
	Params   []interface{} `json:"params,omitempty"`
	MaxRows  int64         `json:"max_rows"`
	Timeout  int64         `json:"timeout"`
}

// StreamSessionQuery executes one bounded native read query as Arrow IPC.
// @Summary 执行 Notebook 有界只读查询 | Execute a bounded Notebook read query
// @Description language 必须与引擎 QueryRuntimeProvider 声明一致；查询必须携带显式 max_rows 与 timeout。 | Language must match the engine QueryRuntimeProvider declaration; the query requires explicit max_rows and timeout.
// @Tags Notebook
// @Accept json
// @Produce application/vnd.apache.arrow.stream
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookQueryRequest true "查询与执行边界 | Query and execution boundary"
// @Success 200 {file} binary
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/queries [post]
func (h *NotebookHandler) StreamSessionQuery(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookQueryRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.EngineID == 0 || strings.TrimSpace(request.Language) == "" || request.MaxRows <= 0 ||
		request.MaxRows > 1_000_000 || request.Timeout <= 0 || request.Timeout > 300 {
		respondDevelopCatalogError(c, http.StatusBadRequest, "query_request_invalid", developi18n.MsgNotebookEngineCatalogRequestInvalid)
		return
	}
	ready := false
	err := h.notebookSessionService.StreamQuery(c.Request.Context(), c.Param("session_id"), token,
		service.NotebookQueryRequest{
			EngineID: request.EngineID, Language: request.Language, Query: request.Query, Params: request.Params,
			MaxRows: request.MaxRows, Timeout: time.Duration(request.Timeout) * time.Second,
		}, c.Writer, func() {
			ready = true
			c.Header("Content-Type", "application/vnd.apache.arrow.stream")
			c.Header("Cache-Control", "no-store")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Status(http.StatusOK)
		})
	if err == nil || ready {
		return
	}
	if errors.Is(err, service.ErrNotebookSessionNotFound) {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	if errors.Is(err, service.ErrNotebookQueryInvalid) {
		respondDevelopCatalogError(c, http.StatusBadRequest, "query_request_invalid", developi18n.MsgNotebookEngineCatalogRequestInvalid)
		return
	}
	if errors.Is(err, service.ErrNotebookQueryUnsupported) {
		respondDevelopCatalogError(c, http.StatusUnprocessableEntity, "query_unsupported", developi18n.MsgNotebookEngineCatalogUnsupported)
		return
	}
	if code, exists := commonClient.SystemAPIErrorCode(err); exists {
		if code == "notebook_session_authorization_forbidden" || code == "execution_access_forbidden" {
			respondDevelopCatalogError(c, http.StatusForbidden, "notebook_data_forbidden", developi18n.MsgNotebookEngineCatalogForbidden)
			return
		}
		if code == "engine_unavailable" {
			respondDevelopCatalogError(c, http.StatusServiceUnavailable, code, developi18n.MsgNotebookEngineCatalogEngineUnavailable)
			return
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		respondDevelopCatalogError(c, http.StatusGatewayTimeout, "query_timeout", developi18n.MsgNotebookEngineCatalogTimeout)
		return
	}
	respondDevelopCatalogError(c, http.StatusBadGateway, "query_failed", developi18n.MsgNotebookEngineCatalogProviderFailed)
}

// StreamSessionTable streams one catalog-resolved table as Arrow IPC.
// @Summary 流式读取 Notebook 表 | Stream a Notebook table
// @Description 每次调用派生独立只读 Execution Authorization；连接只在 Develop 受控 Runtime 内使用。 | Each call derives an independent read-only Execution Authorization; connection details remain inside the controlled Develop runtime.
// @Tags Notebook
// @Accept json
// @Produce application/vnd.apache.arrow.stream
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookTableScanRequest true "表路径与批大小 | Table path and batch size"
// @Success 200 {file} binary
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/table-scans [post]
func (h *NotebookHandler) StreamSessionTable(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookTableScanRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.EngineID == 0 || request.BatchSize <= 0 ||
		request.BatchSize > 1_000_000 || request.MaxRows < 0 {
		respondDevelopCatalogError(c, http.StatusBadRequest, "table_scan_request_invalid", developi18n.MsgNotebookEngineCatalogRequestInvalid)
		return
	}
	ready := false
	err := h.notebookSessionService.StreamTable(c.Request.Context(), c.Param("session_id"), token,
		service.NotebookTableScanRequest{
			EngineID: request.EngineID, Path: request.Path, BatchSize: request.BatchSize, MaxRows: request.MaxRows,
		}, c.Writer, func() {
			ready = true
			c.Header("Content-Type", "application/vnd.apache.arrow.stream")
			c.Header("Cache-Control", "no-store")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Status(http.StatusOK)
		})
	if err == nil || ready {
		return
	}
	if errors.Is(err, service.ErrNotebookSessionNotFound) {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	if errors.Is(err, service.ErrNotebookTableScanInvalid) {
		respondDevelopCatalogError(c, http.StatusBadRequest, "table_scan_request_invalid", developi18n.MsgNotebookEngineCatalogRequestInvalid)
		return
	}
	if errors.Is(err, service.ErrNotebookTableScanUnsupported) {
		respondDevelopCatalogError(c, http.StatusUnprocessableEntity, "table_scan_unsupported", developi18n.MsgNotebookEngineCatalogUnsupported)
		return
	}
	if code, exists := commonClient.SystemAPIErrorCode(err); exists {
		switch code {
		case "notebook_session_authorization_forbidden", "execution_access_forbidden":
			respondDevelopCatalogError(c, http.StatusForbidden, "notebook_data_forbidden", developi18n.MsgNotebookEngineCatalogForbidden)
			return
		case "execution_authorization_conflict":
			respondDevelopCatalogError(c, http.StatusConflict, code, developi18n.MsgNotebookSessionConflict)
			return
		case "engine_unavailable":
			respondDevelopCatalogError(c, http.StatusServiceUnavailable, code, developi18n.MsgNotebookEngineCatalogEngineUnavailable)
			return
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		respondDevelopCatalogError(c, http.StatusGatewayTimeout, "table_scan_timeout", developi18n.MsgNotebookEngineCatalogTimeout)
		return
	}
	respondDevelopCatalogError(c, http.StatusBadGateway, "table_scan_failed", developi18n.MsgNotebookEngineCatalogProviderFailed)
}

// ListSessionEngineCatalogChildren returns live Catalog children for the current Kernel Session.
// @Summary 获取 Notebook Kernel 实时 Catalog 子节点 | List live Catalog children for a Notebook Kernel
// @Description 仅接受当前隔离 Kernel 的短期 Notebook Kernel Capability；Develop 使用 Session 绑定的 Notebook Session Authorization 调用 System。 | Only accepts the isolated Kernel's short-lived Notebook Kernel Capability; Develop calls System with the Session-bound Notebook Session Authorization.
// @Tags Notebook
// @Accept json
// @Produce json
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookEngineCatalogChildrenRequest true "Engine 与 Catalog 路径 | Engine and Catalog path"
// @Success 200 {object} commonClient.EngineCatalogListChildrenResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/catalog/children [post]
func (h *NotebookHandler) ListSessionEngineCatalogChildren(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookEngineCatalogChildrenRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.EngineID == 0 || request.Options.Limit <= 0 ||
		request.Options.Limit > 1000 || request.Options.Offset < 0 || request.Options.Recursive {
		respondDevelopCatalogError(c, http.StatusBadRequest, "engine_catalog_request_invalid", developi18n.MsgNotebookEngineCatalogRequestInvalid)
		return
	}
	nodes, err := h.notebookSessionService.ListEngineCatalogChildren(c.Request.Context(), c.Param("session_id"), token,
		commonClient.NotebookEngineCatalogChildrenRequest{EngineID: request.EngineID, Path: request.Path, Options: request.Options})
	if err != nil {
		if errors.Is(err, service.ErrNotebookSessionNotFound) {
			respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
			return
		}
		if code, exists := commonClient.SystemAPIErrorCode(err); exists {
			if code == "notebook_session_authorization_forbidden" {
				respondDevelopCatalogError(c, http.StatusForbidden, "notebook_engine_catalog_forbidden", developi18n.MsgNotebookEngineCatalogForbidden)
				return
			}
			if status, ok := commonClient.SystemAPIStatusCode(err); ok && isNotebookEngineCatalogProviderError(status, code) {
				respondDevelopCatalogError(c, status, code, notebookEngineCatalogMessageID(code))
				return
			}
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			respondDevelopCatalogError(c, http.StatusGatewayTimeout, "engine_catalog_timeout", developi18n.MsgNotebookEngineCatalogTimeout)
			return
		}
		respondDevelopCatalogError(c, http.StatusBadGateway, "engine_catalog_control_plane_failed", developi18n.MsgNotebookSessionControlPlaneFailed)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, commonClient.EngineCatalogListChildrenResponse{Nodes: nodes})
}

func isNotebookEngineCatalogProviderError(status int, code string) bool {
	return (status == http.StatusBadRequest && code == "engine_catalog_request_invalid") ||
		(status == http.StatusNotFound && (code == "engine_not_found" || code == "engine_catalog_entry_not_found")) ||
		(status == http.StatusUnprocessableEntity && code == "engine_catalog_operation_unsupported") ||
		(status == http.StatusBadGateway && code == "engine_catalog_provider_failed") ||
		(status == http.StatusServiceUnavailable && code == "engine_unavailable") ||
		(status == http.StatusGatewayTimeout && code == "engine_catalog_timeout")
}

func notebookEngineCatalogMessageID(code string) string {
	switch code {
	case "engine_catalog_request_invalid":
		return developi18n.MsgNotebookEngineCatalogRequestInvalid
	case "engine_not_found":
		return developi18n.MsgNotebookEngineCatalogEngineNotFound
	case "engine_catalog_entry_not_found":
		return developi18n.MsgNotebookEngineCatalogEntryNotFound
	case "engine_catalog_operation_unsupported":
		return developi18n.MsgNotebookEngineCatalogUnsupported
	case "engine_unavailable":
		return developi18n.MsgNotebookEngineCatalogEngineUnavailable
	case "engine_catalog_timeout":
		return developi18n.MsgNotebookEngineCatalogTimeout
	default:
		return developi18n.MsgNotebookEngineCatalogProviderFailed
	}
}

func respondDevelopCatalogError(c *gin.Context, status int, code, messageID string) {
	c.JSON(status, gin.H{"error": commoni18n.T(c, messageID), "error_code": code})
}

// ListSessionEngineDescriptors 返回当前 Notebook Kernel 可访问的数据引擎。
// @Summary 获取 Notebook Kernel 可用数据引擎 | List data engines available to a Notebook Kernel
// @Description 仅接受 Develop 注入当前隔离 Kernel 的短期 Notebook Kernel Capability Bearer；Develop 使用 Session Authorization 调用 System 完成授权复核和数据引擎发现。 | Only accepts the short-lived Notebook Kernel Capability Bearer injected by Develop into the isolated Kernel; Develop uses the Session Authorization to let System review authorization and discover data engines.
// @Tags Notebook
// @Produce json
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Success 200 {array} models.EngineRuntimeDescriptor "脱敏数据引擎描述列表 | Masked data engine descriptor list"
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
	http.SetCookie(c.Writer, &http.Cookie{
		Name: service.NotebookCopilotSessionCookieName, Value: "", Path: "/api/v1/develop/notebook-copilot-sessions/" + uri.SessionID + "/",
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
