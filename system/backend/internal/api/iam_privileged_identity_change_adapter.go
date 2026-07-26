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

type IAMPrivilegedIdentityChangeResponse struct {
	ID                     string                     `json:"id"`
	ChangeType             iam.PrivilegedChangeType   `json:"change_type"`
	TargetPrincipalID      string                     `json:"target_principal_id"`
	Reason                 string                     `json:"reason"`
	RequestedByPrincipalID string                     `json:"requested_by_principal_id"`
	Status                 iam.PrivilegedChangeStatus `json:"status"`
	RequestedAt            time.Time                  `json:"requested_at"`
	DecidedAt              *time.Time                 `json:"decided_at"`
	AppliedAt              *time.Time                 `json:"applied_at"`
	CreatedAt              time.Time                  `json:"created_at"`
	UpdatedAt              time.Time                  `json:"updated_at"`
}

type IAMCreatePrivilegedIdentityChangeRequest struct {
	ChangeType        iam.PrivilegedChangeType `json:"change_type"`
	TargetPrincipalID string                   `json:"target_principal_id"`
	Reason            string                   `json:"reason"`
}

type IAMReviewPrivilegedIdentityChangeRequest struct {
	Reason string `json:"reason"`
}

type iamPrivilegedIdentityChangeService interface {
	List(context.Context, int, int, *iam.PrivilegedChangeStatus, *int64) ([]iam.PrivilegedChangeRequest, int64, error)
	Get(context.Context, int64) (*iam.PrivilegedChangeRequest, error)
	Create(context.Context, iam.CreatePrivilegedIdentityChangeInput) (*iam.PrivilegedChangeRequest, error)
	Approve(context.Context, iam.ReviewPrivilegedIdentityChangeInput) (*iam.PrivilegedChangeRequest, error)
	Reject(context.Context, iam.ReviewPrivilegedIdentityChangeInput) (*iam.PrivilegedChangeRequest, error)
}

type IAMPrivilegedIdentityChangeHandler struct {
	service iamPrivilegedIdentityChangeService
}

func NewIAMPrivilegedIdentityChangeHandler(
	service iamPrivilegedIdentityChangeService,
) (*IAMPrivilegedIdentityChangeHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: privileged identity change service is required", commonapi.ErrBadRequest)
	}
	return &IAMPrivilegedIdentityChangeHandler{service: service}, nil
}

// List godoc
// @Summary      查询平台身份变更请求 | List platform identity change requests
// @Tags         平台权限治理 | Platform Authorization Governance
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} object{data=[]IAMPrivilegedIdentityChangeResponse,total=int64,page=int,page_size=int,total_pages=int}
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_identity_change.read"]
// @Router       /platform/identity_changes [get]
func (h *IAMPrivilegedIdentityChangeHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	status, err := parsePrivilegedChangeStatus(c.Query("status"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var targetPrincipalID *int64
	if value := strings.TrimSpace(c.Query("target_principal_id")); value != "" {
		parsed, err := parseIAMDecimalID(value)
		if err != nil {
			respondIAMError(c, err)
			return
		}
		targetPrincipalID = &parsed
	}
	requests, total, err := h.service.List(c.Request.Context(), page, pageSize, status, targetPrincipalID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responses := make([]IAMPrivilegedIdentityChangeResponse, 0, len(requests))
	for _, request := range requests {
		responses = append(responses, mapIAMPrivilegedIdentityChange(request))
	}
	commonapi.RespondPaginated(c, responses, total, page, pageSize)
}

// Get godoc
// @Summary      查询平台身份变更请求详情 | Get platform identity change request
// @Tags         平台权限治理 | Platform Authorization Governance
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "变更请求 ID | Change request ID"
// @Success      200 {object} IAMPrivilegedIdentityChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_identity_change.read"]
// @Router       /platform/identity_changes/{id} [get]
func (h *IAMPrivilegedIdentityChangeHandler) Get(c *gin.Context) {
	requestID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	request, err := h.service.Get(c.Request.Context(), requestID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMPrivilegedIdentityChange(*request))
}

// Create godoc
// @Summary      创建平台身份变更请求 | Create platform identity change request
// @Tags         平台权限治理 | Platform Authorization Governance
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMCreatePrivilegedIdentityChangeRequest true "身份变更 | Identity change"
// @Success      201 {object} IAMPrivilegedIdentityChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_identity_change.create"]
// @Router       /platform/identity_changes [post]
func (h *IAMPrivilegedIdentityChangeHandler) Create(c *gin.Context) {
	actorID, err := iamPlatformUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMCreatePrivilegedIdentityChangeRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid identity change request", commonapi.ErrBadRequest))
		return
	}
	targetPrincipalID, err := parseIAMDecimalID(request.TargetPrincipalID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	created, err := h.service.Create(c.Request.Context(), iam.CreatePrivilegedIdentityChangeInput{
		ChangeType: request.ChangeType, TargetPrincipalID: targetPrincipalID,
		Reason: request.Reason, RequestedByPrincipalID: actorID,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapIAMPrivilegedIdentityChange(*created))
}

// Approve godoc
// @Summary      批准平台身份变更 | Approve platform identity change
// @Tags         平台权限治理 | Platform Authorization Governance
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "变更请求 ID | Change request ID"
// @Param        request body IAMReviewPrivilegedIdentityChangeRequest false "复核意见 | Review reason"
// @Success      200 {object} IAMPrivilegedIdentityChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_identity_change.approve"]
// @Router       /platform/identity_changes/{id}/approve [post]
func (h *IAMPrivilegedIdentityChangeHandler) Approve(c *gin.Context) {
	h.review(c, h.service.Approve)
}

// Reject godoc
// @Summary      拒绝平台身份变更 | Reject platform identity change
// @Tags         平台权限治理 | Platform Authorization Governance
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "变更请求 ID | Change request ID"
// @Param        request body IAMReviewPrivilegedIdentityChangeRequest false "复核意见 | Review reason"
// @Success      200 {object} IAMPrivilegedIdentityChangeResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.platform_identity_change.reject"]
// @Router       /platform/identity_changes/{id}/reject [post]
func (h *IAMPrivilegedIdentityChangeHandler) Reject(c *gin.Context) {
	h.review(c, h.service.Reject)
}

func (h *IAMPrivilegedIdentityChangeHandler) review(
	c *gin.Context,
	review func(context.Context, iam.ReviewPrivilegedIdentityChangeInput) (*iam.PrivilegedChangeRequest, error),
) {
	actorID, err := iamPlatformUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	requestID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMReviewPrivilegedIdentityChangeRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid identity review request", commonapi.ErrBadRequest))
		return
	}
	reviewed, err := review(c.Request.Context(), iam.ReviewPrivilegedIdentityChangeInput{
		RequestID: requestID, ReviewerPrincipalID: actorID, Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMPrivilegedIdentityChange(*reviewed))
}

func mapIAMPrivilegedIdentityChange(request iam.PrivilegedChangeRequest) IAMPrivilegedIdentityChangeResponse {
	return IAMPrivilegedIdentityChangeResponse{
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
