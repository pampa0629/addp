package iam

import (
	"context"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type RecoveryAdministratorTarget struct {
	RoleKey          string             `gorm:"column:role_key"`
	RoleAssignmentID int64              `gorm:"column:role_assignment_id"`
	PrincipalID      int64              `gorm:"column:principal_id"`
	PrincipalStatus  PrincipalStatus    `gorm:"column:principal_status"`
	AccountID        int64              `gorm:"column:account_id"`
	Username         string             `gorm:"column:username"`
	AccountStatus    LocalAccountStatus `gorm:"column:account_status"`
	DisplayName      string             `gorm:"column:display_name"`
}

func (r *Repository) LockIAMRecoveryTable(ctx context.Context) error {
	return wrapRepositoryError(r.db.WithContext(ctx).
		Exec("LOCK TABLE system.iam_recovery_attempts IN EXCLUSIVE MODE").Error)
}

func (r *Repository) CreateIAMRecoveryAttempt(ctx context.Context, attempt *IAMRecoveryAttempt) error {
	if attempt == nil {
		return fmt.Errorf("%w: IAM recovery attempt is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.sensitiveRecoveryDB(ctx).Create(attempt).Error)
}

func (r *Repository) LockIAMRecoveryAttemptByHash(
	ctx context.Context,
	secretHash string,
) (*IAMRecoveryAttempt, error) {
	var attempt IAMRecoveryAttempt
	err := r.sensitiveRecoveryDB(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("secret_hash = ? AND status = ?", secretHash, IAMRecoveryStatusPrepared).
		Take(&attempt).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &attempt, nil
}

func (r *Repository) ExpirePreparedIAMRecoveryAttempts(
	ctx context.Context,
	now time.Time,
) ([]IAMRecoveryAttempt, error) {
	var attempts []IAMRecoveryAttempt
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("status = ? AND expires_at <= ?", IAMRecoveryStatusPrepared, now).
		Find(&attempts).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	for index := range attempts {
		result := r.db.WithContext(ctx).Model(&IAMRecoveryAttempt{}).
			Where("id = ? AND status = ?", attempts[index].ID, IAMRecoveryStatusPrepared).
			Updates(map[string]any{
				"status": IAMRecoveryStatusExpired, "secret_hash": nil, "expired_at": now,
			})
		if result.Error != nil {
			return nil, wrapRepositoryError(result.Error)
		}
		if result.RowsAffected != 1 {
			return nil, commonapi.ErrConflict
		}
	}
	return attempts, nil
}

func (r *Repository) HasPreparedIAMRecoveryAttempt(ctx context.Context) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&IAMRecoveryAttempt{}).
		Where("status = ?", IAMRecoveryStatusPrepared).
		Count(&count).Error
	return count > 0, wrapRepositoryError(err)
}

func (r *Repository) CompleteIAMRecoveryAttempt(
	ctx context.Context,
	attemptID int64,
	completedAt time.Time,
) error {
	result := r.db.WithContext(ctx).Model(&IAMRecoveryAttempt{}).
		Where("id = ? AND status = ?", attemptID, IAMRecoveryStatusPrepared).
		Updates(map[string]any{
			"status": IAMRecoveryStatusCompleted, "secret_hash": nil, "completed_at": completedAt,
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) LockRecoveryAdministratorTargets(
	ctx context.Context,
	now time.Time,
) ([]RecoveryAdministratorTarget, error) {
	var targets []RecoveryAdministratorTarget
	err := r.db.WithContext(ctx).Raw(`
		SELECT role.role_key,
		       assignment.id AS role_assignment_id,
		       principal.id AS principal_id,
		       principal.status AS principal_status,
		       account.id AS account_id,
		       account.username,
		       account.status AS account_status,
		       app_user.display_name
		FROM system.roles role
		JOIN system.role_assignments assignment ON assignment.role_id = role.id
		JOIN system.principals principal ON principal.id = assignment.principal_id
		JOIN system.users app_user ON app_user.id = principal.id
		JOIN system.local_accounts account ON account.user_id = principal.id
		WHERE role.role_key IN ?
		  AND role.role_type = 'platform_builtin'
		  AND role.status = 'active'
		  AND assignment.scope_type = 'platform'
		  AND assignment.status = 'active'
		  AND assignment.valid_from <= ?
		  AND (assignment.valid_until IS NULL OR assignment.valid_until > ?)
		ORDER BY role.role_key, assignment.id
		FOR UPDATE OF assignment, principal, account
	`, bootstrapRoleOrder, now, now).Scan(&targets).Error
	return targets, wrapRepositoryError(err)
}

func (r *Repository) DisableMFACredential(ctx context.Context, credentialID int64) error {
	result := r.db.WithContext(ctx).Model(&MFACredential{}).
		Where("id = ? AND status = ?", credentialID, MFACredentialStatusActive).
		Update("status", MFACredentialStatusDisabled)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) ResetLocalAccountPassword(
	ctx context.Context,
	accountID int64,
	passwordHash string,
	changedAt time.Time,
) error {
	result := r.sensitiveRecoveryDB(ctx).Model(&LocalAccount{}).
		Where("id = ? AND status IN ?", accountID, []LocalAccountStatus{
			LocalAccountStatusActive, LocalAccountStatusLocked,
		}).
		Updates(map[string]any{
			"password_hash": passwordHash, "password_changed_at": changedAt,
			"status": LocalAccountStatusActive, "locked_until": nil,
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) sensitiveRecoveryDB(ctx context.Context) *gorm.DB {
	return r.db.Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).WithContext(ctx)
}

func (r *Repository) ConsumePendingMFAChallenges(
	ctx context.Context,
	principalID int64,
	consumedAt time.Time,
) (int64, error) {
	result := r.db.WithContext(ctx).Model(&MFAChallenge{}).
		Where("principal_id = ? AND consumed_at IS NULL", principalID).
		Update("consumed_at", consumedAt)
	return result.RowsAffected, wrapRepositoryError(result.Error)
}

func (r *Repository) ConsumePendingMFAEnrollments(
	ctx context.Context,
	principalID int64,
	consumedAt time.Time,
) (int64, error) {
	result := r.db.WithContext(ctx).Model(&MFAEnrollment{}).
		Where("principal_id = ? AND consumed_at IS NULL", principalID).
		Update("consumed_at", consumedAt)
	return result.RowsAffected, wrapRepositoryError(result.Error)
}

func (r *Repository) ConsumeActiveContextSelectionTickets(
	ctx context.Context,
	principalID int64,
	consumedAt time.Time,
) (int64, error) {
	result := r.db.WithContext(ctx).Model(&ContextSelectionTicket{}).
		Where("principal_id = ? AND consumed_at IS NULL AND expires_at >= ?", principalID, consumedAt).
		Update("consumed_at", consumedAt)
	return result.RowsAffected, wrapRepositoryError(result.Error)
}
