package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMTenantInvitationResponse struct {
	ID                    string                     `json:"id"`
	TenantID              string                     `json:"tenant_id"`
	Email                 string                     `json:"email"`
	Status                iam.TenantInvitationStatus `json:"status"`
	ExpiresAt             time.Time                  `json:"expires_at"`
	AcceptedAt            *time.Time                 `json:"accepted_at"`
	AcceptedByPrincipalID *string                    `json:"accepted_by_principal_id"`
	RevokedAt             *time.Time                 `json:"revoked_at"`
	RevokedByPrincipalID  *string                    `json:"revoked_by_principal_id"`
	ExpiredAt             *time.Time                 `json:"expired_at"`
	CreatedByPrincipalID  string                     `json:"created_by_principal_id"`
	CreatedAt             time.Time                  `json:"created_at"`
	UpdatedAt             time.Time                  `json:"updated_at"`
}

type IAMCreateTenantInvitationRequest struct {
	Email string `json:"email"`
}

type IAMCreatedTenantInvitationResponse struct {
	Invitation    IAMTenantInvitationResponse `json:"invitation"`
	InvitationURL string                      `json:"invitation_url"`
}

type IAMInvitationEnrollmentRequest struct {
	InvitationSecret string `json:"invitation_secret"`
	Username         string `json:"username"`
	Password         string `json:"password"`
}

type IAMInvitationEnrollmentResponse struct {
	EnrollmentTicket string    `json:"enrollment_ticket"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type IAMInvitationRegistrationRequest struct {
	InvitationSecret string  `json:"invitation_secret"`
	Username         string  `json:"username"`
	Password         string  `json:"password"`
	DisplayName      string  `json:"display_name"`
	Locale           *string `json:"locale"`
}

type IAMInvitationAcceptanceRequest struct {
	InvitationSecret string `json:"invitation_secret"`
	EnrollmentTicket string `json:"enrollment_ticket,omitempty"`
}

type IAMInvitationAcceptanceResponse struct {
	Invitation         IAMTenantInvitationResponse `json:"invitation"`
	TenantMembershipID string                      `json:"tenant_membership_id"`
	Session            IAMAccessTokenResponse      `json:"session"`
}

type iamTenantInvitationService interface {
	List(context.Context, int64, int, int, string, *iam.TenantInvitationStatus, iam.AuditMetadata) ([]iam.TenantInvitation, int64, error)
	Get(context.Context, int64, int64, iam.AuditMetadata) (*iam.TenantInvitation, error)
	Create(context.Context, iam.CreateTenantInvitationInput) (*iam.CreatedTenantInvitation, error)
	Revoke(context.Context, int64, int64, int64, iam.AuditMetadata) (*iam.TenantInvitation, error)
	IssueEnrollmentTicket(context.Context, iam.IssueEnrollmentTicketInput) (*iam.IssuedEnrollmentTicket, error)
	Register(context.Context, iam.RegisterTenantInvitationInput) (*iam.AcceptedTenantInvitation, error)
	Accept(context.Context, iam.AcceptTenantInvitationInput) (*iam.AcceptedTenantInvitation, error)
}

type iamFirstPartyContextResolver interface {
	ResolveFirstPartyAccessToken(context.Context, string) (*commonauth.AuthContext, error)
}

type IAMTenantInvitationHandler struct {
	service     iamTenantInvitationService
	authContext iamFirstPartyContextResolver
	authHandler *IAMAuthHandler
	consoleURL  string
	now         func() time.Time
}

func NewIAMTenantInvitationHandler(
	service iamTenantInvitationService,
	authContext iamFirstPartyContextResolver,
	authHandler *IAMAuthHandler,
	consoleURL string,
) (*IAMTenantInvitationHandler, error) {
	if service == nil || authContext == nil || authHandler == nil || strings.TrimSpace(consoleURL) == "" {
		return nil, fmt.Errorf("%w: tenant invitation handler dependencies are required", commonapi.ErrBadRequest)
	}
	return &IAMTenantInvitationHandler{
		service: service, authContext: authContext, authHandler: authHandler,
		consoleURL: strings.TrimSuffix(consoleURL, "/"), now: time.Now,
	}, nil
}

// List godoc
// @Summary      查询租户邀请 | List tenant invitations
// @Tags         租户邀请 | Tenant Invitations
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number"
// @Param        page_size query int false "每页数量 | Page size"
// @Param        search query string false "邀请邮箱 | Invitation email"
// @Param        status query string false "状态 | Status"
// @Success      200 {object} object{data=[]IAMTenantInvitationResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_invitation.read"]
// @Router       /tenant/invitations [get]
func (h *IAMTenantInvitationHandler) List(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil || actorID == 0 {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	status, err := parseTenantInvitationStatus(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	invitations, total, err := h.service.List(
		c.Request.Context(), int64(tenantID), page, pageSize, c.Query("search"), status, iamAuditMetadata(c),
	)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMTenantInvitationResponse, 0, len(invitations))
	for _, invitation := range invitations {
		responses = append(responses, mapIAMTenantInvitation(invitation))
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

// Get godoc
// @Summary      查询租户邀请详情 | Get tenant invitation
// @Tags         租户邀请 | Tenant Invitations
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "邀请 ID | Invitation ID"
// @Success      200 {object} IAMTenantInvitationResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_invitation.read"]
// @Router       /tenant/invitations/{id} [get]
func (h *IAMTenantInvitationHandler) Get(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	invitationID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	invitation, err := h.service.Get(c.Request.Context(), int64(tenantID), invitationID, iamAuditMetadata(c))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenantInvitation(*invitation))
}

// Create godoc
// @Summary      创建租户邀请 | Create tenant invitation
// @Tags         租户邀请 | Tenant Invitations
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCreateTenantInvitationRequest true "邀请 | Invitation"
// @Success      201 {object} IAMCreatedTenantInvitationResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_invitation.create"]
// @Router       /tenant/invitations [post]
func (h *IAMTenantInvitationHandler) Create(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMCreateTenantInvitationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid invitation request", commonapi.ErrBadRequest))
		return
	}
	created, err := h.service.Create(c.Request.Context(), iam.CreateTenantInvitationInput{
		TenantID: int64(tenantID), Email: request.Email, CreatedByPrincipalID: int64(actorID),
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	invitationURL, err := h.invitationURL(created.Secret)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMCreatedTenantInvitationResponse{
		Invitation: mapIAMTenantInvitation(created.Invitation), InvitationURL: invitationURL,
	})
}

// Revoke godoc
// @Summary      撤销租户邀请 | Revoke tenant invitation
// @Tags         租户邀请 | Tenant Invitations
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "邀请 ID | Invitation ID"
// @Success      200 {object} IAMTenantInvitationResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_invitation.revoke"]
// @Router       /tenant/invitations/{id}/revoke [post]
func (h *IAMTenantInvitationHandler) Revoke(c *gin.Context) {
	actorID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	invitationID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	invitation, err := h.service.Revoke(
		c.Request.Context(), int64(tenantID), invitationID, int64(actorID),
		iamAuditMetadataWithStatus(c, http.StatusOK),
	)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenantInvitation(*invitation))
}

// Enroll godoc
// @Summary      签发邀请 Enrollment Ticket | Issue invitation enrollment ticket
// @Tags         租户邀请 | Tenant Invitations
// @Accept       json
// @Produce      json
// @Param        request body IAMInvitationEnrollmentRequest true "邀请与本地账号凭据 | Invitation and local credentials"
// @Success      201 {object} IAMInvitationEnrollmentResponse
// @x-addp-auth-mode "public"
// @Router       /tenant/invitations/enrollments [post]
func (h *IAMTenantInvitationHandler) Enroll(c *gin.Context) {
	var request IAMInvitationEnrollmentRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid enrollment request", commonapi.ErrBadRequest))
		return
	}
	issued, err := h.service.IssueEnrollmentTicket(c.Request.Context(), iam.IssueEnrollmentTicketInput{
		InvitationSecret: request.InvitationSecret, Username: request.Username,
		Password: request.Password, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMInvitationEnrollmentResponse{
		EnrollmentTicket: issued.EnrollmentTicket, ExpiresAt: issued.ExpiresAt.UTC(),
	})
}

// Register godoc
// @Summary      通过邀请注册 | Register through tenant invitation
// @Tags         租户邀请 | Tenant Invitations
// @Accept       json
// @Produce      json
// @Param        request body IAMInvitationRegistrationRequest true "注册资料 | Registration profile"
// @Success      201 {object} IAMInvitationAcceptanceResponse
// @x-addp-auth-mode "public"
// @Router       /tenant/invitations/registrations [post]
func (h *IAMTenantInvitationHandler) Register(c *gin.Context) {
	var request IAMInvitationRegistrationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid invitation registration", commonapi.ErrBadRequest))
		return
	}
	accepted, err := h.service.Register(c.Request.Context(), iam.RegisterTenantInvitationInput{
		InvitationSecret: request.InvitationSecret, Username: request.Username, Password: request.Password,
		DisplayName: request.DisplayName, Locale: request.Locale,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	h.respondAccepted(c, http.StatusCreated, accepted)
}

// Accept godoc
// @Summary      接受租户邀请 | Accept tenant invitation
// @Tags         租户邀请 | Tenant Invitations
// @Accept       json
// @Produce      json
// @Param        request body IAMInvitationAcceptanceRequest true "邀请和可选 Enrollment Ticket | Invitation and optional enrollment ticket"
// @Success      200 {object} IAMInvitationAcceptanceResponse
// @x-addp-auth-mode "authenticated"
// @Router       /tenant/invitations/acceptances [post]
func (h *IAMTenantInvitationHandler) Accept(c *gin.Context) {
	var request IAMInvitationAcceptanceRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid invitation acceptance", commonapi.ErrBadRequest))
		return
	}
	input := iam.AcceptTenantInvitationInput{
		InvitationSecret: request.InvitationSecret, EnrollmentTicket: request.EnrollmentTicket,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}
	bearer := iamBearerToken(c.GetHeader("Authorization"))
	if request.EnrollmentTicket != "" {
		if strings.TrimSpace(c.GetHeader("Authorization")) != "" {
			respondIAMError(c, fmt.Errorf("%w: acceptance credentials are mutually exclusive", commonapi.ErrBadRequest))
			return
		}
	} else {
		if bearer == "" {
			respondIAMError(c, commonapi.ErrUnauthorized)
			return
		}
		authContext, err := h.authContext.ResolveFirstPartyAccessToken(c.Request.Context(), bearer)
		if err != nil {
			respondIAMError(c, err)
			return
		}
		principalID, err := parseIAMDecimalID(authContext.Principal.ID)
		if err != nil || authContext.Principal.Type != "user" {
			respondIAMError(c, commonapi.ErrForbidden)
			return
		}
		input.PrincipalID = principalID
		input.Authentication = iam.SessionAuthentication{
			Methods:         append([]string(nil), authContext.Authentication.Methods...),
			AssuranceLevel:  iam.AssuranceLevel(authContext.Authentication.AssuranceLevel),
			AuthenticatedAt: authContext.Authentication.AuthenticatedAt,
			StepUpExpiresAt: authContext.Authentication.StepUpExpiresAt,
		}
	}
	accepted, err := h.service.Accept(c.Request.Context(), input)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	h.respondAccepted(c, http.StatusOK, accepted)
}

func (h *IAMTenantInvitationHandler) respondAccepted(c *gin.Context, status int, accepted *iam.AcceptedTenantInvitation) {
	if accepted == nil {
		respondIAMError(c, fmt.Errorf("invalid accepted invitation result"))
		return
	}
	sessionResponse, err := newIAMAccessTokenResponse(&accepted.Session, h.now().UTC())
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if err := h.authHandler.setBrowserSessionCookies(c, &accepted.Session); err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(status, IAMInvitationAcceptanceResponse{
		Invitation:         mapIAMTenantInvitation(accepted.Invitation),
		TenantMembershipID: strconv.FormatInt(accepted.Membership.ID, 10), Session: *sessionResponse,
	})
}

func (h *IAMTenantInvitationHandler) invitationURL(secret string) (string, error) {
	parsed, err := url.Parse(h.consoleURL + "/invitations/accept")
	if err != nil {
		return "", fmt.Errorf("invalid Console URL: %w", err)
	}
	query := parsed.Query()
	query.Set("invitation", secret)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func mapIAMTenantInvitation(invitation iam.TenantInvitation) IAMTenantInvitationResponse {
	response := IAMTenantInvitationResponse{
		ID: strconv.FormatInt(invitation.ID, 10), TenantID: strconv.FormatInt(invitation.TenantID, 10),
		Email: invitation.Email, Status: invitation.Status, ExpiresAt: invitation.ExpiresAt.UTC(),
		AcceptedAt: utcTimeResponse(invitation.AcceptedAt), RevokedAt: utcTimeResponse(invitation.RevokedAt),
		ExpiredAt:            utcTimeResponse(invitation.ExpiredAt),
		CreatedByPrincipalID: strconv.FormatInt(invitation.CreatedByPrincipalID, 10),
		CreatedAt:            invitation.CreatedAt.UTC(), UpdatedAt: invitation.UpdatedAt.UTC(),
	}
	if invitation.AcceptedByPrincipalID != nil {
		value := strconv.FormatInt(*invitation.AcceptedByPrincipalID, 10)
		response.AcceptedByPrincipalID = &value
	}
	if invitation.RevokedByPrincipalID != nil {
		value := strconv.FormatInt(*invitation.RevokedByPrincipalID, 10)
		response.RevokedByPrincipalID = &value
	}
	return response
}

func utcTimeResponse(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func parseTenantInvitationStatus(value string) (*iam.TenantInvitationStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	status := iam.TenantInvitationStatus(value)
	switch status {
	case iam.TenantInvitationStatusPending, iam.TenantInvitationStatusAccepted,
		iam.TenantInvitationStatusRevoked, iam.TenantInvitationStatusExpired:
		return &status, nil
	default:
		return nil, fmt.Errorf("%w: invalid invitation status", commonapi.ErrBadRequest)
	}
}
