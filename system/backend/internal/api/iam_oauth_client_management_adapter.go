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

type IAMOAuthClientResponse struct {
	ClientID             string                `json:"client_id"`
	DisplayName          string                `json:"display_name"`
	OwnerScope           string                `json:"owner_scope"`
	RedirectURIs         []string              `json:"redirect_uris"`
	ClientType           string                `json:"client_type"`
	GrantTypes           []string              `json:"grant_types"`
	ResponseTypes        []string              `json:"response_types"`
	AllowedScopes        []string              `json:"allowed_scopes"`
	AllowedAudiences     []string              `json:"allowed_audiences"`
	TokenAuthMethod      string                `json:"token_endpoint_auth_method"`
	Status               iam.OAuthClientStatus `json:"status"`
	Version              int64                 `json:"version"`
	CreatedByPrincipalID string                `json:"created_by_principal_id"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

type IAMCreateOAuthClientRequest struct {
	DisplayName  string   `json:"display_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

type IAMUpdateOAuthClientRequest struct {
	DisplayName  string   `json:"display_name"`
	RedirectURIs []string `json:"redirect_uris"`
	Version      int64    `json:"version"`
}

type IAMOAuthClientManagementHandler struct {
	service *iam.OAuthClientManagementService
}

func NewIAMOAuthClientManagementHandler(service *iam.OAuthClientManagementService) (*IAMOAuthClientManagementHandler, error) {
	if service == nil {
		return nil, commonapi.ErrBadRequest
	}
	return &IAMOAuthClientManagementHandler{service: service}, nil
}

// List godoc
// @Summary 查询外部 OAuth 客户端 | List external OAuth clients
// @Description 分页查询当前 Tenant 管理的公共 OAuth Client | List public OAuth clients managed by the current tenant
// @Tags 租户 OAuth 客户端 | Tenant OAuth Clients
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量 | Page size"
// @Param search query string false "客户端 ID 或名称 | Client ID or name"
// @Param status query string false "状态：active/disabled | Status: active/disabled"
// @Success 200 {object} object{data=[]IAMOAuthClientResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.oauth_client.read"]
// @Router /tenant/oauth_clients [get]
func (h *IAMOAuthClientManagementHandler) List(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	status, err := parseManagedOAuthClientStatus(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	clients, total, err := h.service.List(c.Request.Context(), int64(tenantID), page, pageSize, c.Query("search"), status)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	data := make([]IAMOAuthClientResponse, 0, len(clients))
	for _, client := range clients {
		data = append(data, mapIAMOAuthClient(client))
	}
	commonapi.RespondPaginated(c, data, total, page, pageSize)
}

// Create godoc
// @Summary 创建外部 OAuth 客户端 | Create external OAuth client
// @Description 创建固定使用 Authorization Code、PKCE 和 Refresh Token 的无密钥公共客户端 | Create a secretless public client fixed to Authorization Code, PKCE, and Refresh Token
// @Tags 租户 OAuth 客户端 | Tenant OAuth Clients
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body IAMCreateOAuthClientRequest true "客户端定义 | Client definition"
// @Success 201 {object} IAMOAuthClientResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.oauth_client.create"]
// @Router /tenant/oauth_clients [post]
func (h *IAMOAuthClientManagementHandler) Create(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	var request IAMCreateOAuthClientRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	client, err := h.service.Create(c.Request.Context(), iam.CreateManagedOAuthClientInput{
		TenantID: int64(tenantID), ActorPrincipalID: int64(actorID),
		DisplayName: request.DisplayName, RedirectURIs: request.RedirectURIs,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMOAuthClient(*client))
}

// Get godoc
// @Summary 查询外部 OAuth 客户端详情 | Get external OAuth client
// @Tags 租户 OAuth 客户端 | Tenant OAuth Clients
// @Produce json
// @Security BearerAuth
// @Param client_id path string true "OAuth Client ID"
// @Success 200 {object} IAMOAuthClientResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.oauth_client.read"]
// @Router /tenant/oauth_clients/{client_id} [get]
func (h *IAMOAuthClientManagementHandler) Get(c *gin.Context) {
	_, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	client, err := h.service.Get(c.Request.Context(), int64(tenantID), c.Param("client_id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMOAuthClient(*client))
}

// Update godoc
// @Summary 更新外部 OAuth 客户端 | Update external OAuth client
// @Description 完整更新显示名称和 redirect URI；协议配置不可修改 | Fully update display name and redirect URIs; protocol settings are immutable
// @Tags 租户 OAuth 客户端 | Tenant OAuth Clients
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param client_id path string true "OAuth Client ID"
// @Param request body IAMUpdateOAuthClientRequest true "客户端定义与版本 | Client definition and version"
// @Success 200 {object} IAMOAuthClientResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.oauth_client.update"]
// @Router /tenant/oauth_clients/{client_id} [put]
func (h *IAMOAuthClientManagementHandler) Update(c *gin.Context) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	var request IAMUpdateOAuthClientRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	client, err := h.service.Update(c.Request.Context(), iam.UpdateManagedOAuthClientInput{
		TenantID: int64(tenantID), ActorPrincipalID: int64(actorID), ClientID: c.Param("client_id"),
		DisplayName: request.DisplayName, RedirectURIs: request.RedirectURIs, Version: request.Version,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMOAuthClient(*client))
}

// Suspend godoc
// @Summary 停用外部 OAuth 客户端 | Disable external OAuth client
// @Description 停用并撤销该客户端的待授权请求和全部有效令牌族 | Disable and revoke pending authorizations and all active token families for the client
// @Tags 租户 OAuth 客户端 | Tenant OAuth Clients
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param client_id path string true "OAuth Client ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMOAuthClientResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.oauth_client.suspend"]
// @Router /tenant/oauth_clients/{client_id}/suspend [post]
func (h *IAMOAuthClientManagementHandler) Suspend(c *gin.Context) {
	h.changeStatus(c, true)
}

// Restore godoc
// @Summary 恢复外部 OAuth 客户端 | Restore external OAuth client
// @Description 恢复注册状态；历史授权和令牌不会恢复 | Restore registration state; historical authorizations and tokens remain revoked
// @Tags 租户 OAuth 客户端 | Tenant OAuth Clients
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param client_id path string true "OAuth Client ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} IAMOAuthClientResponse
// @Failure 409 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.oauth_client.restore"]
// @Router /tenant/oauth_clients/{client_id}/restore [post]
func (h *IAMOAuthClientManagementHandler) Restore(c *gin.Context) {
	h.changeStatus(c, false)
}

func (h *IAMOAuthClientManagementHandler) changeStatus(c *gin.Context, disable bool) {
	actorID, tenantID, ok := organizationActor(c)
	if !ok {
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	input := iam.ChangeManagedOAuthClientStatusInput{
		TenantID: int64(tenantID), ActorPrincipalID: int64(actorID), ClientID: c.Param("client_id"),
		Version: request.Version, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}
	var client *iam.ManagedOAuthClient
	var err error
	if disable {
		client, err = h.service.Disable(c.Request.Context(), input)
	} else {
		client, err = h.service.Restore(c.Request.Context(), input)
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMOAuthClient(*client))
}

func parseManagedOAuthClientStatus(raw string) (*iam.OAuthClientStatus, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	status := iam.OAuthClientStatus(strings.TrimSpace(raw))
	if status != iam.OAuthClientStatusActive && status != iam.OAuthClientStatusDisabled {
		return nil, commonapi.ErrBadRequest
	}
	return &status, nil
}

func mapIAMOAuthClient(client iam.ManagedOAuthClient) IAMOAuthClientResponse {
	return IAMOAuthClientResponse{
		ClientID: client.ClientID, DisplayName: client.DisplayName, OwnerScope: "tenant",
		RedirectURIs: append([]string(nil), client.RedirectURIs...), ClientType: "public",
		GrantTypes: append([]string(nil), client.GrantTypes...), ResponseTypes: append([]string(nil), client.ResponseTypes...),
		AllowedScopes: append([]string(nil), client.AllowedScopes...), AllowedAudiences: append([]string(nil), client.AllowedAudiences...),
		TokenAuthMethod: client.TokenAuthMethod, Status: client.Status, Version: client.Version,
		CreatedByPrincipalID: strconv.FormatInt(client.CreatedByPrincipal, 10),
		CreatedAt:            client.CreatedAt.UTC(), UpdatedAt: client.UpdatedAt.UTC(),
	}
}
