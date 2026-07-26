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

type IAMTenantMembershipResponse struct {
	ID                   string                     `json:"id"`
	PrincipalID          string                     `json:"principal_id"`
	PrincipalType        iam.PrincipalType          `json:"principal_type"`
	PrincipalStatus      iam.PrincipalStatus        `json:"principal_status"`
	DisplayName          string                     `json:"display_name"`
	Username             *string                    `json:"username"`
	Status               iam.TenantMembershipStatus `json:"status"`
	SourceType           iam.TenantMembershipSource `json:"source_type"`
	SourceRef            *string                    `json:"source_ref"`
	JoinedAt             time.Time                  `json:"joined_at"`
	ExpiresAt            *time.Time                 `json:"expires_at"`
	EndedAt              *time.Time                 `json:"ended_at"`
	CreatedByPrincipalID *string                    `json:"created_by_principal_id"`
	CreatedAt            time.Time                  `json:"created_at"`
	UpdatedAt            time.Time                  `json:"updated_at"`
}

type IAMUpdateTenantMembershipRequest struct {
	ExpiresAt *time.Time `json:"expires_at"`
}

type iamTenantMembershipService interface {
	ListManagedMemberships(context.Context, int64, int, int, string, *iam.TenantMembershipStatus) ([]iam.ManagedTenantMembership, int64, error)
	GetManagedMembership(context.Context, int64, int64) (*iam.ManagedTenantMembership, error)
	UpdateManagedMembership(context.Context, iam.UpdateTenantMembershipInput) (*iam.TenantMembershipChangeResult, error)
	SuspendMembership(context.Context, iam.ChangeTenantMembershipInput) (*iam.TenantMembershipChangeResult, error)
	RestoreMembership(context.Context, iam.ChangeTenantMembershipInput) (*iam.TenantMembershipChangeResult, error)
	EndMembership(context.Context, iam.ChangeTenantMembershipInput) (*iam.TenantMembershipChangeResult, error)
}

type IAMTenantMembershipHandler struct {
	service iamTenantMembershipService
}

func NewIAMTenantMembershipHandler(service iamTenantMembershipService) (*IAMTenantMembershipHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: tenant membership service is required", commonapi.ErrBadRequest)
	}
	return &IAMTenantMembershipHandler{service: service}, nil
}

// List godoc
// @Summary      查询当前租户成员 | List current tenant memberships
// @Tags         租户成员 | Tenant Memberships
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number"
// @Param        page_size query int false "每页数量 | Page size"
// @Param        search query string false "姓名或用户名 | Name or username"
// @Param        status query string false "状态 | Status"
// @Success      200 {object} object{data=[]IAMTenantMembershipResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.read"]
// @Router       /tenant/memberships [get]
func (h *IAMTenantMembershipHandler) List(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if rejectTenantIDQuery(c) {
		return
	}
	status, err := parseTenantMembershipStatusFilter(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	page, pageSize := commonapi.ParsePagination(c)
	memberships, total, err := h.service.ListManagedMemberships(
		c.Request.Context(), int64(tenantID), page, pageSize, c.Query("search"), status,
	)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMTenantMembershipResponse, 0, len(memberships))
	for _, membership := range memberships {
		responses = append(responses, mapIAMTenantMembership(membership))
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

// Get godoc
// @Summary      查询当前租户成员详情 | Get current tenant membership
// @Tags         租户成员 | Tenant Memberships
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "成员关系 ID | Membership ID"
// @Success      200 {object} IAMTenantMembershipResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.read"]
// @Router       /tenant/memberships/{id} [get]
func (h *IAMTenantMembershipHandler) Get(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	membershipID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	membership, err := h.service.GetManagedMembership(c.Request.Context(), int64(tenantID), membershipID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenantMembership(*membership))
}

// Update godoc
// @Summary      更新当前租户成员 | Update current tenant membership
// @Tags         租户成员 | Tenant Memberships
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "成员关系 ID | Membership ID"
// @Param        request body IAMUpdateTenantMembershipRequest true "成员有效期 | Membership expiry"
// @Success      200 {object} IAMTenantMembershipResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.update"]
// @Router       /tenant/memberships/{id} [put]
func (h *IAMTenantMembershipHandler) Update(c *gin.Context) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	membershipID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMUpdateTenantMembershipRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid membership request", commonapi.ErrBadRequest))
		return
	}
	if _, err := h.service.UpdateManagedMembership(c.Request.Context(), iam.UpdateTenantMembershipInput{
		TenantID: int64(tenantID), MembershipID: membershipID, ExpiresAt: request.ExpiresAt,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}); err != nil {
		respondIAMError(c, err)
		return
	}
	membership, err := h.service.GetManagedMembership(c.Request.Context(), int64(tenantID), membershipID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenantMembership(*membership))
}

// Suspend godoc
// @Summary      暂停租户成员关系 | Suspend tenant membership
// @Tags         租户成员 | Tenant Memberships
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "成员关系 ID | Membership ID"
// @Param        request body IAMLifecycleReasonRequest true "原因 | Reason"
// @Success      200 {object} IAMTenantMembershipResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.suspend"]
// @Router       /tenant/memberships/{id}/suspend [post]
func (h *IAMTenantMembershipHandler) Suspend(c *gin.Context) {
	h.changeStatus(c, h.service.SuspendMembership)
}

// Restore godoc
// @Summary      恢复租户成员关系 | Restore tenant membership
// @Tags         租户成员 | Tenant Memberships
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "成员关系 ID | Membership ID"
// @Param        request body IAMLifecycleReasonRequest true "原因 | Reason"
// @Success      200 {object} IAMTenantMembershipResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.restore"]
// @Router       /tenant/memberships/{id}/restore [post]
func (h *IAMTenantMembershipHandler) Restore(c *gin.Context) {
	h.changeStatus(c, h.service.RestoreMembership)
}

// Close godoc
// @Summary      结束租户成员关系 | Close tenant membership
// @Tags         租户成员 | Tenant Memberships
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "成员关系 ID | Membership ID"
// @Param        request body IAMLifecycleReasonRequest true "原因 | Reason"
// @Success      200 {object} IAMTenantMembershipResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.tenant_membership.close"]
// @Router       /tenant/memberships/{id}/close [post]
func (h *IAMTenantMembershipHandler) Close(c *gin.Context) {
	h.changeStatus(c, h.service.EndMembership)
}

func (h *IAMTenantMembershipHandler) changeStatus(
	c *gin.Context,
	change func(context.Context, iam.ChangeTenantMembershipInput) (*iam.TenantMembershipChangeResult, error),
) {
	_, tenantID, err := iamTenantUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	membershipID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMLifecycleReasonRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil || strings.TrimSpace(request.Reason) == "" {
		respondIAMError(c, fmt.Errorf("%w: lifecycle reason is required", commonapi.ErrBadRequest))
		return
	}
	membership, err := h.service.GetManagedMembership(c.Request.Context(), int64(tenantID), membershipID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if _, err := change(c.Request.Context(), iam.ChangeTenantMembershipInput{
		TenantID: int64(tenantID), PrincipalID: membership.PrincipalID,
		Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	}); err != nil {
		respondIAMError(c, err)
		return
	}
	updated, err := h.service.GetManagedMembership(c.Request.Context(), int64(tenantID), membershipID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMTenantMembership(*updated))
}

func mapIAMTenantMembership(membership iam.ManagedTenantMembership) IAMTenantMembershipResponse {
	response := IAMTenantMembershipResponse{
		ID:            strconv.FormatInt(membership.ID, 10),
		PrincipalID:   strconv.FormatInt(membership.PrincipalID, 10),
		PrincipalType: membership.PrincipalType, PrincipalStatus: membership.PrincipalStatus,
		DisplayName: membership.DisplayName, Username: membership.Username,
		Status: membership.Status, SourceType: membership.SourceType, SourceRef: membership.SourceRef,
		JoinedAt: membership.JoinedAt.UTC(), ExpiresAt: utcTimePointer(membership.ExpiresAt),
		EndedAt: utcTimePointer(membership.EndedAt), CreatedAt: membership.CreatedAt.UTC(),
		UpdatedAt: membership.UpdatedAt.UTC(),
	}
	if membership.CreatedByPrincipalID != nil {
		value := strconv.FormatInt(*membership.CreatedByPrincipalID, 10)
		response.CreatedByPrincipalID = &value
	}
	return response
}

func parseTenantMembershipStatusFilter(value string) (*iam.TenantMembershipStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	status := iam.TenantMembershipStatus(value)
	switch status {
	case iam.TenantMembershipStatusActive, iam.TenantMembershipStatusSuspended, iam.TenantMembershipStatusEnded:
		return &status, nil
	default:
		return nil, fmt.Errorf("%w: invalid membership status", commonapi.ErrBadRequest)
	}
}

func rejectTenantIDQuery(c *gin.Context) bool {
	if _, exists := c.GetQuery("tenant_id"); !exists {
		return false
	}
	respondIAMError(c, fmt.Errorf("%w: tenant_id is derived from AuthContext", commonapi.ErrBadRequest))
	return true
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
