package iam

import (
	"context"
	"time"

	"gorm.io/gorm/clause"
)

type administratorCredentialRecovery struct {
	RequestID                    int64 `gorm:"primaryKey"`
	TicketHash                   *string
	EnrollmentHash               *string
	ReviewerPrincipalID          int64
	ReviewerAuthorizationVersion int64
	SecretCiphertext             []byte
	SecretNonce                  []byte
	KeyVersion                   int
	FailedAttempts               int
	ExpiresAt                    time.Time
	ConsumedAt                   *time.Time
}

func (administratorCredentialRecovery) TableName() string {
	return "system.administrator_credential_recoveries"
}

func (r *Repository) administratorChangeApproval(ctx context.Context, id int64) (*PrivilegedChangeApproval, error) {
	var approval PrivilegedChangeApproval
	err := r.db.WithContext(ctx).Where("request_id=?", id).First(&approval).Error
	return &approval, wrapRepositoryError(err)
}
func (r *Repository) createAdministratorRecovery(ctx context.Context, record *administratorCredentialRecovery) error {
	return wrapRepositoryError(r.sensitiveRecoveryDB(ctx).Create(record).Error)
}
func (r *Repository) administratorRecoveryByHash(ctx context.Context, column, hash string) (*administratorCredentialRecovery, error) {
	var record administratorCredentialRecovery
	err := r.sensitiveRecoveryDB(ctx).Where(column+"=?", hash).First(&record).Error
	return &record, wrapRepositoryError(err)
}
func (r *Repository) lockAdministratorRecovery(ctx context.Context, id int64) (*administratorCredentialRecovery, error) {
	var record administratorCredentialRecovery
	err := r.sensitiveRecoveryDB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error
	return &record, wrapRepositoryError(err)
}
func (r *Repository) administratorRecoveryEligible(ctx context.Context, req *PrivilegedChangeRequest, record *administratorCredentialRecovery) (bool, error) {
	var eligible bool
	err := r.db.WithContext(ctx).Raw(`SELECT system.platform_administrator_role(?)=system.administrator_change_requester(?)
 AND system.platform_administrator_role(?)=system.administrator_change_reviewer(?)
 AND system.platform_administrator_role(?)=?`, req.RequestedByPrincipalID, req.GovernanceRoleKey, record.ReviewerPrincipalID, req.GovernanceRoleKey, req.TargetPrincipalID, req.GovernanceRoleKey).Scan(&eligible).Error
	return eligible, wrapRepositoryError(err)
}
func (r *Repository) startAdministratorRecoveryEnrollment(ctx context.Context, record *administratorCredentialRecovery, hash string, ciphertext, nonce []byte, version int) error {
	return wrapRepositoryError(r.sensitiveRecoveryDB(ctx).Model(record).Updates(map[string]any{"ticket_hash": nil, "enrollment_hash": hash, "secret_ciphertext": ciphertext, "secret_nonce": nonce, "key_version": version}).Error)
}
func (r *Repository) recordAdministratorRecoveryFailure(ctx context.Context, record *administratorCredentialRecovery) error {
	return wrapRepositoryError(r.sensitiveRecoveryDB(ctx).Model(record).Update("failed_attempts", record.FailedAttempts+1).Error)
}
func (r *Repository) completeAdministratorRecovery(ctx context.Context, record *administratorCredentialRecovery, req *PrivilegedChangeRequest, now time.Time) error {
	if err := r.sensitiveRecoveryDB(ctx).Model(record).Updates(map[string]any{"enrollment_hash": nil, "secret_ciphertext": nil, "secret_nonce": nil, "consumed_at": now}).Error; err != nil {
		return wrapRepositoryError(err)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Model(req).Updates(map[string]any{"status": "applied", "applied_at": now, "updated_at": now}).Error)
}
