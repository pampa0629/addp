package iam

import (
	"context"
	"encoding/json"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
)

type RecoveryResolutionState string

const (
	RecoveryResolutionUnknown        RecoveryResolutionState = "unknown"
	RecoveryResolutionPrepared       RecoveryResolutionState = "prepared"
	RecoveryResolutionExpired        RecoveryResolutionState = "expired"
	RecoveryResolutionCompletedThis  RecoveryResolutionState = "completed_this_submission"
	RecoveryResolutionCompletedOther RecoveryResolutionState = "completed_other_submission"
)

// This is a historical receipt, never a current authentication decision.
type RecoveryResolution struct {
	State                 RecoveryResolutionState
	AttemptID             int64
	SubmissionID          uuid.UUID
	CompletedAt           *time.Time
	ExpiresAt             time.Time
	Principals            map[int64]string
	AuthorizationVersions map[int64]int64
}

func (s *RecoveryService) Resolve(ctx context.Context, attemptID int64, submissionID uuid.UUID) (*RecoveryResolution, error) {
	unknown := &RecoveryResolution{State: RecoveryResolutionUnknown, AttemptID: attemptID}
	if s == nil || s.repository == nil || attemptID <= 0 || submissionID == uuid.Nil {
		return unknown, commonapi.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resolution *RecoveryResolution
	err := s.repository.resolveRecoveryTransaction(ctx, func(tx *Repository) error {
		attempt, events, err := tx.readRecoveryReceipt(ctx, attemptID)
		if err != nil {
			return err
		}
		now, err := tx.recoveryDatabaseTime(ctx)
		if err != nil {
			return err
		}
		resolution, err = resolveRecoveryReceipt(attempt, events, submissionID, now)
		return err
	})
	if err != nil {
		return unknown, err
	}
	return resolution, nil
}

func resolveRecoveryReceipt(attempt *IAMRecoveryAttempt, events []AuditLog, submissionID uuid.UUID, now time.Time) (*RecoveryResolution, error) {
	result := &RecoveryResolution{AttemptID: attempt.ID, ExpiresAt: attempt.ExpiresAt,
		Principals: map[int64]string{}, AuthorizationVersions: map[int64]int64{}}
	prepared, completed, count := 0, 0, 0
	roles := map[string]bool{}
	for _, event := range events {
		if event.Result != AuditResultSucceeded {
			return nil, commonapi.ErrConflict
		}
		switch event.EventName {
		case "iam.recovery.prepared":
			prepared++
		case "iam.recovery.completed":
			completed++
			var detail struct {
				SubmissionID string `json:"submission_id"`
				Count        int    `json:"administrator_count"`
			}
			if json.Unmarshal(event.Details, &detail) != nil {
				return nil, commonapi.ErrConflict
			}
			id, err := uuid.Parse(detail.SubmissionID)
			if err != nil || id == uuid.Nil || id.String() != detail.SubmissionID {
				return nil, commonapi.ErrConflict
			}
			result.SubmissionID, count = id, detail.Count
		case "iam.recovery.administrator_credentials_replaced":
			var detail struct {
				RoleKey string `json:"role_key"`
				Version int64  `json:"authorization_version"`
			}
			if json.Unmarshal(event.Details, &detail) != nil || event.PrincipalID == nil || *event.PrincipalID <= 0 ||
				!isBootstrapRole(detail.RoleKey) || detail.Version <= 0 || result.Principals[*event.PrincipalID] != "" {
				return nil, commonapi.ErrConflict
			}
			result.Principals[*event.PrincipalID], result.AuthorizationVersions[*event.PrincipalID] = detail.RoleKey, detail.Version
			roles[detail.RoleKey] = true
		}
	}
	if prepared != 1 {
		return nil, commonapi.ErrConflict
	}
	if attempt.Status == IAMRecoveryStatusCompleted {
		if completed != 1 || count != len(result.Principals) || len(roles) != len(bootstrapRoleOrder) || attempt.SecretHash != nil || attempt.CompletedAt == nil {
			return nil, commonapi.ErrConflict
		}
		result.CompletedAt = attempt.CompletedAt
		result.State = RecoveryResolutionCompletedOther
		if result.SubmissionID == submissionID {
			result.State = RecoveryResolutionCompletedThis
		}
		return result, nil
	}
	if completed != 0 || len(result.Principals) != 0 {
		return nil, commonapi.ErrConflict
	}
	switch attempt.Status {
	case IAMRecoveryStatusPrepared:
		if attempt.SecretHash == nil || attempt.CompletedAt != nil || attempt.ExpiredAt != nil {
			return nil, commonapi.ErrConflict
		}
		result.State = RecoveryResolutionPrepared
		if !attempt.ExpiresAt.After(now) {
			result.State = RecoveryResolutionExpired
		}
	case IAMRecoveryStatusExpired:
		if attempt.SecretHash != nil || attempt.ExpiredAt == nil || attempt.CompletedAt != nil {
			return nil, commonapi.ErrConflict
		}
		result.State = RecoveryResolutionExpired
	default:
		return nil, commonapi.ErrConflict
	}
	return result, nil
}

func sameRecoveryTargets(expected, current []RecoveryAdministratorTarget) bool {
	if len(expected) != len(current) || len(expected) < len(bootstrapRoleOrder) {
		return false
	}
	byID := map[int64]RecoveryAdministratorTarget{}
	for _, target := range expected {
		if target.PrincipalID <= 0 || byID[target.PrincipalID].PrincipalID != 0 {
			return false
		}
		byID[target.PrincipalID] = target
	}
	for _, target := range current {
		old, ok := byID[target.PrincipalID]
		if !ok || old.RoleKey != target.RoleKey || old.RoleAssignmentID != target.RoleAssignmentID ||
			old.AccountID != target.AccountID || old.PrincipalStatus != target.PrincipalStatus || old.AccountStatus != target.AccountStatus ||
			old.AuthorizationVersion != target.AuthorizationVersion {
			return false
		}
		delete(byID, target.PrincipalID)
	}
	return len(byID) == 0
}
