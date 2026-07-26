package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/logger"
	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
)

type IAMOAuthHandler struct {
	provider *iamoauth.Provider
	bridge   *iamoauth.ConsentBridge
}

type IAMOAuthAuthorizationRequestResponse struct {
	RequestID     string `json:"request_id"`
	RequestSecret string `json:"request_secret"`
	ExpiresIn     int    `json:"expires_in"`
}

type IAMOAuthAuthorizationRequestView struct {
	RequestID  string    `json:"request_id"`
	ClientID   string    `json:"client_id"`
	ClientName string    `json:"client_name"`
	Scope      string    `json:"scope"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type IAMOAuthAuthorizationDecisionRequest struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
}

type IAMOAuthAuthorizationDecisionResponse struct {
	RedirectURL string `json:"redirect_url"`
}

type IAMOAuthDeviceDecisionRequest struct {
	UserCode string `json:"user_code"`
	Approve  bool   `json:"approve"`
}

func NewIAMOAuthHandler(provider *iamoauth.Provider, bridge *iamoauth.ConsentBridge) (*IAMOAuthHandler, error) {
	if provider == nil || provider.OAuth2 == nil || bridge == nil {
		return nil, errors.New("IAM OAuth Handler 依赖不能为空")
	}
	return &IAMOAuthHandler{provider: provider, bridge: bridge}, nil
}

// CreateAuthorizationRequest godoc
// @Summary      创建 OAuth 授权请求 | Create OAuth authorization request
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Success      201 {object} IAMOAuthAuthorizationRequestResponse
// @x-addp-auth-mode "public"
// @Router       /oauth/authorization_requests [post]
func (h *IAMOAuthHandler) CreateAuthorizationRequest(c *gin.Context) {
	created, err := h.bridge.CreateAuthorizationRequest(c.Request.Context(), iamoauth.AuthorizationRequestInput{
		ClientID:            c.PostForm("client_id"),
		RedirectURI:         c.PostForm("redirect_uri"),
		Scope:               c.PostForm("scope"),
		CodeChallenge:       c.PostForm("code_challenge"),
		CodeChallengeMethod: c.PostForm("code_challenge_method"),
		Audit:               iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		setIAMOAuthFailure(c, "oauth.authorization_request.failed", inputOAuthClientID(c), "", "", c.PostForm("scope"), err)
		respondIAMOAuthBridgeError(c, err)
		return
	}
	middleware.MarkOAuthSecurityAuditPersisted(c)
	c.JSON(http.StatusCreated, IAMOAuthAuthorizationRequestResponse{
		RequestID:     created.RequestID,
		RequestSecret: created.RequestSecret,
		ExpiresIn:     created.ExpiresIn,
	})
}

// GetAuthorizationRequest godoc
// @Summary      查询 OAuth 授权请求 | Get OAuth authorization request
// @Tags         OAuth
// @Produce      json
// @Security     BearerAuth
// @Param        request_id path string true "授权请求 ID | Authorization request ID"
// @Success      200 {object} IAMOAuthAuthorizationRequestView
// @x-addp-auth-mode "self"
// @Router       /oauth/authorization_requests/{request_id} [get]
func (h *IAMOAuthHandler) GetAuthorizationRequest(c *gin.Context) {
	view, err := h.bridge.GetAuthorizationRequest(c.Request.Context(), c.Param("request_id"))
	if err != nil {
		respondIAMOAuthBridgeError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMOAuthAuthorizationRequestView{
		RequestID:  view.RequestID,
		ClientID:   view.ClientID,
		ClientName: view.ClientName,
		Scope:      view.Scope,
		ExpiresAt:  view.ExpiresAt.UTC(),
	})
}

// CancelAuthorizationRequest godoc
// @Summary      取消 OAuth 授权请求 | Cancel OAuth authorization request
// @Tags         OAuth
// @Security     BearerAuth
// @Param        request_id path string true "授权请求 ID | Authorization request ID"
// @Success      204
// @x-addp-auth-mode "public"
// @Router       /oauth/authorization_requests/{request_id} [delete]
func (h *IAMOAuthHandler) CancelAuthorizationRequest(c *gin.Context) {
	requestSecret := bearerCredential(c.GetHeader("Authorization"))
	if requestSecret == "" {
		setIAMOAuthFailure(c, "oauth.authorization_request.failed", "", "", "", "", fosite.ErrInvalidRequest)
		respondIAMOAuthBridgeError(c, fosite.ErrInvalidRequest)
		return
	}
	if _, err := h.bridge.CancelAuthorizationRequest(
		c.Request.Context(),
		c.Param("request_id"),
		requestSecret,
		iamAuditMetadataWithStatus(c, http.StatusNoContent),
	); err != nil {
		setIAMOAuthFailure(c, "oauth.authorization_request.failed", "", "", "", "", err)
		respondIAMOAuthBridgeError(c, err)
		return
	}
	middleware.MarkOAuthSecurityAuditPersisted(c)
	c.Status(http.StatusNoContent)
}

// Authorize godoc
// @Summary      决定 OAuth 授权 | Decide OAuth authorization
// @Tags         OAuth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMOAuthAuthorizationDecisionRequest true "授权决定 | Authorization decision"
// @Success      200 {object} IAMOAuthAuthorizationDecisionResponse
// @x-addp-auth-mode "self"
// @Router       /oauth/authorizations [post]
func (h *IAMOAuthHandler) Authorize(c *gin.Context) {
	var request IAMOAuthAuthorizationDecisionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		setIAMOAuthFailure(c, "oauth.authorization.failed", "", "", request.Decision, "", commonapi.ErrBadRequest)
		respondIAMOAuthBridgeError(c, commonapi.ErrBadRequest)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists {
		setIAMOAuthFailure(c, "oauth.authorization.failed", "", "", request.Decision, "", commonapi.ErrUnauthorized)
		respondIAMOAuthBridgeError(c, commonapi.ErrUnauthorized)
		return
	}
	result, err := h.bridge.DecideAuthorization(
		c.Request.Context(),
		request.RequestID,
		iamoauth.AuthorizationDecision(request.Decision),
		authContext,
		iamAuditMetadataWithStatus(c, http.StatusOK),
	)
	if err != nil {
		logger.L().Warn("OAuth authorization decision failed", "error", err)
		setIAMOAuthFailure(c, "oauth.authorization.failed", "", "", request.Decision, "", err)
		respondIAMOAuthBridgeError(c, err)
		return
	}
	middleware.MarkOAuthSecurityAuditPersisted(c)
	c.JSON(http.StatusOK, IAMOAuthAuthorizationDecisionResponse{RedirectURL: result.RedirectURL})
}

// DeviceCode godoc
// @Summary      创建设备授权 | Create device authorization
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Success      200 {object} object
// @x-addp-auth-mode "public"
// @Router       /oauth/device/code [post]
func (h *IAMOAuthHandler) DeviceCode(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil || len(fosite.GetAudiences(c.Request.PostForm)) == 0 {
		setIAMOAuthFailure(c, "oauth.device.code.failed", inputOAuthClientID(c), "", "", c.PostForm("scope"), fosite.ErrInvalidRequest)
		h.provider.OAuth2.WriteAccessError(c.Request.Context(), c.Writer, nil, fosite.ErrInvalidRequest)
		return
	}
	txCtx, err := h.provider.Storage.BeginTX(c.Request.Context())
	if err != nil {
		setIAMOAuthFailure(c, "oauth.device.code.failed", inputOAuthClientID(c), "", "", c.PostForm("scope"), err)
		h.provider.OAuth2.WriteAccessError(c.Request.Context(), c.Writer, nil, err)
		return
	}
	requester, err := h.provider.OAuth2.NewDeviceRequest(txCtx, c.Request.WithContext(txCtx))
	if err != nil {
		_ = h.provider.Storage.Rollback(txCtx)
		setIAMOAuthFailure(c, "oauth.device.code.failed", inputOAuthClientID(c), "", "", c.PostForm("scope"), err)
		h.provider.OAuth2.WriteAccessError(c.Request.Context(), c.Writer, requester, err)
		return
	}
	response, err := h.provider.OAuth2.NewDeviceResponse(txCtx, requester, iamoauth.NewIAMSession())
	if err != nil {
		_ = h.provider.Storage.Rollback(txCtx)
		setIAMOAuthFailure(c, "oauth.device.code.failed", inputOAuthClientID(c), "", "", c.PostForm("scope"), err)
		h.provider.OAuth2.WriteAccessError(c.Request.Context(), c.Writer, requester, err)
		return
	}
	if err := h.provider.Storage.WriteAudit(txCtx, oauthSuccessAudit(c, http.StatusOK, iam.AuditEvent{
		EventName:  "oauth.device.code.issued",
		Result:     iam.AuditResultSucceeded,
		RiskLevel:  iam.AuditRiskLow,
		ModuleName: "system",
		EntityType: "oauth_security_event",
		EntityID:   "oauth.device.code.issued",
		Details: map[string]any{
			"client_id": requester.GetClient().GetID(),
			"scope":     strings.Join(requester.GetRequestedScopes(), " "),
		},
	})); err != nil {
		_ = h.provider.Storage.Rollback(txCtx)
		setIAMOAuthFailure(c, "oauth.device.code.failed", inputOAuthClientID(c), "", "", c.PostForm("scope"), err)
		h.provider.OAuth2.WriteAccessError(c.Request.Context(), c.Writer, requester, err)
		return
	}
	if err := h.provider.Storage.Commit(txCtx); err != nil {
		setIAMOAuthFailure(c, "oauth.device.code.failed", inputOAuthClientID(c), "", "", c.PostForm("scope"), err)
		h.provider.OAuth2.WriteAccessError(c.Request.Context(), c.Writer, requester, err)
		return
	}
	middleware.MarkOAuthSecurityAuditPersisted(c)
	h.provider.OAuth2.WriteDeviceResponse(c.Request.Context(), c.Writer, requester, response)
}

// DecideDeviceAuthorization godoc
// @Summary      决定设备授权 | Decide device authorization
// @Tags         OAuth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMOAuthDeviceDecisionRequest true "设备授权决定 | Device authorization decision"
// @Success      204
// @x-addp-auth-mode "self"
// @Router       /oauth/device/authorizations [post]
func (h *IAMOAuthHandler) DecideDeviceAuthorization(c *gin.Context) {
	var request IAMOAuthDeviceDecisionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || strings.TrimSpace(request.UserCode) == "" {
		setIAMOAuthFailure(c, "oauth.device.authorization.failed", "", "", deviceDecisionName(request.Approve), "", commonapi.ErrBadRequest)
		respondIAMOAuthBridgeError(c, commonapi.ErrBadRequest)
		return
	}
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists {
		setIAMOAuthFailure(c, "oauth.device.authorization.failed", "", "", deviceDecisionName(request.Approve), "", commonapi.ErrUnauthorized)
		respondIAMOAuthBridgeError(c, commonapi.ErrUnauthorized)
		return
	}
	if err := h.bridge.DecideDeviceAuthorization(c.Request.Context(), iamoauth.DeviceAuthorizationDecisionInput{
		UserCode:    request.UserCode,
		Approve:     request.Approve,
		AuthContext: authContext,
		Audit:       iamAuditMetadataWithStatus(c, http.StatusNoContent),
	}); err != nil {
		setIAMOAuthFailure(c, "oauth.device.authorization.failed", "", "", deviceDecisionName(request.Approve), "", err)
		respondIAMOAuthBridgeError(c, err)
		return
	}
	middleware.MarkOAuthSecurityAuditPersisted(c)
	c.Status(http.StatusNoContent)
}

// Token godoc
// @Summary      兑换 OAuth Token | Exchange OAuth token
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Success      200 {object} object
// @x-addp-auth-mode "public"
// @Router       /oauth/token [post]
func (h *IAMOAuthHandler) Token(c *gin.Context) {
	clientID := c.Request.FormValue("client_id")
	grantType := c.Request.FormValue("grant_type")
	scope := c.Request.FormValue("scope")
	auditContext := iamoauth.WithTransactionAudit(c.Request.Context(), oauthSuccessAudit(c, http.StatusOK, iam.AuditEvent{
		EventName:  "oauth.token.issued",
		Result:     iam.AuditResultSucceeded,
		RiskLevel:  iam.AuditRiskMedium,
		ModuleName: "system",
		EntityType: "oauth_security_event",
		EntityID:   "oauth.token.issued",
		Details: map[string]any{
			"client_id":  clientID,
			"grant_type": grantType,
			"scope":      scope,
		},
	}))
	request := c.Request.WithContext(auditContext)
	requester, err := h.provider.OAuth2.NewAccessRequest(auditContext, request, iamoauth.NewIAMSession())
	if err != nil {
		markCommittedTransactionAudit(c, auditContext)
		if !middleware.OAuthSecurityAuditWasPersisted(c) {
			setIAMOAuthFailure(c, "oauth.token.failed", clientID, grantType, "", scope, err)
		}
		h.provider.OAuth2.WriteAccessError(auditContext, c.Writer, requester, err)
		return
	}
	response, err := h.provider.OAuth2.NewAccessResponse(auditContext, requester)
	if err != nil {
		markCommittedTransactionAudit(c, auditContext)
		if !middleware.OAuthSecurityAuditWasPersisted(c) {
			setIAMOAuthFailure(c, "oauth.token.failed", clientID, grantType, "", scope, err)
		}
		h.provider.OAuth2.WriteAccessError(auditContext, c.Writer, requester, err)
		return
	}
	markCommittedTransactionAudit(c, auditContext)
	h.provider.OAuth2.WriteAccessResponse(auditContext, c.Writer, requester, response)
}

// Revoke godoc
// @Summary      撤销 OAuth Token | Revoke OAuth token
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Success      200
// @x-addp-auth-mode "public"
// @Router       /oauth/revoke [post]
func (h *IAMOAuthHandler) Revoke(c *gin.Context) {
	txCtx, err := h.provider.Storage.BeginTX(c.Request.Context())
	if err != nil {
		setIAMOAuthFailure(c, "oauth.token.revoke_ignored", inputOAuthClientID(c), "", "", "", err)
		h.provider.OAuth2.WriteRevocationResponse(c.Request.Context(), c.Writer, err)
		return
	}
	err = h.provider.OAuth2.NewRevocationRequest(txCtx, c.Request.WithContext(txCtx))
	eventName := "oauth.token.revoked"
	result := iam.AuditResultSucceeded
	if errors.Is(err, fosite.ErrInvalidRequest) {
		eventName = "oauth.token.revoke_ignored"
		result = iam.AuditResultIgnored
	} else if err != nil {
		_ = h.provider.Storage.Rollback(txCtx)
		setIAMOAuthFailure(c, "oauth.token.revoke_ignored", inputOAuthClientID(c), "", "", "", err)
		h.provider.OAuth2.WriteRevocationResponse(c.Request.Context(), c.Writer, err)
		return
	}
	if auditErr := h.provider.Storage.WriteAudit(txCtx, oauthSuccessAudit(c, http.StatusOK, iam.AuditEvent{
		EventName:  eventName,
		Result:     result,
		RiskLevel:  iam.AuditRiskMedium,
		ModuleName: "system",
		EntityType: "oauth_security_event",
		EntityID:   eventName,
		Details: map[string]any{
			"client_id": c.Request.FormValue("client_id"),
		},
	})); auditErr != nil {
		_ = h.provider.Storage.Rollback(txCtx)
		setIAMOAuthFailure(c, "oauth.token.revoke_ignored", inputOAuthClientID(c), "", "", "", auditErr)
		h.provider.OAuth2.WriteRevocationResponse(c.Request.Context(), c.Writer, auditErr)
		return
	}
	if commitErr := h.provider.Storage.Commit(txCtx); commitErr != nil {
		setIAMOAuthFailure(c, "oauth.token.revoke_ignored", inputOAuthClientID(c), "", "", "", commitErr)
		h.provider.OAuth2.WriteRevocationResponse(c.Request.Context(), c.Writer, commitErr)
		return
	}
	middleware.MarkOAuthSecurityAuditPersisted(c)
	h.provider.OAuth2.WriteRevocationResponse(c.Request.Context(), c.Writer, err)
}

func oauthSuccessAudit(c *gin.Context, status int, event iam.AuditEvent) iam.AuditEvent {
	event.Metadata = iamAuditMetadataWithStatus(c, status)
	return event
}

func markCommittedTransactionAudit(c *gin.Context, ctx context.Context) {
	if _, committed := iamoauth.TransactionAuditCommitted(ctx); committed {
		middleware.MarkOAuthSecurityAuditPersisted(c)
	}
}

func setIAMOAuthFailure(
	c *gin.Context,
	eventName string,
	clientID string,
	grantType string,
	decision string,
	scope string,
	err error,
) {
	result := "failed"
	if errors.Is(err, commonapi.ErrUnauthorized) || errors.Is(err, commonapi.ErrForbidden) ||
		errors.Is(err, fosite.ErrAccessDenied) || errors.Is(err, fosite.ErrInvalidGrant) {
		result = "denied"
	}
	middleware.SetOAuthSecurityAudit(
		c,
		eventName,
		result,
		clientID,
		grantType,
		decision,
		scope,
		iamOAuthErrorCode(err),
	)
}

func iamOAuthErrorCode(err error) string {
	switch {
	case errors.Is(err, commonapi.ErrUnauthorized):
		return "authentication_required"
	case errors.Is(err, commonapi.ErrForbidden):
		return "access_denied"
	case errors.Is(err, commonapi.ErrBadRequest), errors.Is(err, commonapi.ErrConflict),
		errors.Is(err, commonapi.ErrNotFound):
		return "invalid_request"
	case err == nil:
		return "server_error"
	default:
		return fosite.ErrorToRFC6749Error(err).Error()
	}
}

func inputOAuthClientID(c *gin.Context) string {
	return strings.TrimSpace(c.PostForm("client_id"))
}

func deviceDecisionName(approve bool) string {
	if approve {
		return "approve"
	}
	return "reject"
}

func respondIAMOAuthBridgeError(c *gin.Context, err error) {
	var oauthError *fosite.RFC6749Error
	if errors.As(err, &oauthError) {
		status := oauthError.StatusCode()
		if status < 400 || status > 599 {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": oauthError.Error()})
		return
	}
	status := commonapi.MapErrorToHTTPStatus(err)
	if errors.Is(err, commonapi.ErrNotFound) {
		status = http.StatusGone
	}
	errorCode := "server_error"
	switch {
	case errors.Is(err, commonapi.ErrBadRequest):
		errorCode = "invalid_request"
	case errors.Is(err, commonapi.ErrUnauthorized):
		errorCode = "invalid_request"
	case errors.Is(err, commonapi.ErrForbidden):
		errorCode = "access_denied"
	case errors.Is(err, commonapi.ErrConflict), errors.Is(err, commonapi.ErrNotFound):
		errorCode = "invalid_request"
	}
	c.JSON(status, gin.H{"error": errorCode})
}

func bearerCredential(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
