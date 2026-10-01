package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type engineAccessDelegationService interface {
	List(context.Context, int64, int64, int, int) ([]engineaccess.View, int64, error)
	Get(context.Context, int64, int64, int64) (*engineaccess.View, error)
	Create(context.Context, engineaccess.CreateInput) (*engineaccess.View, error)
	Revoke(context.Context, engineaccess.RevokeInput) (*engineaccess.View, error)
}
type EngineAccessDelegationHandler struct{ service engineAccessDelegationService }

func NewEngineAccessDelegationHandler(service engineAccessDelegationService) *EngineAccessDelegationHandler {
	return &EngineAccessDelegationHandler{service: service}
}

type CreateEngineAccessDelegationRequest struct {
	TenantMembershipID string    `json:"tenant_membership_id" binding:"required"`
	ExpiresAt          time.Time `json:"expires_at" binding:"required"`
	Reason             string    `json:"reason" binding:"required"`
}
type EngineAccessDelegationResponse struct {
	ID                   string     `json:"id"`
	EngineID             string     `json:"engine_id"`
	EngineName           string     `json:"engine_name"`
	TenantMembershipID   string     `json:"tenant_membership_id"`
	PrincipalID          string     `json:"principal_id"`
	DisplayName          string     `json:"display_name"`
	Status               string     `json:"status"`
	EffectiveState       string     `json:"effective_state"`
	Version              int64      `json:"version"`
	GrantedByPrincipalID string     `json:"granted_by_principal_id"`
	GrantedAt            time.Time  `json:"granted_at"`
	ExpiresAt            time.Time  `json:"expires_at"`
	GrantReason          string     `json:"grant_reason"`
	RevokedByPrincipalID *string    `json:"revoked_by_principal_id"`
	RevokedAt            *time.Time `json:"revoked_at"`
	RevokedReason        *string    `json:"revoked_reason"`
}

// List godoc
// @Summary 查询引擎授权管理委派 | List engine access management delegations
// @Description 当前租户的管理资格及历史，不返回连接凭据或内容授权 | Current tenant management qualification and history, without connection credentials or content grants
// @Tags 引擎授权管理委派 | Engine Access Management Delegations
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param page query int false "页码，默认 1 | Page, default 1"
// @Param page_size query int false "每页数量，默认 10，最多 100 | Page size, default 10, maximum 100"
// @Success 200 {object} object{data=[]EngineAccessDelegationResponse,total=int64,page=int,page_size=int,total_pages=int} "委派列表 | Delegations"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_delegation.read"]
// @Router /engines/{id}/access_delegations [get]
func (h *EngineAccessDelegationHandler) List(c *gin.Context) {
	_, tenant, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, size := commonapi.ParsePagination(c)
	rows, total, err := h.service.List(c.Request.Context(), int64(tenant), engineID, page, size)
	if err != nil {
		respondEngineDelegationError(c, err)
		return
	}
	responses := make([]EngineAccessDelegationResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, mapEngineAccessDelegation(row))
	}
	commonapi.RespondPaginated(c, responses, total, page, size)
}

// Get godoc
// @Summary 读取引擎授权管理委派 | Get engine access management delegation
// @Description 按租户和引擎隔离读取，不存在与跨租户均返回 404 | Tenant and engine scoped lookup; missing and cross-tenant IDs return 404
// @Tags 引擎授权管理委派 | Engine Access Management Delegations
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param delegation_id path string true "委派 ID | Delegation ID"
// @Success 200 {object} EngineAccessDelegationResponse "委派详情 | Delegation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_delegation.read"]
// @Router /engines/{id}/access_delegations/{delegation_id} [get]
func (h *EngineAccessDelegationHandler) Get(c *gin.Context) {
	_, tenant, ok := organizationActor(c)
	if !ok || rejectTenantIDQuery(c) {
		return
	}
	engineID, id, err := engineDelegationPath(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	row, err := h.service.Get(c.Request.Context(), int64(tenant), engineID, id)
	if err != nil {
		respondEngineDelegationError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapEngineAccessDelegation(*row))
}

// Create godoc
// @Summary 创建引擎授权管理委派 | Create engine access management delegation
// @Description 显式选取有效用户成员、到期时间与原因；不授予数据读取、写入、DDL 或转委派 | Explicit active user membership, expiry and reason; grants no data read, write, DDL or redelegation
// @Tags 引擎授权管理委派 | Engine Access Management Delegations
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request body CreateEngineAccessDelegationRequest true "委派信息 | Delegation definition"
// @Success 201 {object} EngineAccessDelegationResponse "已创建 | Created"
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_delegation.create"]
// @Router /engines/{id}/access_delegations [post]
func (h *EngineAccessDelegationHandler) Create(c *gin.Context) {
	if rejectTenantIDQuery(c) {
		return
	}
	actor, err := engineDelegationActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request CreateEngineAccessDelegationRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	memberID, err := parseIAMDecimalID(request.TenantMembershipID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	row, err := h.service.Create(c.Request.Context(), engineaccess.CreateInput{Actor: actor, EngineID: engineID,
		TenantMembershipID: memberID, ExpiresAt: request.ExpiresAt, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondEngineDelegationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapEngineAccessDelegation(*row))
}

// Revoke godoc
// @Summary 撤销引擎授权管理委派 | Revoke engine access management delegation
// @Description 版本化撤销并保留历史；不允许恢复或修改已到期记录 | Versioned revocation preserves history; no restore or expired record mutation
// @Tags 引擎授权管理委派 | Engine Access Management Delegations
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param delegation_id path string true "委派 ID | Delegation ID"
// @Param request body IAMVersionedLifecycleRequest true "版本与原因 | Version and reason"
// @Success 200 {object} EngineAccessDelegationResponse "已撤销 | Revoked"
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_delegation.revoke"]
// @Router /engines/{id}/access_delegations/{delegation_id}/revoke [post]
func (h *EngineAccessDelegationHandler) Revoke(c *gin.Context) {
	if rejectTenantIDQuery(c) {
		return
	}
	actor, err := engineDelegationActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engineID, id, err := engineDelegationPath(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	request, ok := bindVersionedLifecycle(c)
	if !ok {
		return
	}
	row, err := h.service.Revoke(c.Request.Context(), engineaccess.RevokeInput{Actor: actor, EngineID: engineID, ID: id,
		Version: request.Version, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondEngineDelegationError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapEngineAccessDelegation(*row))
}
func engineDelegationPath(c *gin.Context) (int64, int64, error) {
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		return 0, 0, err
	}
	id, err := parseIAMDecimalID(c.Param("delegation_id"))
	return engineID, id, err
}
func engineDelegationActor(c *gin.Context) (engineaccess.Actor, error) {
	principalID, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		return engineaccess.Actor{}, err
	}
	projection, _ := middleware.IAMAuthContextFromGin(c)
	memberID, err := parseIAMDecimalID(*projection.Context.TenantMembershipID)
	if err != nil {
		return engineaccess.Actor{}, commonapi.ErrUnauthorized
	}
	version, err := parseIAMDecimalID(projection.Authorization.AuthorizationVersion)
	if err != nil {
		return engineaccess.Actor{}, commonapi.ErrUnauthorized
	}
	return engineaccess.Actor{TenantID: int64(tenantID), PrincipalID: int64(principalID), MembershipID: memberID,
		AuthorizationVersion: version, TokenExpiresAt: projection.Token.ExpiresAt}, nil
}
func mapEngineAccessDelegation(row engineaccess.View) EngineAccessDelegationResponse {
	var revoker *string
	if row.RevokedByPrincipalID != nil {
		value := strconv.FormatInt(*row.RevokedByPrincipalID, 10)
		revoker = &value
	}
	return EngineAccessDelegationResponse{ID: strconv.FormatInt(row.ID, 10), EngineID: strconv.FormatInt(row.EngineID, 10), EngineName: row.EngineName,
		TenantMembershipID: strconv.FormatInt(row.TenantMembershipID, 10), PrincipalID: strconv.FormatInt(row.PrincipalID, 10), DisplayName: row.DisplayName,
		Status: row.Status, EffectiveState: row.EffectiveState, Version: row.Version, GrantedByPrincipalID: strconv.FormatInt(row.GrantedByPrincipalID, 10),
		GrantedAt: row.GrantedAt, ExpiresAt: row.ExpiresAt, GrantReason: row.GrantReason,
		RevokedByPrincipalID: revoker, RevokedAt: row.RevokedAt, RevokedReason: row.RevokedReason}
}
func respondEngineDelegationError(c *gin.Context, err error) {
	key, code := "", ""
	switch {
	case errors.Is(err, engineaccess.ErrVersionConflict):
		key, code = "version_conflict", "resource_version_conflict"
	case errors.Is(err, engineaccess.ErrOverlap):
		key, code = "overlap", "engine_access_delegation_overlap"
	case errors.Is(err, engineaccess.ErrUnavailable):
		key, code = "unavailable", "engine_access_delegation_unavailable"
	case errors.Is(err, engineaccess.ErrExpired):
		key, code = "history_read_only", "engine_access_delegation_history_read_only"
	case errors.Is(err, engineaccess.ErrExpiry):
		key, code = "expiry_required", "engine_access_delegation_expiry_invalid"
	default:
		respondIAMError(c, err)
		return
	}
	c.JSON(commonapi.MapErrorToHTTPStatus(err), gin.H{"error": commoni18n.T(c, "system.engine_access_delegation."+key), "error_code": code})
}
