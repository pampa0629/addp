package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

const iamRefreshCookieName = "addp_refresh_token"

type iamBrowserLoginService interface {
	LoginLocalBrowser(context.Context, iam.LoginLocalBrowserInput) (*iam.ContextSelectionResult, error)
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
		Audit:    iamAuditMetadata(c),
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
		Audit:           iamAuditMetadata(c),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	h.respondWithBrowserSession(c, session)
}

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
		Audit:        iamAuditMetadata(c),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	h.respondWithBrowserSession(c, session)
}

func (h *IAMAuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(iamRefreshCookieName)
	if err != nil {
		h.clearBrowserSessionCookies(c)
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	session, err := h.refreshService.RotateBrowserRefreshToken(c.Request.Context(), iam.RotateBrowserRefreshTokenInput{
		RefreshToken: refreshToken,
		Audit:        iamAuditMetadata(c),
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
			Audit:        iamAuditMetadata(c),
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
		if result.Session == nil || result.Challenge != nil {
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
		if result.Session != nil || result.Challenge == nil ||
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
	return metadata
}

func respondIAMError(c *gin.Context, err error) {
	status := commonapi.MapErrorToHTTPStatus(err)
	messageID := sysi18n.MsgInternalError
	var errorCode *string
	switch {
	case errors.Is(err, iam.ErrStepUpRequired):
		status = http.StatusForbidden
		messageID = sysi18n.MsgStepUpRequired
		code := "step_up_required"
		errorCode = &code
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
