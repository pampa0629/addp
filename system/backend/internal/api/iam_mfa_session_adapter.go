package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type IAMMFAStatusResponse struct {
	TOTPEnrolled bool `json:"totp_enrolled"`
}

type IAMBeginMFAEnrollmentRequest struct {
	CurrentPassword string `json:"current_password"`
}

type IAMMFAEnrollmentResponse struct {
	EnrollmentToken string    `json:"enrollment_token"`
	Secret          string    `json:"secret"`
	OTPAuthURI      string    `json:"otpauth_uri"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type IAMCompleteMFAEnrollmentRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	Code            string `json:"code"`
}

type IAMCompleteMFAStepUpRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
}

type iamMFASessionService interface {
	Status(context.Context, int64) (*iam.MFAStatus, error)
	BeginEnrollment(context.Context, iam.BeginMFAEnrollmentInput) (*iam.IssuedMFAEnrollment, error)
	CompleteEnrollment(context.Context, iam.CompleteMFAEnrollmentInput) (*iam.IssuedBrowserSession, error)
	BeginStepUp(context.Context, iam.BeginMFAStepUpInput) (*iam.IssuedMFAChallenge, error)
	CompleteStepUp(context.Context, iam.CompleteMFAStepUpInput) (*iam.IssuedBrowserSession, error)
}

type IAMMFASessionHandler struct {
	service     iamMFASessionService
	authHandler *IAMAuthHandler
}

func NewIAMMFASessionHandler(service iamMFASessionService, authHandler *IAMAuthHandler) (*IAMMFASessionHandler, error) {
	if service == nil || authHandler == nil {
		return nil, fmt.Errorf("%w: IAM MFA session handler dependencies are required", commonapi.ErrBadRequest)
	}
	return &IAMMFASessionHandler{service: service, authHandler: authHandler}, nil
}

// Status godoc
// @Summary      查询当前用户 MFA 状态 | Get current user MFA status
// @Tags         认证 | Authentication
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} IAMMFAStatusResponse
// @x-addp-auth-mode "self"
// @Router       /auth/mfa [get]
func (h *IAMMFASessionHandler) Status(c *gin.Context) {
	principalID, err := currentIAMUserPrincipalID(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	status, err := h.service.Status(c.Request.Context(), principalID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMMFAStatusResponse{TOTPEnrolled: status.TOTPEnrolled})
}

// BeginEnrollment godoc
// @Summary      开始 TOTP 登记 | Begin TOTP enrollment
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMBeginMFAEnrollmentRequest true "当前密码 | Current password"
// @Success      201 {object} IAMMFAEnrollmentResponse
// @x-addp-auth-mode "self"
// @Router       /auth/mfa/totp-enrollments [post]
func (h *IAMMFASessionHandler) BeginEnrollment(c *gin.Context) {
	var request IAMBeginMFAEnrollmentRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || request.CurrentPassword == "" {
		respondIAMError(c, fmt.Errorf("%w: current password is required", commonapi.ErrBadRequest))
		return
	}
	accessToken, refreshToken, ok := browserSessionCredentials(c)
	if !ok {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	issued, err := h.service.BeginEnrollment(c.Request.Context(), iam.BeginMFAEnrollmentInput{
		AccessToken: accessToken, RefreshToken: refreshToken, CurrentPassword: request.CurrentPassword,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		h.respondMFAError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMMFAEnrollmentResponse{
		EnrollmentToken: issued.EnrollmentToken, Secret: issued.Secret,
		OTPAuthURI: issued.OTPAuthURI, ExpiresAt: issued.ExpiresAt.UTC(),
	})
}

// CompleteEnrollment godoc
// @Summary      完成 TOTP 登记 | Complete TOTP enrollment
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCompleteMFAEnrollmentRequest true "TOTP 验证 | TOTP verification"
// @Success      200 {object} IAMAccessTokenResponse
// @x-addp-auth-mode "self"
// @Router       /auth/mfa/totp-enrollment-verifications [post]
func (h *IAMMFASessionHandler) CompleteEnrollment(c *gin.Context) {
	var request IAMCompleteMFAEnrollmentRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil ||
		strings.TrimSpace(request.EnrollmentToken) == "" || strings.TrimSpace(request.Code) == "" {
		respondIAMError(c, fmt.Errorf("%w: enrollment token and code are required", commonapi.ErrBadRequest))
		return
	}
	accessToken, refreshToken, ok := browserSessionCredentials(c)
	if !ok {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	session, err := h.service.CompleteEnrollment(c.Request.Context(), iam.CompleteMFAEnrollmentInput{
		AccessToken: accessToken, RefreshToken: refreshToken,
		EnrollmentToken: request.EnrollmentToken, Code: request.Code,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		h.respondMFAError(c, err)
		return
	}
	h.authHandler.respondWithBrowserSession(c, session)
}

// BeginStepUp godoc
// @Summary      开始会话增强认证 | Begin session step-up
// @Tags         认证 | Authentication
// @Produce      json
// @Security     BearerAuth
// @Success      201 {object} IAMMFAChallengeResponse
// @x-addp-auth-mode "self"
// @Router       /auth/mfa/step-up-challenges [post]
func (h *IAMMFASessionHandler) BeginStepUp(c *gin.Context) {
	accessToken, refreshToken, ok := browserSessionCredentials(c)
	if !ok {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	issued, err := h.service.BeginStepUp(c.Request.Context(), iam.BeginMFAStepUpInput{
		AccessToken: accessToken, RefreshToken: refreshToken,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		h.respondMFAError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMMFAChallengeResponse{
		ChallengeToken: issued.ChallengeToken, Method: "totp", ExpiresAt: issued.ExpiresAt.UTC(),
	})
}

// CompleteStepUp godoc
// @Summary      完成会话增强认证 | Complete session step-up
// @Tags         认证 | Authentication
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCompleteMFAStepUpRequest true "TOTP 验证 | TOTP verification"
// @Success      200 {object} IAMAccessTokenResponse
// @x-addp-auth-mode "self"
// @Router       /auth/mfa/step-up-verifications [post]
func (h *IAMMFASessionHandler) CompleteStepUp(c *gin.Context) {
	var request IAMCompleteMFAStepUpRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil ||
		strings.TrimSpace(request.ChallengeToken) == "" || strings.TrimSpace(request.Code) == "" {
		respondIAMError(c, fmt.Errorf("%w: challenge token and code are required", commonapi.ErrBadRequest))
		return
	}
	accessToken, refreshToken, ok := browserSessionCredentials(c)
	if !ok {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	session, err := h.service.CompleteStepUp(c.Request.Context(), iam.CompleteMFAStepUpInput{
		AccessToken: accessToken, RefreshToken: refreshToken,
		ChallengeToken: request.ChallengeToken, Code: request.Code,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		h.respondMFAError(c, err)
		return
	}
	h.authHandler.respondWithBrowserSession(c, session)
}

func (h *IAMMFASessionHandler) respondMFAError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, iam.ErrInvalidCurrentPassword):
		code := "invalid_current_password"
		c.JSON(http.StatusBadRequest, IAMErrorResponse{Error: commoni18n.T(c, sysi18n.MsgInvalidCurrentPassword), ErrorCode: &code})
	case errors.Is(err, iam.ErrTOTPAlreadyEnrolled):
		code := "totp_already_enrolled"
		c.JSON(http.StatusConflict, IAMErrorResponse{Error: commoni18n.T(c, sysi18n.MsgTOTPAlreadyEnrolled), ErrorCode: &code})
	case errors.Is(err, iam.ErrTOTPEnrollmentRequired):
		code := "totp_enrollment_required"
		c.JSON(http.StatusConflict, IAMErrorResponse{Error: commoni18n.T(c, sysi18n.MsgTOTPEnrollmentRequired), ErrorCode: &code})
	case errors.Is(err, commonapi.ErrUnauthorized):
		code := "invalid_mfa_verification"
		c.JSON(http.StatusUnauthorized, IAMErrorResponse{Error: commoni18n.T(c, sysi18n.MsgInvalidMFAVerification), ErrorCode: &code})
	default:
		respondIAMError(c, err)
	}
}

func browserSessionCredentials(c *gin.Context) (string, string, bool) {
	accessToken := iamBearerToken(c.GetHeader("Authorization"))
	refreshToken, err := c.Cookie(iamRefreshCookieName)
	return accessToken, refreshToken, err == nil && accessToken != "" && refreshToken != ""
}

func currentIAMUserPrincipalID(c *gin.Context) (int64, error) {
	authContext, exists := middleware.IAMAuthContextFromGin(c)
	if !exists {
		return 0, commonapi.ErrUnauthorized
	}
	if authContext.Principal.Type != "user" {
		return 0, commonapi.ErrForbidden
	}
	principalID, err := strconv.ParseInt(authContext.Principal.ID, 10, 64)
	if err != nil || principalID <= 0 || strconv.FormatInt(principalID, 10) != authContext.Principal.ID {
		return 0, errors.New("invalid IAM principal projection")
	}
	return principalID, nil
}
