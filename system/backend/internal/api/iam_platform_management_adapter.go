package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMTenantResponse struct {
	ID          string           `json:"id"`
	Code        string           `json:"code"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Status      iam.TenantStatus `json:"status"`
	Initialized bool             `json:"initialized"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

type IAMCreateTenantRequest struct {
	Code                            string `json:"code"`
	Name                            string `json:"name"`
	Description                     string `json:"description"`
	InitialAdministratorPrincipalID string `json:"initial_administrator_principal_id"`
}

type IAMInitializeTenantRequest struct {
	InitialAdministratorPrincipalID string `json:"initial_administrator_principal_id"`
}

type IAMTenantAdministratorCandidateResponse struct {
	PrincipalID  string  `json:"principal_id"`
	DisplayName  string  `json:"display_name"`
	PrimaryEmail *string `json:"primary_email"`
	Username     *string `json:"username"`
}

type IAMUpdateTenantRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type IAMLifecycleReasonRequest struct {
	Reason string `json:"reason"`
}

type IAMTenantStatusResponse struct {
	Tenant             IAMTenantResponse `json:"tenant"`
	AffectedPrincipals int               `json:"affected_principals"`
	RevokedFamilyCount int64             `json:"revoked_family_count"`
}

type IAMLocalAccountManagementResponse struct {
	ID                   string                 `json:"id"`
	Username             string                 `json:"username"`
	Status               iam.LocalAccountStatus `json:"status"`
	PasswordResetAllowed bool                   `json:"password_reset_allowed"`
}

type IAMManagedUserResponse struct {
	ID                   string                             `json:"id"`
	Status               iam.PrincipalStatus                `json:"status"`
	AuthorizationVersion string                             `json:"authorization_version"`
	DisplayName          string                             `json:"display_name"`
	PrimaryEmail         *string                            `json:"primary_email"`
	Locale               *string                            `json:"locale"`
	LocalAccount         *IAMLocalAccountManagementResponse `json:"local_account"`
	CreatedAt            time.Time                          `json:"created_at"`
	UpdatedAt            time.Time                          `json:"updated_at"`
}

type IAMCreateManagedUserRequest struct {
	DisplayName  string  `json:"display_name"`
	PrimaryEmail *string `json:"primary_email"`
	Locale       *string `json:"locale"`
	LocalAccount struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"local_account"`
}

type IAMUpdateManagedUserRequest struct {
	DisplayName  string  `json:"display_name"`
	PrimaryEmail *string `json:"primary_email"`
	Locale       *string `json:"locale"`
}

type IAMChangeManagedUserStatusRequest struct {
	Reason          string  `json:"reason"`
	ChangeRequestID *string `json:"change_request_id"`
}

type IAMResetManagedLocalAccountPasswordRequest struct {
	NewPassword string `json:"new_password"`
	Reason      string `json:"reason"`
}

type IAMManagedLocalAccountPasswordResetResponse struct {
	AuthorizationVersion       string    `json:"authorization_version"`
	RevokedFamilyCount         int64     `json:"revoked_family_count"`
	ConsumedMFAChallengeCount  int64     `json:"consumed_mfa_challenge_count"`
	ConsumedContextTicketCount int64     `json:"consumed_context_ticket_count"`
	ChangedAt                  time.Time `json:"changed_at"`
}

type IAMManagedUserStatusResponse struct {
	User               IAMManagedUserResponse `json:"user"`
	RevokedFamilyCount int64                  `json:"revoked_family_count"`
}

type iamPlatformTenantService interface {
	List(context.Context, int, int, string, *iam.TenantStatus) ([]iam.ManagedTenant, int64, error)
	Get(context.Context, int64) (*iam.ManagedTenant, error)
	Create(context.Context, iam.CreateTenantInput) (*iam.ManagedTenant, error)
	Initialize(context.Context, iam.InitializeTenantInput) (*iam.ManagedTenant, error)
	ListAdministratorCandidates(context.Context, string) ([]iam.TenantAdministratorCandidate, error)
	Update(context.Context, iam.UpdateTenantInput) (*iam.ManagedTenant, error)
	Suspend(context.Context, iam.ChangeTenantStatusInput) (*iam.TenantStatusChangeResult, error)
	Restore(context.Context, iam.ChangeTenantStatusInput) (*iam.TenantStatusChangeResult, error)
	Close(context.Context, iam.ChangeTenantStatusInput) (*iam.TenantStatusChangeResult, error)
}

type IAMPlatformTenantHandler struct {
	service iamPlatformTenantService
}

func NewIAMPlatformTenantHandler(service iamPlatformTenantService) (*IAMPlatformTenantHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: platform tenant service is required", commonapi.ErrBadRequest)
	}
	return &IAMPlatformTenantHandler{service: service}, nil
}

// List godoc
// @Summary      查询平台租户 | List platform tenants
// @Tags         平台租户 | Platform Tenants
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number"
// @Param        page_size query int false "每页数量 | Page size"
// @Param        search query string false "名称或编码 | Name or code"
// @Param        status query string false "状态 | Status"
// @Success      200 {object} object{data=[]IAMTenantResponse,total=int64,page=int,page_size=int,total_pages=int}
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.read"]
// @Router       /platform/tenants [get]
func (h *IAMPlatformTenantHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	status, err := parseTenantStatusFilter(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	tenants, total, err := h.service.List(c.Request.Context(), page, pageSize, c.Query("search"), status)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMTenantResponse, 0, len(tenants))
	for _, tenant := range tenants {
		responses = append(responses, mapIAMTenant(tenant))
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

// Get godoc
// @Summary      查询平台租户详情 | Get platform tenant
// @Tags         平台租户 | Platform Tenants
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "租户 ID | Tenant ID"
// @Success      200 {object} IAMTenantResponse
// @Failure      404 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.read"]
// @Router       /platform/tenants/{id} [get]
func (h *IAMPlatformTenantHandler) Get(c *gin.Context) {
	tenantID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	tenant, err := h.service.Get(c.Request.Context(), tenantID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenant(*tenant))
}

// Create godoc
// @Summary      创建平台租户 | Create platform tenant
// @Tags         平台租户 | Platform Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCreateTenantRequest true "租户 | Tenant"
// @Success      201 {object} IAMTenantResponse
// @Failure      400 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.create"]
// @Router       /platform/tenants [post]
func (h *IAMPlatformTenantHandler) Create(c *gin.Context) {
	var request IAMCreateTenantRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid tenant request", commonapi.ErrBadRequest))
		return
	}
	actorID, err := iamPlatformUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	administratorID, err := parseIAMDecimalID(request.InitialAdministratorPrincipalID)
	if err != nil {
		respondIAMError(c, fmt.Errorf("%w: initial administrator is required", commonapi.ErrBadRequest))
		return
	}
	tenant, err := h.service.Create(c.Request.Context(), iam.CreateTenantInput{
		Code: request.Code, Name: request.Name, Description: request.Description,
		InitialAdministratorPrincipalID: administratorID, ActorPrincipalID: actorID,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMTenant(*tenant))
}

// Initialize godoc
// @Summary      初始化既有租户 | Initialize existing tenant
// @Description  为尚无成员和角色分配的既有租户原子建立首位租户管理员 | Atomically establish the first tenant administrator for an existing tenant without memberships or assignments
// @Tags         平台租户 | Platform Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "租户 ID | Tenant ID"
// @Param        request body IAMInitializeTenantRequest true "首位租户管理员 | Initial tenant administrator"
// @Success      200 {object} IAMTenantResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.initialize"]
// @Router       /platform/tenants/{id}/initialization [post]
func (h *IAMPlatformTenantHandler) Initialize(c *gin.Context) {
	tenantID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMInitializeTenantRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid tenant initialization request", commonapi.ErrBadRequest))
		return
	}
	administratorID, err := parseIAMDecimalID(request.InitialAdministratorPrincipalID)
	if err != nil {
		respondIAMError(c, fmt.Errorf("%w: initial administrator is required", commonapi.ErrBadRequest))
		return
	}
	actorID, err := iamPlatformUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	tenant, err := h.service.Initialize(c.Request.Context(), iam.InitializeTenantInput{
		TenantID: tenantID, InitialAdministratorPrincipalID: administratorID, ActorPrincipalID: actorID,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenant(*tenant))
}

// ListAdministratorCandidates godoc
// @Summary      查询首位租户管理员候选人 | List initial tenant administrator candidates
// @Description  仅返回有效且未持有平台角色的普通用户 | Return only active users without an effective platform role
// @Tags         平台租户 | Platform Tenants
// @Produce      json
// @Security     BearerAuth
// @Param        search query string false "姓名、邮箱或用户名 | Name, email, or username"
// @Success      200 {array} IAMTenantAdministratorCandidateResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.create"]
// @Router       /platform/tenant_administrator_candidates [get]
func (h *IAMPlatformTenantHandler) ListAdministratorCandidates(c *gin.Context) {
	candidates, err := h.service.ListAdministratorCandidates(c.Request.Context(), c.Query("search"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMTenantAdministratorCandidateResponse, 0, len(candidates))
	for _, candidate := range candidates {
		responses = append(responses, IAMTenantAdministratorCandidateResponse{
			PrincipalID: strconv.FormatInt(candidate.PrincipalID, 10), DisplayName: candidate.DisplayName,
			PrimaryEmail: candidate.PrimaryEmail, Username: candidate.Username,
		})
	}
	c.JSON(http.StatusOK, responses)
}

// Update godoc
// @Summary      更新平台租户 | Update platform tenant
// @Tags         平台租户 | Platform Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "租户 ID | Tenant ID"
// @Param        request body IAMUpdateTenantRequest true "租户资料 | Tenant profile"
// @Success      200 {object} IAMTenantResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.update"]
// @Router       /platform/tenants/{id} [put]
func (h *IAMPlatformTenantHandler) Update(c *gin.Context) {
	tenantID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMUpdateTenantRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid tenant request", commonapi.ErrBadRequest))
		return
	}
	tenant, err := h.service.Update(c.Request.Context(), iam.UpdateTenantInput{
		TenantID: tenantID, Name: request.Name, Description: request.Description,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenant(*tenant))
}

// Suspend godoc
// @Summary      暂停平台租户 | Suspend platform tenant
// @Tags         平台租户 | Platform Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "租户 ID | Tenant ID"
// @Param        request body IAMLifecycleReasonRequest false "原因 | Reason"
// @Success      200 {object} IAMTenantStatusResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.suspend"]
// @Router       /platform/tenants/{id}/suspend [post]
func (h *IAMPlatformTenantHandler) Suspend(c *gin.Context) { h.changeStatus(c, h.service.Suspend) }

// Restore godoc
// @Summary      恢复平台租户 | Restore platform tenant
// @Tags         平台租户 | Platform Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "租户 ID | Tenant ID"
// @Param        request body IAMLifecycleReasonRequest false "原因 | Reason"
// @Success      200 {object} IAMTenantStatusResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.restore"]
// @Router       /platform/tenants/{id}/restore [post]
func (h *IAMPlatformTenantHandler) Restore(c *gin.Context) { h.changeStatus(c, h.service.Restore) }

// Close godoc
// @Summary      关闭平台租户 | Close platform tenant
// @Tags         平台租户 | Platform Tenants
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "租户 ID | Tenant ID"
// @Param        request body IAMLifecycleReasonRequest false "原因 | Reason"
// @Success      200 {object} IAMTenantStatusResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["platform.tenant.close"]
// @Router       /platform/tenants/{id}/close [post]
func (h *IAMPlatformTenantHandler) Close(c *gin.Context) { h.changeStatus(c, h.service.Close) }

func (h *IAMPlatformTenantHandler) changeStatus(
	c *gin.Context,
	change func(context.Context, iam.ChangeTenantStatusInput) (*iam.TenantStatusChangeResult, error),
) {
	tenantID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMLifecycleReasonRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid lifecycle request", commonapi.ErrBadRequest))
		return
	}
	result, err := change(c.Request.Context(), iam.ChangeTenantStatusInput{
		TenantID: tenantID, Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMTenantStatusResponse{
		Tenant: mapIAMTenant(result.Tenant), AffectedPrincipals: result.AffectedPrincipals,
		RevokedFamilyCount: result.RevokedFamilyCount,
	})
}

type iamPlatformUserService interface {
	List(context.Context, int, int, string, *iam.PrincipalStatus) ([]iam.ManagedUser, int64, error)
	Get(context.Context, int64) (*iam.ManagedUser, error)
	Create(context.Context, iam.CreateManagedLocalUserInput) (*iam.ManagedUser, error)
	Update(context.Context, iam.UpdateManagedUserInput) (*iam.ManagedUser, error)
	ResetLocalAccountPassword(context.Context, iam.ResetManagedLocalAccountPasswordInput) (*iam.ManagedLocalAccountPasswordResetResult, error)
	Suspend(context.Context, iam.ChangeManagedUserStatusInput) (*iam.ManagedUserStatusChangeResult, error)
	Reactivate(context.Context, iam.ChangeManagedUserStatusInput) (*iam.ManagedUserStatusChangeResult, error)
}

type IAMPlatformUserHandler struct {
	service iamPlatformUserService
}

func NewIAMPlatformUserHandler(service iamPlatformUserService) (*IAMPlatformUserHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: platform user service is required", commonapi.ErrBadRequest)
	}
	return &IAMPlatformUserHandler{service: service}, nil
}

// List godoc
// @Summary      查询平台用户 | List platform users
// @Tags         平台用户 | Platform Users
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number"
// @Param        page_size query int false "每页数量 | Page size"
// @Param        search query string false "姓名、邮箱或用户名 | Name, email, or username"
// @Param        status query string false "状态 | Status"
// @Success      200 {object} object{data=[]IAMManagedUserResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.user.read"]
// @Router       /platform/users [get]
func (h *IAMPlatformUserHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	status, err := parsePrincipalStatusFilter(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	users, total, err := h.service.List(c.Request.Context(), page, pageSize, c.Query("search"), status)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMManagedUserResponse, 0, len(users))
	for _, user := range users {
		responses = append(responses, mapIAMManagedUser(user))
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

// Get godoc
// @Summary      查询平台用户详情 | Get platform user
// @Tags         平台用户 | Platform Users
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户 ID | User ID"
// @Success      200 {object} IAMManagedUserResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.user.read"]
// @Router       /platform/users/{id} [get]
func (h *IAMPlatformUserHandler) Get(c *gin.Context) {
	userID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	user, err := h.service.Get(c.Request.Context(), userID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMManagedUser(*user))
}

// Create godoc
// @Summary      创建本地平台用户 | Create local platform user
// @Tags         平台用户 | Platform Users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCreateManagedUserRequest true "用户与本地账号 | User and local account"
// @Success      201 {object} IAMManagedUserResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.user.create"]
// @Router       /platform/users [post]
func (h *IAMPlatformUserHandler) Create(c *gin.Context) {
	var request IAMCreateManagedUserRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid user request", commonapi.ErrBadRequest))
		return
	}
	user, err := h.service.Create(c.Request.Context(), iam.CreateManagedLocalUserInput{
		Username: request.LocalAccount.Username, Password: request.LocalAccount.Password,
		DisplayName: request.DisplayName, PrimaryEmail: request.PrimaryEmail, Locale: request.Locale,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMManagedUser(*user))
}

// Update godoc
// @Summary      更新平台用户 | Update platform user
// @Tags         平台用户 | Platform Users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户 ID | User ID"
// @Param        request body IAMUpdateManagedUserRequest true "用户资料 | User profile"
// @Success      200 {object} IAMManagedUserResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.user.update"]
// @Router       /platform/users/{id} [put]
func (h *IAMPlatformUserHandler) Update(c *gin.Context) {
	userID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMUpdateManagedUserRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid user request", commonapi.ErrBadRequest))
		return
	}
	user, err := h.service.Update(c.Request.Context(), iam.UpdateManagedUserInput{
		UserID: userID, DisplayName: request.DisplayName,
		PrimaryEmail: request.PrimaryEmail, Locale: request.Locale,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMManagedUser(*user))
}

// ResetLocalAccountPassword godoc
// @Summary      重置普通用户本地密码 | Reset ordinary user local password
// @Description  仅允许重置不持有有效平台角色的普通用户，并撤销其全部既有认证会话 | Reset only an ordinary user without an effective platform role and revoke all existing authentication sessions
// @Tags         平台用户 | Platform Users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户 ID | User ID"
// @Param        request body IAMResetManagedLocalAccountPasswordRequest true "新密码与原因 | New password and reason"
// @Success      200 {object} IAMManagedLocalAccountPasswordResetResponse
// @Failure      400 {object} IAMErrorResponse
// @Failure      403 {object} IAMErrorResponse
// @Failure      404 {object} IAMErrorResponse
// @Failure      409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.local_account.reset"]
// @Router       /platform/users/{id}/reset-password [post]
func (h *IAMPlatformUserHandler) ResetLocalAccountPassword(c *gin.Context) {
	userID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMResetManagedLocalAccountPasswordRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid local account password reset request", commonapi.ErrBadRequest))
		return
	}
	result, err := h.service.ResetLocalAccountPassword(
		c.Request.Context(),
		iam.ResetManagedLocalAccountPasswordInput{
			UserID: userID, NewPassword: request.NewPassword, Reason: request.Reason,
			Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
		},
	)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMManagedLocalAccountPasswordResetResponse{
		AuthorizationVersion:       strconv.FormatInt(result.AuthorizationVersion, 10),
		RevokedFamilyCount:         result.RevokedFamilyCount,
		ConsumedMFAChallengeCount:  result.ConsumedMFAChallengeCount,
		ConsumedContextTicketCount: result.ConsumedContextTicketCount,
		ChangedAt:                  result.ChangedAt.UTC(),
	})
}

// Suspend godoc
// @Summary      暂停平台用户 | Suspend platform user
// @Tags         平台用户 | Platform Users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户 ID | User ID"
// @Param        request body IAMChangeManagedUserStatusRequest true "状态变更 | Status change"
// @Success      200 {object} IAMManagedUserStatusResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.user.suspend"]
// @Router       /platform/users/{id}/suspend [post]
func (h *IAMPlatformUserHandler) Suspend(c *gin.Context) {
	h.changeStatus(c, h.service.Suspend)
}

// Reactivate godoc
// @Summary      重新激活平台用户 | Reactivate platform user
// @Tags         平台用户 | Platform Users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "用户 ID | User ID"
// @Param        request body IAMChangeManagedUserStatusRequest true "状态变更 | Status change"
// @Success      200 {object} IAMManagedUserStatusResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.user.reactivate"]
// @Router       /platform/users/{id}/reactivate [post]
func (h *IAMPlatformUserHandler) Reactivate(c *gin.Context) {
	h.changeStatus(c, h.service.Reactivate)
}

func (h *IAMPlatformUserHandler) changeStatus(
	c *gin.Context,
	change func(context.Context, iam.ChangeManagedUserStatusInput) (*iam.ManagedUserStatusChangeResult, error),
) {
	userID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMChangeManagedUserStatusRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid user lifecycle request", commonapi.ErrBadRequest))
		return
	}
	var changeRequestID *int64
	if request.ChangeRequestID != nil {
		parsed, err := parseIAMDecimalID(*request.ChangeRequestID)
		if err != nil {
			respondIAMError(c, err)
			return
		}
		changeRequestID = &parsed
	}
	result, err := change(c.Request.Context(), iam.ChangeManagedUserStatusInput{
		UserID: userID, Reason: request.Reason, ChangeRequestID: changeRequestID,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMManagedUserStatusResponse{
		User: mapIAMManagedUser(result.User), RevokedFamilyCount: result.RevokedFamilyCount,
	})
}

func mapIAMTenant(tenant iam.ManagedTenant) IAMTenantResponse {
	return IAMTenantResponse{
		ID: strconv.FormatInt(tenant.ID, 10), Code: tenant.Code, Name: tenant.Name,
		Description: tenant.Description, Status: tenant.Status, Initialized: tenant.Initialized,
		CreatedAt: tenant.CreatedAt.UTC(), UpdatedAt: tenant.UpdatedAt.UTC(),
	}
}

func mapIAMManagedUser(user iam.ManagedUser) IAMManagedUserResponse {
	response := IAMManagedUserResponse{
		ID: strconv.FormatInt(user.ID, 10), Status: user.Status,
		AuthorizationVersion: strconv.FormatInt(user.AuthorizationVersion, 10),
		DisplayName:          user.DisplayName, PrimaryEmail: user.PrimaryEmail, Locale: user.Locale,
		CreatedAt: user.CreatedAt.UTC(), UpdatedAt: user.UpdatedAt.UTC(),
	}
	if user.AccountID != nil && user.Username != nil && user.LocalAccountStatus != nil {
		response.LocalAccount = &IAMLocalAccountManagementResponse{
			ID: strconv.FormatInt(*user.AccountID, 10), Username: *user.Username,
			Status: *user.LocalAccountStatus,
			PasswordResetAllowed: user.Status == iam.PrincipalStatusActive &&
				(*user.LocalAccountStatus == iam.LocalAccountStatusActive ||
					*user.LocalAccountStatus == iam.LocalAccountStatusLocked) &&
				!user.HasEffectivePlatformRole,
		}
	}
	return response
}

func parseTenantStatusFilter(value string) (*iam.TenantStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	status := iam.TenantStatus(value)
	switch status {
	case iam.TenantStatusActive, iam.TenantStatusSuspended, iam.TenantStatusClosed:
		return &status, nil
	default:
		return nil, fmt.Errorf("%w: invalid tenant status", commonapi.ErrBadRequest)
	}
}

func parsePrincipalStatusFilter(value string) (*iam.PrincipalStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	status := iam.PrincipalStatus(value)
	switch status {
	case iam.PrincipalStatusActive, iam.PrincipalStatusSuspended, iam.PrincipalStatusDeactivated:
		return &status, nil
	default:
		return nil, fmt.Errorf("%w: invalid user status", commonapi.ErrBadRequest)
	}
}
