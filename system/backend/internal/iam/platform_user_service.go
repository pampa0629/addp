package iam

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	passwordutils "github.com/addp/system/pkg/utils"
)

var ErrPrivilegedChangeRequestRequired = errors.New("privileged change request required")

type CreateManagedLocalUserInput struct {
	Username     string
	Password     string
	DisplayName  string
	PrimaryEmail *string
	Locale       *string
	Audit        AuditMetadata
}

type UpdateManagedUserInput struct {
	UserID       int64
	DisplayName  string
	PrimaryEmail *string
	Locale       *string
	Audit        AuditMetadata
}

type ChangeManagedUserStatusInput struct {
	UserID          int64
	Reason          string
	ChangeRequestID *int64
	Audit           AuditMetadata
}

type ManagedUserStatusChangeResult struct {
	User               ManagedUser
	RevokedFamilyCount int64
}

type ResetManagedLocalAccountPasswordInput struct {
	UserID      int64
	NewPassword string
	Reason      string
	Audit       AuditMetadata
}

type ManagedLocalAccountPasswordResetResult struct {
	AuthorizationVersion       int64
	RevokedFamilyCount         int64
	ConsumedMFAChallengeCount  int64
	ConsumedContextTicketCount int64
	ChangedAt                  time.Time
}

type PlatformUserService struct {
	repository      *Repository
	identityService *IdentityService
	now             func() time.Time
}

func NewPlatformUserService(
	repository *Repository,
	identityService *IdentityService,
	now func() time.Time,
) *PlatformUserService {
	if now == nil {
		now = time.Now
	}
	return &PlatformUserService{repository: repository, identityService: identityService, now: now}
}

func (s *PlatformUserService) List(
	ctx context.Context,
	page int,
	pageSize int,
	search string,
	status *PrincipalStatus,
) ([]ManagedUser, int64, error) {
	if err := s.validate(); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	var users []ManagedUser
	var total int64
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		users, total, err = tx.ListManagedUsers(ctx, page, pageSize, search, status)
		return err
	})
	return users, total, err
}

func (s *PlatformUserService) Get(ctx context.Context, userID int64) (*ManagedUser, error) {
	if err := s.validateUserID(userID); err != nil {
		return nil, err
	}
	return s.repository.GetManagedUser(ctx, userID)
}

func (s *PlatformUserService) Create(
	ctx context.Context,
	input CreateManagedLocalUserInput,
) (*ManagedUser, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	created, err := s.identityService.CreateLocalUser(ctx, CreateLocalUserInput{
		Username:     input.Username,
		Password:     input.Password,
		DisplayName:  input.DisplayName,
		PrimaryEmail: normalizeOptionalText(input.PrimaryEmail),
		Locale:       normalizeOptionalText(input.Locale),
		Audit:        input.Audit,
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetManagedUser(ctx, created.PrincipalID)
}

func (s *PlatformUserService) Update(
	ctx context.Context,
	input UpdateManagedUserInput,
) (*ManagedUser, error) {
	if err := s.validateUserID(input.UserID); err != nil {
		return nil, err
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return nil, fmt.Errorf("%w: display name is required", commonapi.ErrBadRequest)
	}
	primaryEmail := normalizeOptionalText(input.PrimaryEmail)
	locale := normalizeOptionalText(input.Locale)
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, input.UserID)
		if err != nil {
			return err
		}
		if principal.PrincipalType != PrincipalTypeUser || principal.Status == PrincipalStatusDeactivated {
			return fmt.Errorf("%w: user cannot be updated", commonapi.ErrConflict)
		}
		if _, err := tx.GetUser(ctx, input.UserID); err != nil {
			return err
		}
		if err := tx.UpdateUserProfile(ctx, input.UserID, displayName, primaryEmail, locale); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   input.Audit,
			EventName:  "iam.user.updated",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "principal",
			EntityID:   strconv.FormatInt(input.UserID, 10),
			Details:    map[string]any{},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetManagedUser(ctx, input.UserID)
}

func (s *PlatformUserService) ResetLocalAccountPassword(
	ctx context.Context,
	input ResetManagedLocalAccountPasswordInput,
) (*ManagedLocalAccountPasswordResetResult, error) {
	if err := s.validateUserID(input.UserID); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(input.Reason)
	if input.NewPassword == "" || reason == "" {
		return nil, fmt.Errorf("%w: new password and reason are required", commonapi.ErrBadRequest)
	}
	passwordHash, err := passwordutils.HashPassword(input.NewPassword)
	if err != nil {
		return nil, fmt.Errorf("%w: hash password: %v", commonapi.ErrBadRequest, err)
	}

	changedAt := s.now().UTC()
	var reset *ManagedLocalAccountPasswordResetResult
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, input.UserID)
		if err != nil {
			return err
		}
		if principal.PrincipalType != PrincipalTypeUser {
			return commonapi.ErrNotFound
		}
		if principal.Status != PrincipalStatusActive {
			return fmt.Errorf("%w: user is not active", commonapi.ErrConflict)
		}
		governed, err := tx.HasEffectivePlatformRole(ctx, principal.ID, changedAt)
		if err != nil {
			return err
		}
		if governed {
			return fmt.Errorf("%w: platform role holder credentials cannot be reset", commonapi.ErrConflict)
		}
		account, err := tx.LockLocalAccountByUserID(ctx, principal.ID)
		if err != nil {
			return err
		}
		if account.Status != LocalAccountStatusActive && account.Status != LocalAccountStatusLocked {
			return fmt.Errorf("%w: local account cannot be reset", commonapi.ErrConflict)
		}
		if passwordutils.CheckPassword(input.NewPassword, account.PasswordHash) {
			return ErrPasswordUnchanged
		}
		if err := tx.ResetLocalAccountPassword(ctx, account.ID, passwordHash, changedAt); err != nil {
			return err
		}
		authorizationVersion, err := tx.IncrementPrincipalAuthorizationVersion(ctx, principal.ID)
		if err != nil {
			return err
		}
		revokedFamilyCount, err := tx.RevokeActiveTokenFamilies(
			ctx, principal.ID, changedAt, "local_account_password_reset",
		)
		if err != nil {
			return err
		}
		consumedMFAChallengeCount, err := tx.ConsumePendingMFAChallenges(ctx, principal.ID, changedAt)
		if err != nil {
			return err
		}
		consumedContextTicketCount, err := tx.ConsumeActiveContextSelectionTickets(ctx, principal.ID, changedAt)
		if err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.local_account.password_reset",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system",
			EntityType: "local_account", EntityID: strconv.FormatInt(account.ID, 10),
			Details: map[string]any{
				"target_principal_id":           principal.ID,
				"reason":                        reason,
				"authorization_version":         authorizationVersion,
				"revoked_family_count":          revokedFamilyCount,
				"consumed_mfa_challenge_count":  consumedMFAChallengeCount,
				"consumed_context_ticket_count": consumedContextTicketCount,
			},
		}); err != nil {
			return err
		}
		reset = &ManagedLocalAccountPasswordResetResult{
			AuthorizationVersion: authorizationVersion, RevokedFamilyCount: revokedFamilyCount,
			ConsumedMFAChallengeCount:  consumedMFAChallengeCount,
			ConsumedContextTicketCount: consumedContextTicketCount, ChangedAt: changedAt,
		}
		return nil
	})
	return reset, err
}

func (s *PlatformUserService) Suspend(
	ctx context.Context,
	input ChangeManagedUserStatusInput,
) (*ManagedUserStatusChangeResult, error) {
	return s.changeStatus(ctx, input, PrincipalStatusSuspended)
}

func (s *PlatformUserService) Reactivate(
	ctx context.Context,
	input ChangeManagedUserStatusInput,
) (*ManagedUserStatusChangeResult, error) {
	return s.changeStatus(ctx, input, PrincipalStatusActive)
}

func (s *PlatformUserService) changeStatus(
	ctx context.Context,
	input ChangeManagedUserStatusInput,
	target PrincipalStatus,
) (*ManagedUserStatusChangeResult, error) {
	if err := s.validateUserID(input.UserID); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", commonapi.ErrBadRequest)
	}
	now := s.now().UTC()
	var changed *ManagedUserStatusChangeResult
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, input.UserID)
		if err != nil {
			return err
		}
		if principal.PrincipalType != PrincipalTypeUser {
			return commonapi.ErrNotFound
		}
		if err := validatePrincipalStatusTransition(principal.Status, target); err != nil {
			return err
		}
		governed, err := tx.HasEffectivePlatformRole(ctx, principal.ID, now)
		if err != nil {
			return err
		}
		if governed && input.ChangeRequestID == nil {
			return fmt.Errorf("%w: %w", commonapi.ErrConflict, ErrPrivilegedChangeRequestRequired)
		}
		if !governed && input.ChangeRequestID != nil {
			return fmt.Errorf("%w: change request is only valid for a platform role holder", commonapi.ErrBadRequest)
		}
		authorizationVersion, err := tx.UpdatePrincipalStatus(
			ctx, principal.ID, target, nil, input.ChangeRequestID,
		)
		if err != nil {
			return err
		}
		revoked, err := tx.RevokeActiveTokenFamilies(ctx, principal.ID, now, "principal_"+string(target))
		if err != nil {
			return err
		}
		eventName := "iam.user.suspended"
		if target == PrincipalStatusActive {
			eventName = "iam.user.reactivated"
		}
		details := map[string]any{
			"reason":                reason,
			"status":                target,
			"authorization_version": authorizationVersion,
			"revoked_family_count":  revoked,
		}
		if input.ChangeRequestID != nil {
			details["change_request_id"] = *input.ChangeRequestID
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   input.Audit,
			EventName:  eventName,
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskHigh,
			ModuleName: "system",
			EntityType: "principal",
			EntityID:   strconv.FormatInt(principal.ID, 10),
			Details:    details,
		}); err != nil {
			return err
		}
		managed, err := tx.GetManagedUser(ctx, principal.ID)
		if err != nil {
			return err
		}
		changed = &ManagedUserStatusChangeResult{User: *managed, RevokedFamilyCount: revoked}
		return nil
	})
	return changed, err
}

func (s *PlatformUserService) validate() error {
	if s == nil || s.repository == nil || s.identityService == nil {
		return fmt.Errorf("%w: IAM user management dependencies are required", commonapi.ErrBadRequest)
	}
	return nil
}

func (s *PlatformUserService) validateUserID(userID int64) error {
	if err := s.validate(); err != nil {
		return err
	}
	if userID <= 0 {
		return fmt.Errorf("%w: user is required", commonapi.ErrBadRequest)
	}
	return nil
}

func validatePrincipalStatusTransition(current PrincipalStatus, target PrincipalStatus) error {
	valid := (current == PrincipalStatusActive && target == PrincipalStatusSuspended) ||
		(current == PrincipalStatusSuspended && target == PrincipalStatusActive)
	if !valid {
		return fmt.Errorf("%w: invalid user status transition from %s to %s", commonapi.ErrConflict, current, target)
	}
	return nil
}

func normalizeOptionalText(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}
