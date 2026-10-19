package iam

import (
	"context"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) CreatePrivilegedChangeRequest(
	ctx context.Context,
	request *PrivilegedChangeRequest,
) error {
	if request == nil {
		return commonapi.ErrBadRequest
	}
	return wrapAdministratorChangeError(r.db.WithContext(ctx).Create(request).Error)
}

func (r *Repository) CreatePrivilegedChangeApproval(
	ctx context.Context,
	approval *PrivilegedChangeApproval,
) error {
	if approval == nil {
		return commonapi.ErrBadRequest
	}
	return wrapAdministratorChangeError(r.db.WithContext(ctx).Create(approval).Error)
}

func (r *Repository) GetPrivilegedChangeRequest(
	ctx context.Context,
	requestID int64,
) (*PrivilegedChangeRequest, error) {
	var request PrivilegedChangeRequest
	if err := r.db.WithContext(ctx).Preload("Approval").First(&request, requestID).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &request, nil
}

func (r *Repository) LockPrivilegedChangeRequest(
	ctx context.Context,
	requestID int64,
) (*PrivilegedChangeRequest, error) {
	var request PrivilegedChangeRequest
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&request, requestID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &request, nil
}

func (r *Repository) ListAdministratorChangeRequests(
	ctx context.Context,
	page int,
	pageSize int,
	status *PrivilegedChangeStatus,
	targetPrincipalID *int64,
) ([]PrivilegedChangeRequest, int64, error) {
	query := r.db.WithContext(ctx).Model(&PrivilegedChangeRequest{}).
		Where("change_type IN ?", []PrivilegedChangeType{
			PrivilegedChangePlatformIdentitySuspend,
			PrivilegedChangePlatformIdentityReactivate,
			PrivilegedChangePlatformIdentityDeactivate,
			PrivilegedChangePlatformRoleGrant, PrivilegedChangePlatformRoleRevoke, PrivilegedChangePlatformRoleReplace, PrivilegedChangePlatformCredentialRecovery,
		})
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	if targetPrincipalID != nil {
		query = query.Where("target_principal_id = ?", *targetPrincipalID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var requests []PrivilegedChangeRequest
	if err := query.Preload("Approval").Order("requested_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&requests).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	return requests, total, nil
}

func wrapAdministratorChangeError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "42501" {
			return commonapi.ErrForbidden
		}
		if pg.Code == "23514" {
			return commonapi.ErrConflict
		}
	}
	return wrapRepositoryError(err)
}

func (r *Repository) grantAdministratorRole(ctx context.Context, req *PrivilegedChangeRequest, principalID int64, now time.Time) error {
	return wrapRepositoryError(r.db.WithContext(ctx).Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type,created_by_principal_id,grant_reason,grant_change_request_id)
 VALUES(?,?,'platform','active',?,'manual',?,?,?)`, principalID, req.TargetRoleID, now, req.RequestedByPrincipalID, req.Reason, req.ID).Error)
}
func (r *Repository) revokeAdministratorRole(ctx context.Context, req *PrivilegedChangeRequest, now time.Time) error {
	if req.TargetAssignmentID == nil {
		return commonapi.ErrConflict
	}
	res := r.db.WithContext(ctx).Exec(`UPDATE system.role_assignments SET status='revoked',revoked_by_principal_id=?,revoked_at=?,revoked_reason=?,revoke_change_request_id=?,updated_at=? WHERE id=? AND status='active'`, req.RequestedByPrincipalID, now, req.Reason, req.ID, now, *req.TargetAssignmentID)
	if res.Error != nil {
		return wrapRepositoryError(res.Error)
	}
	if res.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) administratorRoleID(ctx context.Context, key string) (int64, error) {
	var role Role
	err := r.db.WithContext(ctx).Where("role_key=? AND tenant_id IS NULL AND status='active'", key).First(&role).Error
	return role.ID, wrapRepositoryError(err)
}
func (r *Repository) invalidAdministratorChanges(ctx context.Context, target int64) ([]PrivilegedChangeRequest, error) {
	var rows []PrivilegedChangeRequest
	err := r.db.WithContext(ctx).Where(`target_principal_id=? AND status IN ('pending','approved') AND
 (expires_at<=statement_timestamp() OR target_authorization_version IS DISTINCT FROM (SELECT authorization_version FROM system.principals WHERE id=target_principal_id)
 OR requester_authorization_version IS DISTINCT FROM (SELECT authorization_version FROM system.principals WHERE id=requested_by_principal_id)
 OR EXISTS(SELECT 1 FROM system.administrator_credential_recoveries recovery WHERE recovery.request_id=system.privileged_change_requests.id AND (recovery.expires_at<=statement_timestamp() OR recovery.failed_attempts>=5)))`, target).Find(&rows).Error
	return rows, wrapRepositoryError(err)
}
func (r *Repository) cancelAdministratorChange(ctx context.Context, req *PrivilegedChangeRequest) error {
	result := r.db.WithContext(ctx).Model(req).Where("version=? AND status IN ('pending','approved')", req.Version).Updates(map[string]any{"status": "cancelled", "decided_at": gorm.Expr("statement_timestamp()"), "updated_at": gorm.Expr("statement_timestamp()")})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) lockAdministratorRole(ctx context.Context, id int64) error {
	var role Role
	return wrapRepositoryError(r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&role, id).Error)
}
func (r *Repository) availableAdministratorPeerCount(ctx context.Context, roleID, targetID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM system.role_assignments a JOIN system.principals p ON p.id=a.principal_id
 WHERE a.role_id=? AND a.scope_type='platform' AND a.status='active' AND a.valid_from<=statement_timestamp()
 AND(a.valid_until IS NULL OR a.valid_until>statement_timestamp()) AND p.status='active' AND p.id<>?
 AND EXISTS(SELECT 1 FROM system.local_accounts account WHERE account.user_id=p.id AND account.status='active' AND (account.locked_until IS NULL OR account.locked_until<=statement_timestamp()))
 AND EXISTS(SELECT 1 FROM system.mfa_credentials mfa WHERE mfa.user_id=p.id AND mfa.status='active')`, roleID, targetID).Scan(&count).Error
	return count, wrapRepositoryError(err)
}
func (r *Repository) listAdministratorChangeCandidates(ctx context.Context, page, size int, search string) ([]AdministratorChangeCandidate, int64, error) {
	var rows []AdministratorChangeCandidate
	var total int64
	q := r.db.WithContext(ctx).Table("system.principals p").Joins("JOIN system.users u ON u.id=p.id").Where("p.principal_type='user' AND p.status IN ('active','suspended')")
	if search != "" {
		q = q.Where("u.display_name ILIKE ?", "%"+search+"%")
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	err := q.Select("p.id,u.display_name,p.status,system.platform_administrator_role(p.id) AS role_key,EXISTS(SELECT 1 FROM system.mfa_credentials m WHERE m.user_id=p.id AND m.status='active') AS mfa_enrolled").Order("p.id").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	return rows, total, wrapRepositoryError(err)
}
