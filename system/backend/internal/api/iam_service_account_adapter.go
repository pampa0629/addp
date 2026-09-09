package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMServiceAccountResponse struct {
	ID                   string                     `json:"id"`
	Name                 string                     `json:"name"`
	Description          string                     `json:"description"`
	OwnerScope           string                     `json:"owner_scope" enums:"platform,tenant"`
	Status               iam.PrincipalStatus        `json:"status"`
	MembershipID         string                     `json:"membership_id"`
	MembershipStatus     iam.TenantMembershipStatus `json:"membership_status"`
	ClientID             string                     `json:"client_id"`
	CredentialStatus     iam.OAuthClientStatus      `json:"credential_status"`
	Version              int64                      `json:"version"`
	CreatedByPrincipalID string                     `json:"created_by_principal_id"`
	CreatedAt            time.Time                  `json:"created_at"`
	UpdatedAt            time.Time                  `json:"updated_at"`
}

type IAMServiceAccountCredentialResponse struct {
	IAMServiceAccountResponse
	ClientSecret string `json:"client_secret"`
}

type IAMCreateServiceAccountRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type IAMUpdateServiceAccountRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
}

type IAMTenantServiceAccountHandler struct {
	service *iam.TenantServiceAccountService
}

func NewIAMTenantServiceAccountHandler(service *iam.TenantServiceAccountService) (*IAMTenantServiceAccountHandler, error) {
	if service == nil {
		return nil, commonapi.ErrBadRequest
	}
	return &IAMTenantServiceAccountHandler{service: service}, nil
}

// List godoc
// @Summary 查询服务账号 | List service accounts
// @Description 分页查询当前 Tenant 可用的机器身份；Tenant-owned 服务账号可管理，Platform-owned Runtime 服务主体只读 | List machine identities available in the current tenant; tenant-owned service accounts are manageable and platform-owned runtime identities are read-only
// @Tags 租户服务账号 | Tenant Service Accounts
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param search query string false "名称、说明或 Client ID | Name, description, or client ID"
// @Param status query string false "状态：active/suspended | Status: active/suspended"
// @Param owner_scope query string false "管理归属：tenant/platform | Management ownership: tenant/platform"
// @Success 200 {object} object{data=[]IAMServiceAccountResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.service_account.read"]
// @Router /tenant/service_accounts [get]
func (h *IAMTenantServiceAccountHandler) List(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	status, err := parseServiceAccountStatus(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	ownerScope, err := parseServiceAccountOwnerScope(c.Query("owner_scope"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	accounts, total, err := h.service.List(c.Request.Context(), int64(tenantID), page, pageSize, c.Query("search"), status, ownerScope)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	data := make([]IAMServiceAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		data = append(data, mapIAMServiceAccount(account))
	}
	commonapi.RespondPaginated(c, data, total, page, pageSize)
}

// Create godoc
// @Summary 创建服务账号 | Create service account
// @Description 原子创建服务主体、租户成员关系和 Client Credentials 凭据；Client Secret 仅在本响应展示一次 | Atomically create the identity, membership, and credential; the client secret is returned once
// @Tags 租户服务账号 | Tenant Service Accounts
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body IAMCreateServiceAccountRequest true "服务账号定义 | Service account definition"
// @Success 201 {object} IAMServiceAccountCredentialResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.service_account.create"]
// @Router /tenant/service_accounts [post]
func (h *IAMTenantServiceAccountHandler) Create(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	var request IAMCreateServiceAccountRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	created, err := h.service.Create(c.Request.Context(), iam.CreateTenantServiceAccountInput{
		TenantID: int64(tenantID), ActorPrincipalID: int64(actorID), Name: request.Name,
		Description: request.Description, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMServiceAccountCredentialResponse{
		IAMServiceAccountResponse: mapIAMServiceAccount(created.Account), ClientSecret: created.ClientSecret,
	})
}

// Get godoc
// @Summary 查询服务账号详情 | Get service account
// @Tags 租户服务账号 | Tenant Service Accounts
// @Produce json
// @Security BearerAuth
// @Param id path string true "服务账号 ID | Service account ID"
// @Success 200 {object} IAMServiceAccountResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.service_account.read"]
// @Router /tenant/service_accounts/{id} [get]
func (h *IAMTenantServiceAccountHandler) Get(c *gin.Context) {
	_, tenantID, accountID, ok := serviceAccountActorAndID(c)
	if !ok {
		return
	}
	account, err := h.service.Get(c.Request.Context(), int64(tenantID), accountID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMServiceAccount(*account))
}

// Update godoc
// @Summary 更新服务账号 | Update service account
// @Description 完整更新名称与说明；Client ID 不可修改 | Fully update name and description; client ID is immutable
// @Tags 租户服务账号 | Tenant Service Accounts
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "服务账号 ID | Service account ID"
// @Param request body IAMUpdateServiceAccountRequest true "定义与版本 | Definition and version"
// @Success 200 {object} IAMServiceAccountResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.service_account.update"]
// @Router /tenant/service_accounts/{id} [put]
func (h *IAMTenantServiceAccountHandler) Update(c *gin.Context) {
	actorID, tenantID, accountID, ok := serviceAccountActorAndID(c)
	if !ok {
		return
	}
	var request IAMUpdateServiceAccountRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	account, err := h.service.Update(c.Request.Context(), iam.UpdateTenantServiceAccountInput{
		TenantID: int64(tenantID), AccountID: accountID, Version: request.Version,
		ActorPrincipalID: int64(actorID), Name: request.Name, Description: request.Description,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMServiceAccount(*account))
}

// Suspend godoc
// @Summary 暂停服务账号 | Suspend service account
// @Description 原子暂停服务主体、成员关系和 Client Credentials 凭据，并使已签发令牌失效 | Atomically suspend the identity, membership, and credential and invalidate issued tokens
// @Tags 租户服务账号 | Tenant Service Accounts
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "服务账号 ID | Service account ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMServiceAccountResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.service_account.suspend"]
// @Router /tenant/service_accounts/{id}/suspend [post]
func (h *IAMTenantServiceAccountHandler) Suspend(c *gin.Context) {
	h.changeStatus(c, true)
}

// Restore godoc
// @Summary 恢复服务账号 | Restore service account
// @Tags 租户服务账号 | Tenant Service Accounts
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "服务账号 ID | Service account ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMServiceAccountResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.service_account.restore"]
// @Router /tenant/service_accounts/{id}/restore [post]
func (h *IAMTenantServiceAccountHandler) Restore(c *gin.Context) {
	h.changeStatus(c, false)
}

// RotateSecret godoc
// @Summary 轮换服务账号密钥 | Rotate service account secret
// @Description 旧 Client Secret 立即失效，新 Secret 仅在本响应展示一次 | Invalidate the old client secret and return the new secret once
// @Tags 租户服务账号 | Tenant Service Accounts
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "服务账号 ID | Service account ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMServiceAccountCredentialResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.service_credential.update"]
// @Router /tenant/service_accounts/{id}/rotate-secret [post]
func (h *IAMTenantServiceAccountHandler) RotateSecret(c *gin.Context) {
	actorID, tenantID, accountID, ok := serviceAccountActorAndID(c)
	if !ok {
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	credential, err := h.service.RotateSecret(c.Request.Context(), iam.RotateTenantServiceAccountSecretInput{
		TenantID: int64(tenantID), AccountID: accountID, Version: request.Version,
		ActorPrincipalID: int64(actorID), Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, IAMServiceAccountCredentialResponse{
		IAMServiceAccountResponse: mapIAMServiceAccount(credential.Account), ClientSecret: credential.ClientSecret,
	})
}

func (h *IAMTenantServiceAccountHandler) changeStatus(c *gin.Context, suspend bool) {
	actorID, tenantID, accountID, ok := serviceAccountActorAndID(c)
	if !ok {
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	input := iam.ChangeTenantServiceAccountStatusInput{
		TenantID: int64(tenantID), AccountID: accountID, Version: request.Version,
		ActorPrincipalID: int64(actorID), Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}
	var account *iam.TenantServiceAccount
	var err error
	if suspend {
		account, err = h.service.Suspend(c.Request.Context(), input)
	} else {
		account, err = h.service.Restore(c.Request.Context(), input)
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMServiceAccount(*account))
}

func serviceAccountActorAndID(c *gin.Context) (uint, uint, int64, bool) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return 0, 0, 0, false
	}
	accountID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return 0, 0, 0, false
	}
	return actorID, tenantID, accountID, true
}

func parseServiceAccountStatus(raw string) (*iam.PrincipalStatus, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	status := iam.PrincipalStatus(strings.TrimSpace(raw))
	if status != iam.PrincipalStatusActive && status != iam.PrincipalStatusSuspended {
		return nil, commonapi.ErrBadRequest
	}
	return &status, nil
}

func parseServiceAccountOwnerScope(raw string) (*string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	ownerScope := strings.TrimSpace(raw)
	if ownerScope != "platform" && ownerScope != "tenant" {
		return nil, commonapi.ErrBadRequest
	}
	return &ownerScope, nil
}

func mapIAMServiceAccount(account iam.TenantServiceAccount) IAMServiceAccountResponse {
	return IAMServiceAccountResponse{
		ID: strconv.FormatInt(account.ID, 10), Name: account.Name, Description: account.Description,
		OwnerScope: account.OwnerScope, Status: account.Status, MembershipID: strconv.FormatInt(account.MembershipID, 10),
		MembershipStatus: account.MembershipStatus, ClientID: account.ClientID,
		CredentialStatus: account.CredentialStatus, Version: account.Version,
		CreatedByPrincipalID: strconv.FormatInt(account.CreatedByPrincipalID, 10),
		CreatedAt:            account.CreatedAt.UTC(), UpdatedAt: account.UpdatedAt.UTC(),
	}
}
