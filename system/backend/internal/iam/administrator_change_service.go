package iam

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
)

type CreateAdministratorChangeInput struct {
	ChangeType             PrivilegedChangeType
	TargetPrincipalID      int64
	TargetRoleKey          string
	SuccessorPrincipalID   *int64
	Reason                 string
	RequestedByPrincipalID int64
	Audit                  AuditMetadata
}
type ReviewAdministratorChangeInput struct {
	RequestID           int64
	Version             int64
	ReviewerPrincipalID int64
	Decision            string
	Reason              string
	Audit               AuditMetadata
}
type AdministratorChangeService struct {
	repository         *Repository
	credentialRecovery *AdministratorCredentialRecoveryService
}

func NewAdministratorChangeService(repository *Repository) *AdministratorChangeService {
	return &AdministratorChangeService{repository: repository}
}

func (s *AdministratorChangeService) List(ctx context.Context, page, pageSize int, status *PrivilegedChangeStatus, target *int64) ([]PrivilegedChangeRequest, int64, error) {
	if err := s.validate(); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	var rows []PrivilegedChangeRequest
	var total int64
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		rows, total, err = tx.ListAdministratorChangeRequests(ctx, page, pageSize, status, target)
		return err
	})
	return rows, total, err
}
func (s *AdministratorChangeService) Get(ctx context.Context, id int64) (*PrivilegedChangeRequest, error) {
	if err := s.validateRequestID(id); err != nil {
		return nil, err
	}
	var result *PrivilegedChangeRequest
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		result, err = tx.GetPrivilegedChangeRequest(ctx, id)
		return err
	})
	return result, err
}
func (s *AdministratorChangeService) Create(ctx context.Context, in CreateAdministratorChangeInput) (*PrivilegedChangeRequest, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if in.TargetPrincipalID <= 0 || in.RequestedByPrincipalID <= 0 || strings.TrimSpace(in.Reason) == "" || !isSupportedAdministratorChangeType(in.ChangeType) {
		return nil, commonapi.ErrBadRequest
	}
	roleAction := isAdministratorRoleChange(in.ChangeType)
	if (roleAction && !isBootstrapRole(in.TargetRoleKey) || !roleAction && in.TargetRoleKey != "") || (in.ChangeType == PrivilegedChangePlatformRoleReplace) != (in.SuccessorPrincipalID != nil) {
		return nil, commonapi.ErrBadRequest
	}
	req := &PrivilegedChangeRequest{ChangeType: in.ChangeType, TargetPrincipalID: in.TargetPrincipalID, SuccessorPrincipalID: in.SuccessorPrincipalID, Reason: strings.TrimSpace(in.Reason), RequestedByPrincipalID: in.RequestedByPrincipalID, ScopeType: "platform", Status: PrivilegedChangeStatusPending}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		ids := []int64{in.TargetPrincipalID, in.RequestedByPrincipalID}
		if in.SuccessorPrincipalID != nil {
			ids = append(ids, *in.SuccessorPrincipalID)
		}
		if _, err := tx.lockPrincipalsInOrder(ctx, ids...); err != nil {
			return err
		}
		if roleAction {
			roleID, err := tx.administratorRoleID(ctx, in.TargetRoleKey)
			if err != nil {
				return err
			}
			req.TargetRoleID = &roleID
		}
		retired, err := tx.invalidAdministratorChanges(ctx, in.TargetPrincipalID)
		if err != nil {
			return err
		}
		for i := range retired {
			if err := tx.cancelAdministratorChange(ctx, &retired[i]); err != nil {
				return err
			}
			if err := writeAdministratorChangeAudit(ctx, tx, in.Audit, "cancelled", &retired[i]); err != nil {
				return err
			}
		}
		if err := tx.CreatePrivilegedChangeRequest(ctx, req); err != nil {
			return err
		}
		req, err = tx.GetPrivilegedChangeRequest(ctx, req.ID)
		if err != nil {
			return err
		}
		return writeAdministratorChangeAudit(ctx, tx, in.Audit, "created", req)
	})
	return req, err
}
func (s *AdministratorChangeService) Approve(ctx context.Context, in ReviewAdministratorChangeInput) (*PrivilegedChangeRequest, error) {
	in.Decision = "approved"
	return s.review(ctx, in)
}
func (s *AdministratorChangeService) Reject(ctx context.Context, in ReviewAdministratorChangeInput) (*PrivilegedChangeRequest, error) {
	in.Decision = "rejected"
	return s.review(ctx, in)
}
func (s *AdministratorChangeService) review(ctx context.Context, in ReviewAdministratorChangeInput) (*PrivilegedChangeRequest, error) {
	if err := s.validateRequestID(in.RequestID); err != nil {
		return nil, err
	}
	if in.Version <= 0 || in.ReviewerPrincipalID <= 0 || strings.TrimSpace(in.Reason) == "" {
		return nil, commonapi.ErrBadRequest
	}
	source, err := s.repository.GetPrivilegedChangeRequest(ctx, in.RequestID)
	if err != nil {
		return nil, err
	}
	var result *PrivilegedChangeRequest
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		ids := []int64{source.TargetPrincipalID, source.RequestedByPrincipalID, in.ReviewerPrincipalID}
		if source.SuccessorPrincipalID != nil {
			ids = append(ids, *source.SuccessorPrincipalID)
		}
		var roleHolders []int64
		if source.TargetRoleID != nil {
			var err error
			roleHolders, err = tx.ListActiveRoleHolderPrincipalIDs(ctx, *source.TargetRoleID)
			if err != nil {
				return err
			}
			ids = append(ids, roleHolders...)
		}
		if _, err := tx.lockPrincipalsInOrder(ctx, ids...); err != nil {
			return err
		}
		if source.TargetRoleID != nil {
			if err := tx.lockAdministratorRole(ctx, *source.TargetRoleID); err != nil {
				return err
			}
			current, err := tx.ListActiveRoleHolderPrincipalIDs(ctx, *source.TargetRoleID)
			if err := verifyMutationPrincipalSet(roleHolders, current, err); err != nil {
				return err
			}
		}
		req, err := tx.LockPrivilegedChangeRequest(ctx, in.RequestID)
		if err != nil {
			return err
		}
		if req.Version != in.Version || req.Status != PrivilegedChangeStatusPending {
			return commonapi.ErrConflict
		}
		if err := tx.CreatePrivilegedChangeApproval(ctx, &PrivilegedChangeApproval{RequestID: req.ID, ReviewerPrincipalID: in.ReviewerPrincipalID, Decision: in.Decision, Reason: strings.TrimSpace(in.Reason)}); err != nil {
			return err
		}
		if in.Decision == "approved" {
			if err := s.apply(ctx, tx, req); err != nil {
				return err
			}
		}
		result, err = tx.GetPrivilegedChangeRequest(ctx, req.ID)
		if err != nil {
			return err
		}
		result.RecoveryToken = req.RecoveryToken
		return writeAdministratorChangeAudit(ctx, tx, in.Audit, in.Decision, result)
	})
	return result, err
}
func (s *AdministratorChangeService) apply(ctx context.Context, tx *Repository, req *PrivilegedChangeRequest) error {
	now, err := tx.CurrentDatabaseTime(ctx)
	if err != nil {
		return err
	}
	switch req.ChangeType {
	case PrivilegedChangePlatformIdentitySuspend, PrivilegedChangePlatformIdentityReactivate:
		status := PrincipalStatusSuspended
		if req.ChangeType == PrivilegedChangePlatformIdentityReactivate {
			status = PrincipalStatusActive
		}
		if _, err := tx.UpdatePrincipalStatus(ctx, req.TargetPrincipalID, status, nil, &req.ID); err != nil {
			return err
		}
	case PrivilegedChangePlatformRoleGrant:
		if err := tx.grantAdministratorRole(ctx, req, req.TargetPrincipalID, now); err != nil {
			return err
		}
	case PrivilegedChangePlatformRoleReplace:
		if req.SuccessorPrincipalID == nil {
			return commonapi.ErrBadRequest
		}
		if err := tx.grantAdministratorRole(ctx, req, *req.SuccessorPrincipalID, now); err != nil {
			return err
		}
		if err := tx.revokeAdministratorRole(ctx, req, now); err != nil {
			return err
		}
	case PrivilegedChangePlatformRoleRevoke:
		available, err := tx.availableAdministratorPeerCount(ctx, *req.TargetRoleID, req.TargetPrincipalID)
		if err != nil {
			return err
		}
		if available == 0 {
			return fmt.Errorf("%w: administrator role requires a replacement", commonapi.ErrConflict)
		}
		if err := tx.revokeAdministratorRole(ctx, req, now); err != nil {
			return err
		}
	case PrivilegedChangePlatformCredentialRecovery:
		if s.credentialRecovery == nil {
			return fmt.Errorf("%w: credential recovery is unavailable", commonapi.ErrConflict)
		}
		return s.credentialRecovery.issue(ctx, tx, req, now)
	default:
		return commonapi.ErrBadRequest
	}
	ids := []int64{req.TargetPrincipalID}
	if req.SuccessorPrincipalID != nil {
		ids = append(ids, *req.SuccessorPrincipalID)
	}
	for _, id := range ids {
		if _, err := tx.RevokeActiveTokenFamilies(ctx, id, now, "platform_administrator_changed"); err != nil {
			return err
		}
		if _, err := tx.ConsumeActiveContextSelectionTickets(ctx, id, now); err != nil {
			return err
		}
		if _, err := tx.ConsumePendingMFAChallenges(ctx, id, now); err != nil {
			return err
		}
		if _, err := tx.ConsumePendingMFAEnrollments(ctx, id, now); err != nil {
			return err
		}
	}
	return nil
}
func writeAdministratorChangeAudit(ctx context.Context, tx *Repository, audit AuditMetadata, action string, req *PrivilegedChangeRequest) error {
	return NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.platform_administrator_change." + action, Result: administratorChangeAuditResult(action), RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "privileged_change_request", EntityID: strconv.FormatInt(req.ID, 10), Details: map[string]any{"change_type": req.ChangeType, "target_principal_id": req.TargetPrincipalID, "role_key": req.GovernanceRoleKey, "status": req.Status}})
}
func (s *AdministratorChangeService) validate() error {
	if s == nil || s.repository == nil {
		return commonapi.ErrBadRequest
	}
	return nil
}
func (s *AdministratorChangeService) validateRequestID(id int64) error {
	if err := s.validate(); err != nil {
		return err
	}
	if id <= 0 {
		return commonapi.ErrBadRequest
	}
	return nil
}
func isAdministratorRoleChange(t PrivilegedChangeType) bool {
	return t == PrivilegedChangePlatformRoleGrant || t == PrivilegedChangePlatformRoleRevoke || t == PrivilegedChangePlatformRoleReplace
}
func isSupportedAdministratorChangeType(t PrivilegedChangeType) bool {
	return isAdministratorRoleChange(t) || t == PrivilegedChangePlatformIdentitySuspend || t == PrivilegedChangePlatformIdentityReactivate || t == PrivilegedChangePlatformCredentialRecovery
}

func (s *AdministratorChangeService) WithCredentialRecovery(recovery *AdministratorCredentialRecoveryService) *AdministratorChangeService {
	s.credentialRecovery = recovery
	return s
}

func AdministratorReviewerRole(role string) string {
	switch role {
	case "platform.system_administrator", "platform.security_administrator":
		return "platform.audit_administrator"
	case "platform.audit_administrator":
		return "platform.system_administrator"
	}
	return ""
}

type AdministratorChangeCandidate struct {
	ID          int64
	DisplayName string
	RoleKey     *string
	MFAEnrolled bool
	Status      string
}

func (s *AdministratorChangeService) Candidates(ctx context.Context, page, size int, search string) ([]AdministratorChangeCandidate, int64, error) {
	if err := s.validate(); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, size); err != nil {
		return nil, 0, err
	}
	if len(search) > 255 {
		return nil, 0, commonapi.ErrBadRequest
	}
	rows := []AdministratorChangeCandidate{}
	var total int64
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		rows, total, err = tx.listAdministratorChangeCandidates(ctx, page, size, search)
		return err
	})
	return rows, total, err
}

func (s *AdministratorChangeService) Cancel(ctx context.Context, in ReviewAdministratorChangeInput) (*PrivilegedChangeRequest, error) {
	if err := s.validateRequestID(in.RequestID); err != nil {
		return nil, err
	}
	if in.Version <= 0 || in.ReviewerPrincipalID <= 0 || strings.TrimSpace(in.Reason) == "" {
		return nil, commonapi.ErrBadRequest
	}
	var result *PrivilegedChangeRequest
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.lockPrincipalsInOrder(ctx, in.ReviewerPrincipalID); err != nil {
			return err
		}
		req, err := tx.LockPrivilegedChangeRequest(ctx, in.RequestID)
		if err != nil {
			return err
		}
		if req.RequestedByPrincipalID != in.ReviewerPrincipalID {
			return commonapi.ErrForbidden
		}
		if (req.Status != PrivilegedChangeStatusPending && !(req.ChangeType == PrivilegedChangePlatformCredentialRecovery && req.Status == PrivilegedChangeStatusApproved)) || req.Version != in.Version {
			return commonapi.ErrConflict
		}
		if err := tx.cancelAdministratorChange(ctx, req); err != nil {
			return err
		}
		result, err = tx.GetPrivilegedChangeRequest(ctx, req.ID)
		if err != nil {
			return err
		}
		return writeAdministratorChangeAudit(ctx, tx, in.Audit, "cancelled", result)
	})
	return result, err
}

func administratorChangeAuditResult(action string) AuditResult {
	if action == "credential_enrollment_denied" {
		return AuditResultDenied
	}
	return AuditResultSucceeded
}
