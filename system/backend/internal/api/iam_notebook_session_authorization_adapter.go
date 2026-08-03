package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	systemservice "github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IAMIssueNotebookSessionAuthorizationRequest struct {
	SessionID string `json:"session_id"`
	TaskID    uint   `json:"task_id"`
	ExpiresIn int64  `json:"expires_in"`
}

type IAMNotebookSessionAuthorizationResponse struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TaskID    uint      `json:"task_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type IAMNotebookCatalogChildrenRequest struct {
	SessionID string                    `json:"session_id"`
	EngineID  uint                      `json:"engine_id"`
	Path      models.CatalogPath        `json:"path"`
	Options   models.CatalogListOptions `json:"options,omitempty"`
}

type IAMRevokeNotebookSessionAuthorizationRequest struct {
	SessionID string `json:"session_id"`
}

type IAMNotebookExecutionEngineAccessRequest struct {
	SessionID   string `json:"session_id"`
	ExecutionID string `json:"execution_id"`
	EngineID    uint   `json:"engine_id"`
	ExpiresIn   int64  `json:"expires_in"`
}

type iamNotebookSessionAuthorizationService interface {
	Issue(context.Context, iam.IssueNotebookSessionAuthorizationInput) (*iam.IssuedNotebookSessionAuthorization, error)
	Authorize(context.Context, iam.AuthorizeNotebookCatalogInput) (*iam.AuthorizedNotebookCatalog, error)
	DeriveExecutionEngineAccess(context.Context, iam.DeriveNotebookExecutionEngineAccessInput) (*iam.AuthorizedExecutionEngineAccess, error)
	Revoke(context.Context, iam.RevokeNotebookSessionAuthorizationInput) error
}

type notebookCatalogEngineResolver interface {
	GetForExecution(id, tenantID uint) (*models.Engine, error)
}

type notebookCatalogChildrenLister interface {
	ListCatalogChildren(context.Context, *models.Engine, models.CatalogListChildrenRequest) ([]models.CatalogEntry, error)
}

type IAMNotebookSessionAuthorizationHandler struct {
	service iamNotebookSessionAuthorizationService
	engines notebookCatalogEngineResolver
	catalog notebookCatalogChildrenLister
}

func NewIAMNotebookSessionAuthorizationHandler(
	authorizationService iamNotebookSessionAuthorizationService,
	engines notebookCatalogEngineResolver,
	catalog notebookCatalogChildrenLister,
) (*IAMNotebookSessionAuthorizationHandler, error) {
	if authorizationService == nil || engines == nil || catalog == nil {
		return nil, fmt.Errorf("%w: notebook session authorization handler dependencies are required", commonapi.ErrBadRequest)
	}
	return &IAMNotebookSessionAuthorizationHandler{
		service: authorizationService, engines: engines, catalog: catalog,
	}, nil
}

// Issue godoc
// @Summary      签发 Notebook 会话授权 | Issue Notebook session authorization
// @Description  从当前 Tenant User Access Token 派生绑定唯一 Notebook Session 和 Task 的短期会话授权事实 | Derive short-lived session authorization facts bound to one Notebook Session and Task from the current tenant user access token
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMIssueNotebookSessionAuthorizationRequest true "Notebook 会话边界 | Notebook session boundary"
// @Success      201 {object} IAMNotebookSessionAuthorizationResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine.read"]
// @Router       /auth/notebook-session-authorizations [post]
func (h *IAMNotebookSessionAuthorizationHandler) Issue(c *gin.Context) {
	var request IAMIssueNotebookSessionAuthorizationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization request", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookCatalogUUID(request.SessionID)
	if err != nil || request.TaskID == 0 || request.ExpiresIn <= 0 || request.ExpiresIn > 3600 {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization request", commonapi.ErrBadRequest))
		return
	}
	sourceAccessToken := iamBearerToken(c.GetHeader("Authorization"))
	if sourceAccessToken == "" {
		respondNotebookCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	issued, err := h.service.Issue(c.Request.Context(), iam.IssueNotebookSessionAuthorizationInput{
		SourceAccessToken: sourceAccessToken, SessionID: sessionID, TaskID: int64(request.TaskID),
		ExpiresIn: time.Duration(request.ExpiresIn) * time.Second,
		Audit:     iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMNotebookSessionAuthorizationResponse{
		ID: issued.ID.String(), SessionID: issued.SessionID.String(),
		TaskID: uint(issued.TaskID), ExpiresAt: issued.ExpiresAt.UTC(),
	})
}

// ListCatalogChildren godoc
// @Summary      使用 Notebook 会话授权列出实时 Catalog 子节点 | List live Catalog children with Notebook session authorization
// @Description  仅 addp-develop Tenant Service Principal 可消费绑定 Session 的用户派生授权；授权复核与 CatalogProvider.ListChildren 在同一请求完成 | Only the addp-develop tenant service principal may consume the session-bound user-derived authorization; authorization review and CatalogProvider.ListChildren complete in one request
// @Tags         Notebook 会话授权 | Notebook Session Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Notebook 会话授权 ID | Notebook session authorization ID"
// @Param        request body IAMNotebookCatalogChildrenRequest true "Session、Engine 与 Catalog 路径 | Session, Engine, and Catalog path"
// @Success      200 {object} models.CatalogListChildrenResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      404 {object} IAMErrorResponse
// @Failure      422 {object} IAMErrorResponse
// @Failure      502 {object} IAMErrorResponse
// @Failure      503 {object} IAMErrorResponse
// @Failure      504 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.notebook_session_authorization.execute"]
// @Router       /notebook-session-authorizations/{id}/catalog/children [post]
func (h *IAMNotebookSessionAuthorizationHandler) ListCatalogChildren(c *gin.Context) {
	authorizationID, err := parseCanonicalNotebookCatalogUUID(c.Param("id"))
	if err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization ID", commonapi.ErrBadRequest))
		return
	}
	var request IAMNotebookCatalogChildrenRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook catalog request", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookCatalogUUID(request.SessionID)
	if err != nil || validateNotebookCatalogRequest(request) != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook catalog request", commonapi.ErrBadRequest))
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondNotebookCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondNotebookCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	authorized, err := h.service.Authorize(c.Request.Context(), iam.AuthorizeNotebookCatalogInput{
		AuthorizationID: authorizationID, SessionID: sessionID,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	engine, err := h.engines.GetForExecution(request.EngineID, uint(authorized.TenantID))
	if err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	if err := requireNotebookCatalogCapability(engine); err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	nodes, err := h.catalog.ListCatalogChildren(c.Request.Context(), engine, models.CatalogListChildrenRequest{
		Path: request.Path, Options: request.Options,
	})
	if err != nil {
		respondNotebookCatalogProviderError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, models.CatalogListChildrenResponse{Nodes: nodes})
}

// DeriveExecutionEngineAccess godoc
// @Summary      派生 Notebook 单次只读执行访问 | Derive one Notebook read execution access
// @Description  为新的 execution 原子创建只读 Execution Authorization 并返回仅供 addp-develop 受控 Runtime 使用的引擎访问 | Atomically create a read-only Execution Authorization for a new execution and return engine access only to the controlled addp-develop runtime
// @Tags         Notebook 会话授权 | Notebook Session Authorization
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Notebook 会话授权 ID | Notebook session authorization ID"
// @Param        request body IAMNotebookExecutionEngineAccessRequest true "Session、execution 与 Engine 边界 | Session, execution, and Engine boundary"
// @Success      201 {object} IAMExecutionEngineAccessResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      401 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @Failure      503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.notebook_session_authorization.execute"]
// @Router       /notebook-session-authorizations/{id}/execution-engine-accesses [post]
func (h *IAMNotebookSessionAuthorizationHandler) DeriveExecutionEngineAccess(c *gin.Context) {
	authorizationID, err := parseCanonicalNotebookCatalogUUID(c.Param("id"))
	if err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization ID", commonapi.ErrBadRequest))
		return
	}
	var request IAMNotebookExecutionEngineAccessRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || request.EngineID == 0 ||
		request.ExpiresIn <= 0 {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook execution engine access request", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookCatalogUUID(request.SessionID)
	if err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook execution engine access request", commonapi.ErrBadRequest))
		return
	}
	executionID, err := parseCanonicalExecutionUUID(request.ExecutionID)
	if err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondNotebookCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondNotebookCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	access, err := h.service.DeriveExecutionEngineAccess(c.Request.Context(), iam.DeriveNotebookExecutionEngineAccessInput{
		AuthorizationID: authorizationID, SessionID: sessionID, ExecutionID: executionID,
		EngineID: int64(request.EngineID), ExpiresIn: time.Duration(request.ExpiresIn) * time.Second,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	engine, err := h.engines.GetForExecution(uint(access.EngineID), uint(access.TenantID))
	if err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, IAMExecutionEngineAccessResponse{
		AuthorizationID: strconv.FormatInt(access.AuthorizationID, 10),
		ExecutionID:     access.ExecutionID.String(), Audience: access.Audience,
		EngineID: strconv.FormatInt(access.EngineID, 10), Effects: append([]string(nil), access.Effects...),
		ExpiresAt: access.ExpiresAt.UTC(), Engine: engine,
	})
}

// Revoke godoc
// @Summary      撤销 Notebook 会话授权 | Revoke Notebook session authorization
// @Description  addp-develop 关闭 Session 时幂等撤销其会话授权 | Idempotently revoke the session authorization when addp-develop closes the Session
// @Tags         Notebook 会话授权 | Notebook Session Authorization
// @Accept       json
// @Security     BearerAuth
// @Param        id path string true "Notebook 会话授权 ID | Notebook session authorization ID"
// @Param        request body IAMRevokeNotebookSessionAuthorizationRequest true "Notebook Session | Notebook Session"
// @Success      204
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.notebook_session_authorization.execute"]
// @Router       /notebook-session-authorizations/{id}/revocations [post]
func (h *IAMNotebookSessionAuthorizationHandler) Revoke(c *gin.Context) {
	authorizationID, err := parseCanonicalNotebookCatalogUUID(c.Param("id"))
	if err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook session authorization ID", commonapi.ErrBadRequest))
		return
	}
	var request IAMRevokeNotebookSessionAuthorizationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook catalog revocation", commonapi.ErrBadRequest))
		return
	}
	sessionID, err := parseCanonicalNotebookCatalogUUID(request.SessionID)
	if err != nil {
		respondNotebookCatalogError(c, fmt.Errorf("%w: invalid notebook catalog revocation", commonapi.ErrBadRequest))
		return
	}
	principalID, tenantID, principalType, err := iamTenantActor(c)
	if err != nil || principalType != string(iam.PrincipalTypeServicePrincipal) {
		respondNotebookCatalogError(c, iam.ErrNotebookSessionAuthorizationForbidden)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists || authContext.Client.ClientID == nil {
		respondNotebookCatalogError(c, commonapi.ErrUnauthorized)
		return
	}
	if err := h.service.Revoke(c.Request.Context(), iam.RevokeNotebookSessionAuthorizationInput{
		AuthorizationID: authorizationID, SessionID: sessionID,
		ServicePrincipalID: int64(principalID), ServiceClientID: *authContext.Client.ClientID,
		TenantID: int64(tenantID), Audit: iamAuditMetadataWithStatus(c, http.StatusNoContent),
	}); err != nil {
		respondNotebookCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func validateNotebookCatalogRequest(request IAMNotebookCatalogChildrenRequest) error {
	if request.EngineID == 0 || request.Options.Limit <= 0 || request.Options.Limit > 1000 ||
		request.Options.Offset < 0 || request.Options.Recursive {
		return commonapi.ErrBadRequest
	}
	if request.Path.EngineID != 0 && request.Path.EngineID != request.EngineID {
		return commonapi.ErrBadRequest
	}
	if len(request.Path.Segments) > 0 && request.Path.Version != engineplugin.CatalogPathVersion {
		return commonapi.ErrBadRequest
	}
	return nil
}

func requireNotebookCatalogCapability(engine *models.Engine) error {
	if engine == nil || engine.Capabilities == nil {
		return engineplugin.WrapCatalogError(engineplugin.CatalogErrorUnsupported, errors.New("engine has no catalog capability"))
	}
	capabilities, err := engineplugin.ParseEngineCapabilities(string(*engine.Capabilities))
	if err != nil || capabilities.Storage == nil || capabilities.Storage.Catalog == nil ||
		!capabilities.Storage.Catalog.Supported || capabilities.Storage.CatalogModel == nil {
		return engineplugin.WrapCatalogError(engineplugin.CatalogErrorUnsupported, errors.New("engine has no supported catalog model"))
	}
	return nil
}

func parseCanonicalNotebookCatalogUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return uuid.Nil, fmt.Errorf("%w: invalid UUID", commonapi.ErrBadRequest)
	}
	return parsed, nil
}

func respondNotebookCatalogError(c *gin.Context, err error) {
	respondNotebookCatalogErrorWithDefault(
		c, err, sysi18n.MsgCatalogControlPlaneFailed, "catalog_control_plane_failed",
	)
}

func respondNotebookCatalogProviderError(c *gin.Context, err error) {
	respondNotebookCatalogErrorWithDefault(
		c, err, sysi18n.MsgCatalogProviderFailed, "catalog_provider_failed",
	)
}

func respondNotebookCatalogErrorWithDefault(c *gin.Context, err error, defaultMessageID, defaultErrorCode string) {
	status := http.StatusBadGateway
	messageID := defaultMessageID
	errorCode := defaultErrorCode
	switch {
	case errors.Is(err, iam.ErrNotebookSessionAuthorizationConflict):
		status = http.StatusConflict
		messageID = sysi18n.MsgNotebookSessionAuthorizationConflict
		errorCode = "notebook_session_authorization_conflict"
	case errors.Is(err, iam.ErrNotebookSessionAuthorizationForbidden):
		status = http.StatusForbidden
		messageID = sysi18n.MsgNotebookSessionAuthorizationForbidden
		errorCode = "notebook_session_authorization_forbidden"
	case errors.Is(err, iam.ErrExecutionAuthorizationConflict):
		status = http.StatusConflict
		messageID = sysi18n.MsgNotebookSessionAuthorizationConflict
		errorCode = "execution_authorization_conflict"
	case errors.Is(err, iam.ErrExecutionAuthorizationPermissionDenied):
		status = http.StatusForbidden
		messageID = commoni18n.MsgForbidden
		errorCode = "execution_access_forbidden"
	case errors.Is(err, iam.ErrExecutionAuthorizationUnavailable):
		status = http.StatusServiceUnavailable
		messageID = sysi18n.MsgCatalogEngineUnavailable
		errorCode = "engine_unavailable"
	case errors.Is(err, commonapi.ErrBadRequest), engineplugin.IsCatalogErrorKind(err, engineplugin.CatalogErrorInvalidPath):
		status = http.StatusBadRequest
		messageID = commoni18n.MsgInvalidParams
		errorCode = "catalog_request_invalid"
	case errors.Is(err, commonapi.ErrUnauthorized):
		status = http.StatusUnauthorized
		messageID = commoni18n.MsgUnauthorized
		errorCode = "authentication_required"
	case errors.Is(err, commonapi.ErrForbidden):
		status = http.StatusForbidden
		messageID = commoni18n.MsgForbidden
		errorCode = "permission_denied"
	case errors.Is(err, systemservice.ErrResourceNotFound), errors.Is(err, systemservice.ErrResourceForbidden):
		status = http.StatusNotFound
		messageID = sysi18n.MsgCatalogEngineNotFound
		errorCode = "engine_not_found"
	case engineplugin.IsCatalogErrorKind(err, engineplugin.CatalogErrorNotFound):
		status = http.StatusNotFound
		messageID = sysi18n.MsgCatalogEntryNotFound
		errorCode = "catalog_entry_not_found"
	case engineplugin.IsCatalogErrorKind(err, engineplugin.CatalogErrorUnsupported):
		status = http.StatusUnprocessableEntity
		messageID = sysi18n.MsgCatalogOperationUnsupported
		errorCode = "catalog_operation_unsupported"
	case engineplugin.IsCatalogErrorKind(err, engineplugin.CatalogErrorUnavailable):
		status = http.StatusServiceUnavailable
		messageID = sysi18n.MsgCatalogEngineUnavailable
		errorCode = "engine_unavailable"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		status = http.StatusGatewayTimeout
		messageID = sysi18n.MsgCatalogTimeout
		errorCode = "catalog_timeout"
	}
	c.JSON(status, IAMErrorResponse{Error: commoni18n.T(c, messageID), ErrorCode: &errorCode})
}
