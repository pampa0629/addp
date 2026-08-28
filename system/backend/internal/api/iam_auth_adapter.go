package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

const iamRefreshCookieName = "addp_refresh_token"

type iamBrowserLoginService interface {
	LoginLocalBrowser(context.Context, iam.LoginLocalBrowserInput) (*iam.ContextSelectionResult, error)
	VerifyLocalBrowserMFA(context.Context, iam.VerifyMFAChallengeInput) (*iam.ContextSelectionResult, error)
}

type iamContextSelectionService interface {
	ConsumeContextSelection(context.Context, iam.ConsumeContextSelectionInput) (*iam.IssuedBrowserSession, error)
}

type iamAuthContextService interface {
	ResolveAuthContext(context.Context, string) (*commonauth.AuthContext, error)
}

type iamContextOptionsService interface {
	ListBrowserContextOptions(context.Context, string) ([]iam.BrowserContextOption, error)
}

type iamContextSwitchService interface {
	SwitchBrowserContext(context.Context, iam.SwitchBrowserContextInput) (*iam.IssuedBrowserSession, error)
}

type iamRefreshService interface {
	RotateBrowserRefreshToken(context.Context, iam.RotateBrowserRefreshTokenInput) (*iam.IssuedBrowserSession, error)
}

type iamLogoutService interface {
	LogoutBrowserSession(context.Context, iam.LogoutBrowserSessionInput) error
}

type IAMAuthHandlerConfig struct {
	SecureCookies        bool
	ResourceTicketOwners []string
	Now                  func() time.Time
}

type IAMAuthHandler struct {
	loginService            iamBrowserLoginService
	contextSelectionService iamContextSelectionService
	authContextService      iamAuthContextService
	contextOptionsService   iamContextOptionsService
	contextSwitchService    iamContextSwitchService
	refreshService          iamRefreshService
	logoutService           iamLogoutService
	secureCookies           bool
	resourceTicketOwners    []string
	now                     func() time.Time
}

func NewIAMAuthHandler(
	loginService iamBrowserLoginService,
	contextSelectionService iamContextSelectionService,
	authContextService iamAuthContextService,
	contextOptionsService iamContextOptionsService,
	contextSwitchService iamContextSwitchService,
	refreshService iamRefreshService,
	logoutService iamLogoutService,
	config IAMAuthHandlerConfig,
) (*IAMAuthHandler, error) {
	if loginService == nil || contextSelectionService == nil || authContextService == nil ||
		contextOptionsService == nil || contextSwitchService == nil || refreshService == nil || logoutService == nil {
		return nil, fmt.Errorf("%w: IAM auth handler dependencies are required", commonapi.ErrBadRequest)
	}
	owners, err := normalizeIAMResourceTicketOwners(config.ResourceTicketOwners)
	if err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &IAMAuthHandler{
		loginService:            loginService,
		contextSelectionService: contextSelectionService,
		authContextService:      authContextService,
		contextOptionsService:   contextOptionsService,
		contextSwitchService:    contextSwitchService,
		refreshService:          refreshService,
		logoutService:           logoutService,
		secureCookies:           config.SecureCookies,
		resourceTicketOwners:    owners,
		now:                     config.Now,
	}, nil
}

// Login godoc
// @Summary      本地账号登录 | Sign in with local account
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Param        request body IAMBrowserLoginRequest true "登录凭据 | Login credentials"
// @Success      200 {object} IAMBrowserLoginResponse
// @x-addp-auth-mode "public"
// @Router       /login [post]
func (h *IAMAuthHandler) Login(c *gin.Context) {
	var request IAMBrowserLoginRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil ||
		strings.TrimSpace(request.Username) == "" || request.Password == "" {
		respondIAMError(c, fmt.Errorf("%w: invalid login request", commonapi.ErrBadRequest))
		return
	}
	result, err := h.loginService.LoginLocalBrowser(c.Request.Context(), iam.LoginLocalBrowserInput{
		Username: request.Username,
		Password: request.Password,
		Audit:    iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	response, session, err := h.mapBrowserLoginResult(result)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if session != nil {
		if err := h.setBrowserSessionCookies(c, session); err != nil {
			respondIAMError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, response)
}

// VerifyMFA godoc
// @Summary      完成本地账号增强认证 | Complete local account MFA
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Param        request body IAMMFAVerificationRequest true "TOTP 验证 | TOTP verification"
// @Success      200 {object} IAMBrowserLoginResponse
// @Failure      401 {object} IAMErrorResponse
// @x-addp-auth-mode "public"
// @Router       /auth/mfa-verifications [post]
func (h *IAMAuthHandler) VerifyMFA(c *gin.Context) {
	var request IAMMFAVerificationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil ||
		strings.TrimSpace(request.ChallengeToken) == "" || strings.TrimSpace(request.Code) == "" {
		respondIAMError(c, fmt.Errorf("%w: invalid MFA verification request", commonapi.ErrBadRequest))
		return
	}
	result, err := h.loginService.VerifyLocalBrowserMFA(c.Request.Context(), iam.VerifyMFAChallengeInput{
		ChallengeToken: request.ChallengeToken,
		Code:           request.Code,
		Audit:          iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	response, session, err := h.mapBrowserLoginResult(result)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if session != nil {
		if err := h.setBrowserSessionCookies(c, session); err != nil {
			respondIAMError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, response)
}

// ConsumeContextSelection godoc
// @Summary      选择登录上下文 | Select sign-in context
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Param        request body IAMContextSelectionRequest true "上下文选择 | Context selection"
// @Success      200 {object} IAMAccessTokenResponse
// @x-addp-auth-mode "public"
// @Router       /auth/context-selections [post]
func (h *IAMAuthHandler) ConsumeContextSelection(c *gin.Context) {
	var request IAMContextSelectionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || strings.TrimSpace(request.SelectionTicket) == "" {
		respondIAMError(c, fmt.Errorf("%w: invalid context selection request", commonapi.ErrBadRequest))
		return
	}
	choice, err := request.IAMContextChoiceRequest.toIAMChoice()
	if err != nil {
		respondIAMError(c, err)
		return
	}
	session, err := h.contextSelectionService.ConsumeContextSelection(c.Request.Context(), iam.ConsumeContextSelectionInput{
		SelectionTicket: request.SelectionTicket,
		Choice:          choice,
		Audit:           iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	h.respondWithBrowserSession(c, session)
}

// Context godoc
// @Summary      解析授权上下文 | Resolve authorization context
// @Tags         认证 | Authentication
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} authorization.AuthContext
// @x-addp-auth-mode "authenticated"
// @Router       /auth/context [get]
func (h *IAMAuthHandler) Context(c *gin.Context) {
	accessToken := iamBearerToken(c.GetHeader("Authorization"))
	if accessToken == "" {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	authContext, err := h.authContextService.ResolveAuthContext(c.Request.Context(), accessToken)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, authContext)
}

// ContextOptions godoc
// @Summary      查询可切换上下文 | List switchable contexts
// @Tags         认证 | Authentication
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} IAMContextOptionsResponse
// @x-addp-auth-mode "self"
// @Router       /auth/context-options [get]
func (h *IAMAuthHandler) ContextOptions(c *gin.Context) {
	accessToken := iamBearerToken(c.GetHeader("Authorization"))
	if accessToken == "" {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	options, err := h.contextOptionsService.ListBrowserContextOptions(c.Request.Context(), accessToken)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	mapped, err := mapIAMBrowserContextOptions(options)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMContextOptionsResponse{Contexts: mapped})
}

// SwitchContext godoc
// @Summary      切换浏览器上下文 | Switch browser context
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMContextChoiceRequest true "目标上下文 | Target context"
// @Success      200 {object} IAMAccessTokenResponse
// @x-addp-auth-mode "self"
// @Router       /auth/context-switches [post]
func (h *IAMAuthHandler) SwitchContext(c *gin.Context) {
	accessToken := iamBearerToken(c.GetHeader("Authorization"))
	refreshToken, cookieErr := c.Cookie(iamRefreshCookieName)
	if accessToken == "" || cookieErr != nil {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	var request IAMContextChoiceRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid context switch request", commonapi.ErrBadRequest))
		return
	}
	choice, err := request.toIAMChoice()
	if err != nil {
		respondIAMError(c, err)
		return
	}
	session, err := h.contextSwitchService.SwitchBrowserContext(c.Request.Context(), iam.SwitchBrowserContextInput{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Target:       choice,
		Audit:        iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	h.respondWithBrowserSession(c, session)
}

// Refresh godoc
// @Summary      轮换浏览器令牌 | Rotate browser tokens
// @Tags         认证 | Authentication
// @Produce      json
// @Success      200 {object} IAMAccessTokenResponse
// @x-addp-auth-mode "public"
// @Router       /refresh [post]
func (h *IAMAuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(iamRefreshCookieName)
	if err != nil {
		h.clearBrowserSessionCookies(c)
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	session, err := h.refreshService.RotateBrowserRefreshToken(c.Request.Context(), iam.RotateBrowserRefreshTokenInput{
		RefreshToken: refreshToken,
		Audit:        iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		if errors.Is(err, commonapi.ErrUnauthorized) {
			h.clearBrowserSessionCookies(c)
		}
		respondIAMError(c, err)
		return
	}
	h.respondWithBrowserSession(c, session)
}

// Logout godoc
// @Summary      退出浏览器会话 | Sign out browser session
// @Tags         认证 | Authentication
// @Security     BearerAuth
// @Success      204
// @x-addp-auth-mode "authenticated"
// @Router       /logout [post]
func (h *IAMAuthHandler) Logout(c *gin.Context) {
	accessToken := iamBearerToken(c.GetHeader("Authorization"))
	refreshToken, cookieErr := c.Cookie(iamRefreshCookieName)
	var err error
	if accessToken == "" || cookieErr != nil {
		err = commonapi.ErrUnauthorized
	} else {
		err = h.logoutService.LogoutBrowserSession(c.Request.Context(), iam.LogoutBrowserSessionInput{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			Audit:        iamAuditMetadataWithStatus(c, http.StatusNoContent),
		})
	}
	h.clearBrowserSessionCookies(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IAMAuthHandler) mapBrowserLoginResult(
	result *iam.ContextSelectionResult,
) (*IAMBrowserLoginResponse, *iam.IssuedBrowserSession, error) {
	if result == nil {
		return nil, nil, fmt.Errorf("invalid empty browser login result")
	}
	switch result.NextAction {
	case iam.ContextSelectionNextActionSessionIssued:
		if result.Session == nil || result.Challenge != nil || result.MFA != nil {
			return nil, nil, fmt.Errorf("invalid session-issued browser login result")
		}
		sessionResponse, err := newIAMAccessTokenResponse(result.Session, h.now().UTC())
		if err != nil {
			return nil, nil, err
		}
		return &IAMBrowserLoginResponse{
			NextAction: string(result.NextAction),
			Session:    sessionResponse,
		}, result.Session, nil
	case iam.ContextSelectionNextActionSelectContext:
		if result.Session != nil || result.Challenge == nil || result.MFA != nil ||
			!strings.HasPrefix(result.Challenge.SelectionTicket, "addp_cst_") ||
			!result.Challenge.ExpiresAt.After(h.now().UTC()) {
			return nil, nil, fmt.Errorf("invalid context-selection browser login result")
		}
		contexts, err := mapIAMAvailableContexts(result.Challenge.Contexts)
		if err != nil {
			return nil, nil, err
		}
		return &IAMBrowserLoginResponse{
			NextAction: string(result.NextAction),
			Selection: &IAMContextSelectionChallengeResponse{
				SelectionTicket: result.Challenge.SelectionTicket,
				ExpiresAt:       result.Challenge.ExpiresAt.UTC(),
				Contexts:        contexts,
			},
		}, nil, nil
	case iam.ContextSelectionNextActionVerifyMFA:
		if result.Session != nil || result.Challenge != nil || result.MFA == nil ||
			!strings.HasPrefix(result.MFA.ChallengeToken, "addp_mfc_") ||
			!result.MFA.ExpiresAt.After(h.now().UTC()) {
			return nil, nil, fmt.Errorf("invalid MFA browser login result")
		}
		return &IAMBrowserLoginResponse{
			NextAction: string(result.NextAction),
			MFA: &IAMMFAChallengeResponse{
				ChallengeToken: result.MFA.ChallengeToken,
				Method:         "totp",
				ExpiresAt:      result.MFA.ExpiresAt.UTC(),
			},
		}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported browser login next action %q", result.NextAction)
	}
}

func (h *IAMAuthHandler) respondWithBrowserSession(c *gin.Context, session *iam.IssuedBrowserSession) {
	response, err := newIAMAccessTokenResponse(session, h.now().UTC())
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.setBrowserSessionCookies(c, session); err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *IAMAuthHandler) setBrowserSessionCookies(c *gin.Context, session *iam.IssuedBrowserSession) error {
	now := h.now().UTC()
	if session == nil || !session.RefreshTokenFamilyExpiresAt.After(now) ||
		!session.ResourceTicketExpiresAt.After(now) ||
		len(session.ResourceAccessTickets) != len(h.resourceTicketOwners) {
		return fmt.Errorf("invalid browser session cookies")
	}
	for _, owner := range h.resourceTicketOwners {
		token, exists := session.ResourceAccessTickets[owner]
		if !exists || !strings.HasPrefix(token, "addp_rat_") || len(token) == len("addp_rat_") {
			return fmt.Errorf("invalid resource access ticket for owner %q", owner)
		}
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		iamRefreshCookieName,
		session.RefreshToken,
		secondsUntil(now, session.RefreshTokenFamilyExpiresAt),
		"/api/v1/system",
		"",
		h.secureCookies,
		true,
	)
	for _, owner := range h.resourceTicketOwners {
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(
			models.BrowserResourceAccessTicketCookieName,
			session.ResourceAccessTickets[owner],
			secondsUntil(now, session.ResourceTicketExpiresAt),
			"/api/v1/"+owner,
			"",
			h.secureCookies,
			true,
		)
	}
	return nil
}

func (h *IAMAuthHandler) clearBrowserSessionCookies(c *gin.Context) {
	clearIAMBrowserSessionCookies(c, h.secureCookies, h.resourceTicketOwners)
}

func clearIAMBrowserSessionCookies(c *gin.Context, secureCookies bool, resourceTicketOwners []string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(iamRefreshCookieName, "", -1, "/api/v1/system", "", secureCookies, true)
	for _, owner := range resourceTicketOwners {
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(
			models.BrowserResourceAccessTicketCookieName,
			"",
			-1,
			"/api/v1/"+owner,
			"",
			secureCookies,
			true,
		)
	}
}

func normalizeIAMResourceTicketOwners(owners []string) ([]string, error) {
	normalized := append(make([]string, 0, len(owners)), owners...)
	sort.Strings(normalized)
	for index, owner := range normalized {
		if strings.TrimSpace(owner) != owner || owner == "" {
			return nil, fmt.Errorf("%w: invalid resource ticket owner", commonapi.ErrBadRequest)
		}
		for _, character := range owner {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
				return nil, fmt.Errorf("%w: invalid resource ticket owner", commonapi.ErrBadRequest)
			}
		}
		if owner[0] < 'a' || owner[0] > 'z' || (index > 0 && normalized[index-1] == owner) {
			return nil, fmt.Errorf("%w: invalid resource ticket owner", commonapi.ErrBadRequest)
		}
	}
	return normalized, nil
}

func iamBearerToken(header string) string {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func iamAuditMetadata(c *gin.Context) iam.AuditMetadata {
	method := c.Request.Method
	path := c.FullPath()
	if path == "" {
		path = c.Request.URL.Path
	}
	requestID := requestidmiddleware.FromGinContext(c)
	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()
	metadata := iam.AuditMetadata{
		HTTPMethod:   &method,
		ResourcePath: &path,
		IPAddress:    &ipAddress,
		UserAgent:    &userAgent,
	}
	if requestID != "" {
		metadata.RequestID = &requestID
	}
	if authContext, exists := middleware.IAMAuthContextFromGin(c); exists {
		if principalID, err := strconv.ParseInt(authContext.Principal.ID, 10, 64); err == nil && principalID > 0 {
			principalType := iam.PrincipalType(authContext.Principal.Type)
			metadata.PrincipalID = &principalID
			metadata.PrincipalType = &principalType
		}
		contextType := iam.ContextType(authContext.Context.Type)
		metadata.ContextType = &contextType
		if authContext.Context.TenantID != nil {
			if tenantID, err := strconv.ParseInt(*authContext.Context.TenantID, 10, 64); err == nil && tenantID > 0 {
				metadata.TenantID = &tenantID
			}
		}
	}
	return metadata
}

func iamAuditMetadataWithStatus(c *gin.Context, status int) iam.AuditMetadata {
	metadata := iamAuditMetadata(c)
	metadata.HTTPStatus = &status
	return metadata
}

func respondIAMError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	messageID := sysi18n.MsgInternalError
	var errorCode *string
	switch {
	case errors.Is(err, iam.ErrOAuthClientVersionConflict):
		status = http.StatusConflict
		messageID = sysi18n.MsgOAuthClientVersionConflict
		code := "resource_version_conflict"
		errorCode = &code
	case errors.Is(err, iam.ErrOrganizationVersionConflict):
		status = http.StatusConflict
		messageID = sysi18n.MsgOrganizationVersionConflict
		code := "resource_version_conflict"
		errorCode = &code
	case errors.Is(err, iam.ErrTenantRoleAssignmentAlreadyExists):
		status = http.StatusConflict
		messageID = sysi18n.MsgRoleAssignmentAlreadyExists
		code := "role_assignment_already_exists"
		errorCode = &code
	case errors.Is(err, iam.ErrTenantRoleAssignmentPrincipalTypeNotAllowed):
		status = http.StatusConflict
		messageID = sysi18n.MsgRoleAssignmentPrincipalTypeNotAllowed
		code := "role_assignment_principal_type_not_allowed"
		errorCode = &code
	case errors.Is(err, iam.ErrStepUpRequired):
		status = http.StatusForbidden
		messageID = sysi18n.MsgStepUpRequired
		code := "step_up_required"
		errorCode = &code
	case errors.Is(err, iam.ErrMFAResetNotAvailable):
		status = http.StatusConflict
		messageID = sysi18n.MsgMFAResetNotAvailable
	case errors.Is(err, commonapi.ErrBadRequest):
		messageID = commoni18n.MsgInvalidParams
	case errors.Is(err, commonapi.ErrUnauthorized):
		messageID = commoni18n.MsgUnauthorized
	case errors.Is(err, commonapi.ErrForbidden):
		messageID = commoni18n.MsgForbidden
	case errors.Is(err, commonapi.ErrConflict):
		messageID = sysi18n.MsgSessionConflict
	}
	c.JSON(status, IAMErrorResponse{
		Error:     commoni18n.T(c, messageID),
		ErrorCode: errorCode,
	})
}
