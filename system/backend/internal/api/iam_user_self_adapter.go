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
	"github.com/gin-gonic/gin"
)

type IAMCurrentLocalAccountResponse struct {
	Username string `json:"username"`
}

type IAMCurrentUserResponse struct {
	ID           string                          `json:"id"`
	DisplayName  string                          `json:"display_name"`
	PrimaryEmail *string                         `json:"primary_email"`
	Locale       *string                         `json:"locale"`
	CreatedAt    time.Time                       `json:"created_at"`
	UpdatedAt    time.Time                       `json:"updated_at"`
	LocalAccount *IAMCurrentLocalAccountResponse `json:"local_account"`
}

type IAMChangeCurrentPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type IAMPasswordRotationResponse struct {
	ChangedAt          time.Time `json:"changed_at"`
	RevokedFamilyCount int64     `json:"revoked_family_count"`
}

type iamUserSelfService interface {
	ResolveCurrentUserProfile(context.Context, string) (*iam.CurrentUserProfile, error)
	RotateCurrentPassword(
		context.Context,
		string,
		string,
		string,
		iam.AuditMetadata,
	) (*iam.PasswordRotationResult, error)
}

type IAMUserSelfHandler struct {
	service              iamUserSelfService
	secureCookies        bool
	resourceTicketOwners []string
}

func NewIAMUserSelfHandler(
	service iamUserSelfService,
	secureCookies bool,
	resourceTicketOwners []string,
) (*IAMUserSelfHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: IAM user self service is required", commonapi.ErrBadRequest)
	}
	owners, err := normalizeIAMResourceTicketOwners(resourceTicketOwners)
	if err != nil {
		return nil, err
	}
	return &IAMUserSelfHandler{
		service:              service,
		secureCookies:        secureCookies,
		resourceTicketOwners: owners,
	}, nil
}

// Me godoc
// @Summary      查询当前用户资料 | Get current user profile
// @Tags         当前用户 | Current User
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} IAMCurrentUserResponse
// @x-addp-auth-mode "self"
// @Router       /users/me [get]
func (h *IAMUserSelfHandler) Me(c *gin.Context) {
	accessToken := iamBearerToken(c.GetHeader("Authorization"))
	if accessToken == "" {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	profile, err := h.service.ResolveCurrentUserProfile(c.Request.Context(), accessToken)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	response, err := mapIAMCurrentUserProfile(profile)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// ChangePassword godoc
// @Summary      修改当前用户密码 | Change current user password
// @Tags         当前用户 | Current User
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMChangeCurrentPasswordRequest true "密码 | Passwords"
// @Success      200 {object} IAMPasswordRotationResponse
// @x-addp-auth-mode "self"
// @Router       /users/me/password [put]
func (h *IAMUserSelfHandler) ChangePassword(c *gin.Context) {
	accessToken := iamBearerToken(c.GetHeader("Authorization"))
	if accessToken == "" {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	var request IAMChangeCurrentPasswordRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil ||
		request.CurrentPassword == "" || request.NewPassword == "" {
		respondIAMError(c, fmt.Errorf("%w: both passwords are required", commonapi.ErrBadRequest))
		return
	}
	result, err := h.service.RotateCurrentPassword(
		c.Request.Context(),
		accessToken,
		request.CurrentPassword,
		request.NewPassword,
		iamAuditMetadataWithStatus(c, http.StatusOK),
	)
	if err != nil {
		respondIAMPasswordError(c, err)
		return
	}
	if result == nil || result.ChangedAt.IsZero() || result.RevokedFamilyCount < 1 {
		respondIAMError(c, errors.New("invalid password rotation result"))
		return
	}
	clearIAMBrowserSessionCookies(c, h.secureCookies, h.resourceTicketOwners)
	c.JSON(http.StatusOK, IAMPasswordRotationResponse{
		ChangedAt:          result.ChangedAt.UTC(),
		RevokedFamilyCount: result.RevokedFamilyCount,
	})
}

func mapIAMCurrentUserProfile(profile *iam.CurrentUserProfile) (*IAMCurrentUserResponse, error) {
	if profile == nil || profile.ID <= 0 || strings.TrimSpace(profile.DisplayName) == "" ||
		profile.CreatedAt.IsZero() || profile.UpdatedAt.IsZero() {
		return nil, errors.New("invalid current user profile")
	}
	response := &IAMCurrentUserResponse{
		ID:           strconv.FormatInt(profile.ID, 10),
		DisplayName:  profile.DisplayName,
		PrimaryEmail: profile.PrimaryEmail,
		Locale:       profile.Locale,
		CreatedAt:    profile.CreatedAt.UTC(),
		UpdatedAt:    profile.UpdatedAt.UTC(),
	}
	if profile.LocalAccount != nil {
		if strings.TrimSpace(profile.LocalAccount.Username) == "" {
			return nil, errors.New("invalid current local account profile")
		}
		response.LocalAccount = &IAMCurrentLocalAccountResponse{Username: profile.LocalAccount.Username}
	}
	return response, nil
}

func respondIAMPasswordError(c *gin.Context, err error) {
	var messageID string
	var code string
	switch {
	case errors.Is(err, iam.ErrInvalidCurrentPassword):
		messageID = sysi18n.MsgInvalidCurrentPassword
		code = "invalid_current_password"
	case errors.Is(err, iam.ErrPasswordUnchanged):
		messageID = sysi18n.MsgPasswordUnchanged
		code = "password_unchanged"
	default:
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusBadRequest, IAMErrorResponse{
		Error:     commoni18n.T(c, messageID),
		ErrorCode: &code,
	})
}
