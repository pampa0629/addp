package iam

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
)

type CreatePrivilegedIdentityChangeInput struct {
	ChangeType             PrivilegedChangeType
	TargetPrincipalID      int64
	Reason                 string
	RequestedByPrincipalID int64
	Audit                  AuditMetadata
}

type ReviewPrivilegedIdentityChangeInput struct {
	RequestID           int64
	ReviewerPrincipalID int64
	Decision            string
	Reason              string
	Audit               AuditMetadata
}

type PrivilegedIdentityChangeService struct {
	repository *Repository
	now        func() time.Time
}

func NewPrivilegedIdentityChangeService(
	repository *Repository,
	now func() time.Time,
) *PrivilegedIdentityChangeService {
	if now == nil {
		now = time.Now
	}
	return &PrivilegedIdentityChangeService{repository: repository, now: now}
}

func (s *PrivilegedIdentityChangeService) List(
	ctx context.Context,
	page int,
	pageSize int,
	status *PrivilegedChangeStatus,
	targetPrincipalID *int64,
) ([]PrivilegedChangeRequest, int64, error) {
	if err := s.validate(); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	return s.repository.ListPrivilegedIdentityChangeRequests(
		ctx, page, pageSize, status, targetPrincipalID,
	)
}

func (s *PrivilegedIdentityChangeService) Get(
	ctx context.Context,
	requestID int64,
) (*PrivilegedChangeRequest, error) {
	if err := s.validateRequestID(requestID); err != nil {
		return nil, err
	}
	request, err := s.repository.GetPrivilegedChangeRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if !isIdentityChangeType(request.ChangeType) {
		return nil, commonapi.ErrNotFound
	}
	return request, nil
}

func (s *PrivilegedIdentityChangeService) Create(
	ctx context.Context,
	input CreatePrivilegedIdentityChangeInput,
) (*PrivilegedChangeRequest, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if input.TargetPrincipalID <= 0 || input.RequestedByPrincipalID <= 0 ||
		!isSupportedIdentityChangeType(input.ChangeType) {
		return nil, fmt.Errorf("%w: invalid privileged identity change", commonapi.ErrBadRequest)
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", commonapi.ErrBadRequest)
	}
	request := &PrivilegedChangeRequest{
		ChangeType: input.ChangeType, TargetPrincipalID: input.TargetPrincipalID,
		ScopeType: "platform", Reason: reason, RequestedByPrincipalID: input.RequestedByPrincipalID,
		Status: PrivilegedChangeStatusPending,
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.CreatePrivilegedChangeRequest(ctx, request); err != nil {
			return err
		}
		request, err := tx.GetPrivilegedChangeRequest(ctx, request.ID)
		if err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.platform_identity_change.created",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system",
			EntityType: "privileged_change_request", EntityID: strconv.FormatInt(request.ID, 10),
			Details: map[string]any{
				"change_type": request.ChangeType, "target_principal_id": request.TargetPrincipalID,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return request, nil
}

func (s *PrivilegedIdentityChangeService) Approve(
	ctx context.Context,
	input ReviewPrivilegedIdentityChangeInput,
) (*PrivilegedChangeRequest, error) {
	input.Decision = "approved"
	return s.review(ctx, input)
}

func (s *PrivilegedIdentityChangeService) Reject(
	ctx context.Context,
	input ReviewPrivilegedIdentityChangeInput,
) (*PrivilegedChangeRequest, error) {
	input.Decision = "rejected"
	return s.review(ctx, input)
}

func (s *PrivilegedIdentityChangeService) review(
	ctx context.Context,
	input ReviewPrivilegedIdentityChangeInput,
) (*PrivilegedChangeRequest, error) {
	if err := s.validateRequestID(input.RequestID); err != nil {
		return nil, err
	}
	if input.ReviewerPrincipalID <= 0 || (input.Decision != "approved" && input.Decision != "rejected") {
		return nil, fmt.Errorf("%w: invalid privileged identity review", commonapi.ErrBadRequest)
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: review reason is required", commonapi.ErrBadRequest)
	}
	var reviewed *PrivilegedChangeRequest
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		request, err := tx.LockPrivilegedChangeRequest(ctx, input.RequestID)
		if err != nil {
			return err
		}
		if !isIdentityChangeType(request.ChangeType) {
			return commonapi.ErrNotFound
		}
		if request.Status != PrivilegedChangeStatusPending {
			return fmt.Errorf("%w: privileged identity change is not pending", commonapi.ErrConflict)
		}
		if err := tx.CreatePrivilegedChangeApproval(ctx, &PrivilegedChangeApproval{
			RequestID: request.ID, ReviewerPrincipalID: input.ReviewerPrincipalID,
			Decision: input.Decision, Reason: reason,
		}); err != nil {
			return err
		}
		request, err = tx.GetPrivilegedChangeRequest(ctx, request.ID)
		if err != nil {
			return err
		}
		eventName := "iam.platform_identity_change.approved"
		if input.Decision == "rejected" {
			eventName = "iam.platform_identity_change.rejected"
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: eventName,
			Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system",
			EntityType: "privileged_change_request", EntityID: strconv.FormatInt(request.ID, 10),
			Details: map[string]any{
				"change_type": request.ChangeType, "target_principal_id": request.TargetPrincipalID,
				"decision": input.Decision,
			},
		}); err != nil {
			return err
		}
		reviewed = request
		return nil
	})
	return reviewed, err
}

func (s *PrivilegedIdentityChangeService) validate() error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	return nil
}

func (s *PrivilegedIdentityChangeService) validateRequestID(requestID int64) error {
	if err := s.validate(); err != nil {
		return err
	}
	if requestID <= 0 {
		return fmt.Errorf("%w: privileged change request is required", commonapi.ErrBadRequest)
	}
	return nil
}

func isIdentityChangeType(changeType PrivilegedChangeType) bool {
	return changeType == PrivilegedChangePlatformIdentitySuspend ||
		changeType == PrivilegedChangePlatformIdentityReactivate ||
		changeType == PrivilegedChangePlatformIdentityDeactivate
}

func isSupportedIdentityChangeType(changeType PrivilegedChangeType) bool {
	return changeType == PrivilegedChangePlatformIdentitySuspend ||
		changeType == PrivilegedChangePlatformIdentityReactivate
}
