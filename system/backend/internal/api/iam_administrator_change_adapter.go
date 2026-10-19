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

type IAMAdministratorChangeReviewResponse struct {
	PrincipalID string    `json:"principal_id"`
	Decision    string    `json:"decision"`
	Reason      string    `json:"reason"`
	DecidedAt   time.Time `json:"decided_at"`
}
type IAMAdministratorChangeResponse struct {
	Review                 *IAMAdministratorChangeReviewResponse `json:"review"`
	Version                string                                `json:"version"`
	GovernanceRoleKey      string                                `json:"governance_role_key"`
	ReviewerRoleKey        string                                `json:"reviewer_role_key"`
	SuccessorPrincipalID   *string                               `json:"successor_principal_id"`
	ExpiresAt              time.Time                             `json:"expires_at"`
	RecoveryToken          string                                `json:"recovery_token,omitempty"`
	ID                     string                                `json:"id"`
	ChangeType             iam.PrivilegedChangeType              `json:"change_type"`
	TargetPrincipalID      string                                `json:"target_principal_id"`
	Reason                 string                                `json:"reason"`
	RequestedByPrincipalID string                                `json:"requested_by_principal_id"`
	Status                 iam.PrivilegedChangeStatus            `json:"status"`
	RequestedAt            time.Time                             `json:"requested_at"`
	DecidedAt              *time.Time                            `json:"decided_at"`
	AppliedAt              *time.Time                            `json:"applied_at"`
	CreatedAt              time.Time                             `json:"created_at"`
	UpdatedAt              time.Time                             `json:"updated_at"`
}

type IAMCreateAdministratorChangeRequest struct {
	TargetRoleKey        string                   `json:"target_role_key"`
	SuccessorPrincipalID *string                  `json:"successor_principal_id"`
	ChangeType           iam.PrivilegedChangeType `json:"change_type"`
	TargetPrincipalID    string                   `json:"target_principal_id"`
	Reason               string                   `json:"reason"`
}

type IAMReviewAdministratorChangeRequest struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

type iamAdministratorChangeService interface {
	Candidates(context.Context, int, int, string) ([]iam.AdministratorChangeCandidate, int64, error)
	List(context.Context, int, int, *iam.PrivilegedChangeStatus, *int64) ([]iam.PrivilegedChangeRequest, int64, error)
	Get(context.Context, int64) (*iam.PrivilegedChangeRequest, error)
	Create(context.Context, iam.CreateAdministratorChangeInput) (*iam.PrivilegedChangeRequest, error)
	Approve(context.Context, iam.ReviewAdministratorChangeInput) (*iam.PrivilegedChangeRequest, error)
	Cancel(context.Context, iam.ReviewAdministratorChangeInput) (*iam.PrivilegedChangeRequest, error)
	Reject(context.Context, iam.ReviewAdministratorChangeInput) (*iam.PrivilegedChangeRequest, error)
}

type IAMAdministratorChangeHandler struct {
	service iamAdministratorChangeService
}

func NewIAMAdministratorChangeHandler(
	service iamAdministratorChangeService,
) (*IAMAdministratorChangeHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: administrator change service is required", commonapi.ErrBadRequest)
	}
	return &IAMAdministratorChangeHandler{service: service}, nil
}

// List godoc
// @Summary      查询平台管理员变更请求 | List platform administrator change requests
// @Tags         平台权限治理 | Platform Authorization Governance
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number"
// @Param        page_size query int false "页大小 | Page size"
// @Param        status query string false "请求状态 | Request status" Enums(pending,approved,rejected,cancelled,applied)
// @Param        target_principal_id query string false "目标用户 ID | Target principal ID"
// @Success      200 {object} object{data=[]IAMAdministratorChangeResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_administrator_change.read"]
// @Router       /platform/administrator_changes [get]
func (h *IAMAdministratorChangeHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	page, pageSize := commonapi.ParsePagination(c)
	status, err := parsePrivilegedChangeStatus(c.Query("status"))
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	var targetPrincipalID *int64
	if value := strings.TrimSpace(c.Query("target_principal_id")); value != "" {
		parsed, err := parseIAMDecimalID(value)
		if err != nil {
			respondAdministratorChangeError(c, err)
			return
		}
		targetPrincipalID = &parsed
	}
	requests, total, err := h.service.List(c.Request.Context(), page, pageSize, status, targetPrincipalID)
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	responses := make([]IAMAdministratorChangeResponse, 0, len(requests))
	for _, request := range requests {
		responses = append(responses, mapIAMAdministratorChange(request))
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

// Get godoc
// @Summary      查询平台管理员变更请求详情 | Get platform administrator change request
// @Tags         平台权限治理 | Platform Authorization Governance
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "变更请求 ID | Change request ID"
// @Success      200 {object} IAMAdministratorChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_administrator_change.read"]
// @Router       /platform/administrator_changes/{id} [get]
func (h *IAMAdministratorChangeHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	requestID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	request, err := h.service.Get(c.Request.Context(), requestID)
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMAdministratorChange(*request))
}

// Create godoc
// @Summary      创建平台管理员变更请求 | Create platform administrator change request
// @Tags         平台权限治理 | Platform Authorization Governance
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCreateAdministratorChangeRequest true "身份变更 | Identity change"
// @Success      201 {object} IAMAdministratorChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_administrator_change.create"]
// @Router       /platform/administrator_changes [post]
func (h *IAMAdministratorChangeHandler) Create(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actorID, err := iamPlatformUserActor(c)
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	var request IAMCreateAdministratorChangeRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid administrator change request", commonapi.ErrBadRequest))
		return
	}
	targetPrincipalID, err := parseIAMDecimalID(request.TargetPrincipalID)
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	var successorID *int64
	if request.SuccessorPrincipalID != nil {
		parsed, err := parseIAMDecimalID(*request.SuccessorPrincipalID)
		if err != nil {
			respondAdministratorChangeError(c, err)
			return
		}
		successorID = &parsed
	}
	created, err := h.service.Create(c.Request.Context(), iam.CreateAdministratorChangeInput{
		ChangeType: request.ChangeType, TargetPrincipalID: targetPrincipalID, TargetRoleKey: request.TargetRoleKey, SuccessorPrincipalID: successorID,
		Reason: request.Reason, RequestedByPrincipalID: actorID,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMAdministratorChange(*created))
}

// Approve godoc
// @Summary      批准平台管理员变更 | Approve platform administrator change
// @Tags         平台权限治理 | Platform Authorization Governance
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "变更请求 ID | Change request ID"
// @Param        request body IAMReviewAdministratorChangeRequest true "版本和复核意见 | Version and review reason"
// @Success      200 {object} IAMAdministratorChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_administrator_change.approve"]
// @Router       /platform/administrator_changes/{id}/approve [post]
func (h *IAMAdministratorChangeHandler) Approve(c *gin.Context) {
	h.review(c, h.service.Approve)
}

// Reject godoc
// @Summary      拒绝平台管理员变更 | Reject platform administrator change
// @Tags         平台权限治理 | Platform Authorization Governance
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "变更请求 ID | Change request ID"
// @Param        request body IAMReviewAdministratorChangeRequest true "版本和复核意见 | Version and review reason"
// @Success      200 {object} IAMAdministratorChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_administrator_change.reject"]
// @Router       /platform/administrator_changes/{id}/reject [post]
func (h *IAMAdministratorChangeHandler) Reject(c *gin.Context) {
	h.review(c, h.service.Reject)
}

func (h *IAMAdministratorChangeHandler) review(
	c *gin.Context,
	review func(context.Context, iam.ReviewAdministratorChangeInput) (*iam.PrivilegedChangeRequest, error),
) {
	c.Header("Cache-Control", "no-store")
	actorID, err := iamPlatformUserActor(c)
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	requestID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	var request IAMReviewAdministratorChangeRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid administrator review request", commonapi.ErrBadRequest))
		return
	}
	version, err := parseIAMDecimalID(request.Version)
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	reviewed, err := review(c.Request.Context(), iam.ReviewAdministratorChangeInput{
		RequestID: requestID, Version: version, ReviewerPrincipalID: actorID, Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMAdministratorChange(*reviewed))
}

func mapIAMAdministratorChange(request iam.PrivilegedChangeRequest) IAMAdministratorChangeResponse {
	var review *IAMAdministratorChangeReviewResponse
	if request.Approval != nil {
		review = &IAMAdministratorChangeReviewResponse{PrincipalID: strconv.FormatInt(request.Approval.ReviewerPrincipalID, 10), Decision: request.Approval.Decision, Reason: request.Approval.Reason, DecidedAt: request.Approval.DecidedAt.UTC()}
	}
	return IAMAdministratorChangeResponse{
		Review:  review,
		Version: strconv.FormatInt(request.Version, 10), GovernanceRoleKey: request.GovernanceRoleKey, ReviewerRoleKey: iam.AdministratorReviewerRole(request.GovernanceRoleKey), SuccessorPrincipalID: formatOptionalIAMID(request.SuccessorPrincipalID), ExpiresAt: request.ExpiresAt.UTC(), RecoveryToken: request.RecoveryToken,
		ID: strconv.FormatInt(request.ID, 10), ChangeType: request.ChangeType,
		TargetPrincipalID: strconv.FormatInt(request.TargetPrincipalID, 10), Reason: request.Reason,
		RequestedByPrincipalID: strconv.FormatInt(request.RequestedByPrincipalID, 10), Status: request.Status,
		RequestedAt: request.RequestedAt.UTC(), DecidedAt: utcTimePointer(request.DecidedAt),
		AppliedAt: utcTimePointer(request.AppliedAt), CreatedAt: request.CreatedAt.UTC(), UpdatedAt: request.UpdatedAt.UTC(),
	}
}

func parsePrivilegedChangeStatus(value string) (*iam.PrivilegedChangeStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	status := iam.PrivilegedChangeStatus(value)
	switch status {
	case iam.PrivilegedChangeStatusPending, iam.PrivilegedChangeStatusApproved,
		iam.PrivilegedChangeStatusRejected, iam.PrivilegedChangeStatusCancelled,
		iam.PrivilegedChangeStatusApplied:
		return &status, nil
	default:
		return nil, fmt.Errorf("%w: invalid privileged change status", commonapi.ErrBadRequest)
	}
}

type IAMAdministratorChangeCandidateResponse struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	RoleKey     *string `json:"role_key"`
	MFAEnrolled bool    `json:"mfa_enrolled"`
	Status      string  `json:"status"`
}

// Candidates godoc
// @Summary 查询管理员治理候选用户 | List administrator change candidate users
// @Tags 平台权限治理 | Platform Authorization Governance
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "页大小 | Page size"
// @Param search query string false "显示名称搜索 | Display name search"
// @Success 200 {object} object{data=[]IAMAdministratorChangeCandidateResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_administrator_change.create"]
// @Router /platform/administrator_changes/candidates [get]
func (h *IAMAdministratorChangeHandler) Candidates(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	page, size := commonapi.ParsePagination(c)
	rows, total, err := h.service.Candidates(c.Request.Context(), page, size, c.Query("search"))
	if err != nil {
		respondAdministratorChangeError(c, err)
		return
	}
	result := make([]IAMAdministratorChangeCandidateResponse, 0, len(rows))
	for _, r := range rows {
		result = append(result, IAMAdministratorChangeCandidateResponse{ID: strconv.FormatInt(r.ID, 10), DisplayName: r.DisplayName, RoleKey: r.RoleKey, MFAEnrolled: r.MFAEnrolled, Status: r.Status})
	}
	commonapi.RespondPaginated(c, result, total, page, size)
}

// Cancel godoc
// @Summary 撤销本人待复核或凭据待登记的请求 | Cancel own pending request or unconsumed recovery
// @Tags 平台权限治理 | Platform Authorization Governance
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "请求 ID | Request ID"
// @Param request body IAMReviewAdministratorChangeRequest true "版本和原因 | Version and reason"
// @Success 200 {object} IAMAdministratorChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_administrator_change.create"]
// @Router /platform/administrator_changes/{id}/cancel [post]
func (h *IAMAdministratorChangeHandler) Cancel(c *gin.Context) { h.review(c, h.service.Cancel) }

func respondAdministratorChangeError(c *gin.Context, err error) {
	if errors.Is(err, commonapi.ErrConflict) {
		code := "administrator_change_conflict"
		c.JSON(http.StatusConflict, IAMErrorResponse{Error: commoni18n.T(c, sysi18n.MsgAdministratorChangeConflict), ErrorCode: &code})
		return
	}
	respondIAMError(c, err)
}
