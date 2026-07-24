package api

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
)

type IAMBrowserLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type IAMContextChoiceRequest struct {
	ContextType        string  `json:"context_type"`
	TenantMembershipID *string `json:"tenant_membership_id,omitempty"`
}

type IAMContextSelectionRequest struct {
	SelectionTicket string `json:"selection_ticket"`
	IAMContextChoiceRequest
}

type IAMAccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type IAMContextOptionResponse struct {
	Type               string  `json:"type"`
	TenantID           *string `json:"tenant_id,omitempty"`
	TenantMembershipID *string `json:"tenant_membership_id,omitempty"`
	TenantCode         *string `json:"tenant_code,omitempty"`
	TenantName         *string `json:"tenant_name,omitempty"`
	Current            bool    `json:"current"`
	RequiresStepUp     bool    `json:"requires_step_up"`
}

type IAMContextOptionsResponse struct {
	Contexts []IAMContextOptionResponse `json:"contexts"`
}

type IAMContextSelectionChallengeResponse struct {
	SelectionTicket string                     `json:"selection_ticket"`
	ExpiresAt       time.Time                  `json:"expires_at"`
	Contexts        []IAMContextOptionResponse `json:"contexts"`
}

type IAMBrowserLoginResponse struct {
	NextAction string                                `json:"next_action"`
	Session    *IAMAccessTokenResponse               `json:"session,omitempty"`
	Selection  *IAMContextSelectionChallengeResponse `json:"selection,omitempty"`
}

type IAMErrorResponse struct {
	Error     string  `json:"error"`
	ErrorCode *string `json:"error_code,omitempty"`
}

func newIAMAccessTokenResponse(session *iam.IssuedBrowserSession, now time.Time) (*IAMAccessTokenResponse, error) {
	if session == nil || !strings.HasPrefix(session.AccessToken, "addp_at_") ||
		len(session.AccessToken) == len("addp_at_") ||
		!strings.HasPrefix(session.RefreshToken, "addp_rt_") ||
		len(session.RefreshToken) == len("addp_rt_") ||
		!session.AccessTokenExpiresAt.After(now) || !session.RefreshTokenFamilyExpiresAt.After(now) {
		return nil, fmt.Errorf("invalid issued browser session")
	}
	return &IAMAccessTokenResponse{
		AccessToken: session.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   secondsUntil(now, session.AccessTokenExpiresAt),
	}, nil
}

func mapIAMAvailableContexts(contexts []iam.AvailableContext) ([]IAMContextOptionResponse, error) {
	result := make([]IAMContextOptionResponse, 0, len(contexts))
	for _, contextOption := range contexts {
		mapped, err := mapIAMAvailableContext(contextOption, false, false)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func mapIAMBrowserContextOptions(options []iam.BrowserContextOption) ([]IAMContextOptionResponse, error) {
	result := make([]IAMContextOptionResponse, 0, len(options))
	for _, option := range options {
		mapped, err := mapIAMAvailableContext(option.AvailableContext, option.Current, option.RequiresStepUp)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func mapIAMAvailableContext(
	contextOption iam.AvailableContext,
	current bool,
	requiresStepUp bool,
) (IAMContextOptionResponse, error) {
	result := IAMContextOptionResponse{
		Type:           string(contextOption.Type),
		Current:        current,
		RequiresStepUp: requiresStepUp,
	}
	switch contextOption.Type {
	case iam.ContextTypePlatform:
		if contextOption.TenantID != nil || contextOption.TenantMembershipID != nil ||
			contextOption.TenantCode != "" || contextOption.TenantName != "" {
			return IAMContextOptionResponse{}, fmt.Errorf("platform context contains tenant fields")
		}
	case iam.ContextTypeTenant:
		if contextOption.TenantID == nil || *contextOption.TenantID <= 0 ||
			contextOption.TenantMembershipID == nil || *contextOption.TenantMembershipID <= 0 ||
			strings.TrimSpace(contextOption.TenantCode) == "" || strings.TrimSpace(contextOption.TenantName) == "" {
			return IAMContextOptionResponse{}, fmt.Errorf("tenant context is incomplete")
		}
		tenantID := strconv.FormatInt(*contextOption.TenantID, 10)
		membershipID := strconv.FormatInt(*contextOption.TenantMembershipID, 10)
		tenantCode := contextOption.TenantCode
		tenantName := contextOption.TenantName
		result.TenantID = &tenantID
		result.TenantMembershipID = &membershipID
		result.TenantCode = &tenantCode
		result.TenantName = &tenantName
	default:
		return IAMContextOptionResponse{}, fmt.Errorf("unsupported context type %q", contextOption.Type)
	}
	return result, nil
}

func (request IAMContextChoiceRequest) toIAMChoice() (iam.ContextSelectionChoice, error) {
	choice := iam.ContextSelectionChoice{Type: iam.ContextType(request.ContextType)}
	switch choice.Type {
	case iam.ContextTypePlatform:
		if request.TenantMembershipID != nil {
			return iam.ContextSelectionChoice{}, fmt.Errorf("%w: platform context cannot include membership", commonapi.ErrBadRequest)
		}
	case iam.ContextTypeTenant:
		if request.TenantMembershipID == nil {
			return iam.ContextSelectionChoice{}, fmt.Errorf("%w: tenant context requires membership", commonapi.ErrBadRequest)
		}
		membershipID, err := parseIAMDecimalID(*request.TenantMembershipID)
		if err != nil {
			return iam.ContextSelectionChoice{}, err
		}
		choice.TenantMembershipID = &membershipID
	default:
		return iam.ContextSelectionChoice{}, fmt.Errorf("%w: unsupported context type", commonapi.ErrBadRequest)
	}
	return choice, nil
}

func parseIAMDecimalID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf("%w: invalid IAM ID", commonapi.ErrBadRequest)
	}
	return parsed, nil
}

func secondsUntil(now time.Time, deadline time.Time) int {
	seconds := math.Ceil(deadline.Sub(now).Seconds())
	if seconds <= 0 {
		return 0
	}
	if seconds > float64(math.MaxInt) {
		return math.MaxInt
	}
	return int(seconds)
}
