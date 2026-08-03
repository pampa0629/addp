package iam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonrepo "github.com/addp/common/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Transaction(ctx context.Context, operation func(*Repository) error) error {
	if operation == nil {
		return fmt.Errorf("%w: transaction operation is required", commonapi.ErrBadRequest)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return operation(NewRepository(tx))
	})
	return wrapRepositoryError(err)
}

func (r *Repository) ReadOnlyRepeatableReadTransaction(
	ctx context.Context,
	operation func(*Repository) error,
) error {
	if operation == nil {
		return fmt.Errorf("%w: transaction operation is required", commonapi.ErrBadRequest)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return operation(NewRepository(tx))
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return wrapRepositoryError(err)
}

func (r *Repository) CreatePrincipal(ctx context.Context, principal *Principal) error {
	if principal == nil {
		return fmt.Errorf("%w: principal is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(principal).Error)
}

func (r *Repository) CreateUser(ctx context.Context, user *User) error {
	if user == nil {
		return fmt.Errorf("%w: user is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(user).Error)
}

func (r *Repository) CreateLocalAccount(ctx context.Context, account *LocalAccount) error {
	if account == nil {
		return fmt.Errorf("%w: local account is required", commonapi.ErrBadRequest)
	}
	normalized, err := NormalizeUsername(account.Username)
	if err != nil {
		return err
	}
	account.NormalizedUsername = normalized
	return wrapRepositoryError(r.db.WithContext(ctx).Create(account).Error)
}

func (r *Repository) CreateMFACredential(ctx context.Context, credential *MFACredential) error {
	if credential == nil {
		return fmt.Errorf("%w: MFA credential is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(credential).Error)
}

func (r *Repository) HasActiveMFACredential(ctx context.Context, userID int64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&MFACredential{}).
		Where("user_id = ? AND method = ? AND status = ?", userID, "totp", MFACredentialStatusActive).
		Count(&count).Error
	return count > 0, wrapRepositoryError(err)
}

func (r *Repository) LockActiveMFACredential(ctx context.Context, userID int64) (*MFACredential, error) {
	var credential MFACredential
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND method = ? AND status = ?", userID, "totp", MFACredentialStatusActive).
		Take(&credential).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &credential, nil
}

func (r *Repository) UpdateMFALastAcceptedCounter(ctx context.Context, credentialID, counter int64) error {
	result := r.db.WithContext(ctx).Model(&MFACredential{}).
		Where("id = ? AND (last_accepted_counter IS NULL OR last_accepted_counter < ?)", credentialID, counter).
		Update("last_accepted_counter", counter)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrUnauthorized
	}
	return nil
}

func (r *Repository) CreateMFAChallenge(ctx context.Context, challenge *MFAChallenge) error {
	if challenge == nil {
		return fmt.Errorf("%w: MFA challenge is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(challenge).Error)
}

func (r *Repository) GetMFAChallengeByHash(ctx context.Context, tokenHash string) (*MFAChallenge, error) {
	var challenge MFAChallenge
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&challenge).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &challenge, nil
}

func (r *Repository) LockMFAChallengeByHash(ctx context.Context, tokenHash string) (*MFAChallenge, error) {
	var challenge MFAChallenge
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("token_hash = ?", tokenHash).
		Take(&challenge).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &challenge, nil
}

func (r *Repository) RecordMFAChallengeFailure(ctx context.Context, challengeID int64, consume bool, now time.Time) error {
	updates := map[string]any{"failed_attempts": gorm.Expr("failed_attempts + 1")}
	if consume {
		updates["consumed_at"] = now
	}
	result := r.db.WithContext(ctx).Model(&MFAChallenge{}).
		Where("id = ? AND consumed_at IS NULL AND failed_attempts < 5", challengeID).
		Updates(updates)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrUnauthorized
	}
	return nil
}

func (r *Repository) ConsumeMFAChallenge(ctx context.Context, challengeID int64, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&MFAChallenge{}).
		Where("id = ? AND consumed_at IS NULL", challengeID).
		Update("consumed_at", now)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrUnauthorized
	}
	return nil
}

func (r *Repository) CreateMFAEnrollment(ctx context.Context, enrollment *MFAEnrollment) error {
	if enrollment == nil {
		return fmt.Errorf("%w: MFA enrollment is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(enrollment).Error)
}

func (r *Repository) GetMFAEnrollmentByHash(ctx context.Context, tokenHash string) (*MFAEnrollment, error) {
	var enrollment MFAEnrollment
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&enrollment).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &enrollment, nil
}

func (r *Repository) LockMFAEnrollmentByHash(ctx context.Context, tokenHash string) (*MFAEnrollment, error) {
	var enrollment MFAEnrollment
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("token_hash = ?", tokenHash).
		Take(&enrollment).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &enrollment, nil
}

func (r *Repository) RecordMFAEnrollmentFailure(ctx context.Context, enrollmentID int64, consume bool, now time.Time) error {
	updates := map[string]any{"failed_attempts": gorm.Expr("failed_attempts + 1")}
	if consume {
		updates["consumed_at"] = now
	}
	result := r.db.WithContext(ctx).Model(&MFAEnrollment{}).
		Where("id = ? AND consumed_at IS NULL AND failed_attempts < 5", enrollmentID).
		Updates(updates)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrUnauthorized
	}
	return nil
}

func (r *Repository) ConsumeMFAEnrollment(ctx context.Context, enrollmentID int64, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&MFAEnrollment{}).
		Where("id = ? AND consumed_at IS NULL", enrollmentID).
		Update("consumed_at", now)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrUnauthorized
	}
	return nil
}

func (r *Repository) LockIAMBootstrapTable(ctx context.Context) error {
	return wrapRepositoryError(r.db.WithContext(ctx).
		Exec("LOCK TABLE system.iam_bootstrap_state IN EXCLUSIVE MODE").Error)
}

func (r *Repository) LockIAMBootstrapPrincipalWrites(ctx context.Context) error {
	return wrapRepositoryError(r.db.WithContext(ctx).
		Exec("LOCK TABLE system.principals IN SHARE MODE").Error)
}

func (r *Repository) CreateIAMBootstrapState(ctx context.Context, state *IAMBootstrapState) error {
	if state == nil {
		return fmt.Errorf("%w: IAM bootstrap state is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(state).Error)
}

func (r *Repository) LockIAMBootstrapState(ctx context.Context) (*IAMBootstrapState, error) {
	var state IAMBootstrapState
	result := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("singleton = true").
		Limit(1).
		Find(&state)
	if result.Error != nil {
		return nil, wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, commonapi.ErrNotFound
	}
	return &state, nil
}

func (r *Repository) CompleteIAMBootstrap(ctx context.Context, completedAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&IAMBootstrapState{}).
		Where("singleton = true AND status = ? AND secret_hash IS NOT NULL", IAMBootstrapStatusPrepared).
		Updates(map[string]any{
			"status": IAMBootstrapStatusCompleted, "secret_hash": nil, "completed_at": completedAt,
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) CountIAMBootstrapBlockingUserFacts(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Raw(`
		SELECT count(*)
		FROM system.principals
		WHERE principal_type = 'user'
	`).Scan(&count).Error
	return count, wrapRepositoryError(err)
}

func (r *Repository) CreateBootstrapRoleAssignment(
	ctx context.Context,
	principalID int64,
	roleKey string,
	reason string,
	validFrom time.Time,
) (int64, error) {
	var assignmentID int64
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, status, valid_from, source_type, reason)
		SELECT ?, role.id, 'platform', 'active', ?, 'bootstrap', ?
		FROM system.roles role
		WHERE role.role_key = ?
		  AND role.role_type = 'platform_builtin'
		  AND role.status = 'active'
		RETURNING id
	`, principalID, validFrom, reason, roleKey).Scan(&assignmentID).Error
	if err != nil {
		return 0, wrapRepositoryError(err)
	}
	if assignmentID <= 0 {
		return 0, fmt.Errorf("%w: bootstrap role does not exist", commonapi.ErrBadRequest)
	}
	return assignmentID, nil
}

func (r *Repository) CreateTenant(ctx context.Context, tenant *Tenant) error {
	if tenant == nil {
		return fmt.Errorf("%w: tenant is required", commonapi.ErrBadRequest)
	}
	normalized, err := NormalizeTenantCode(tenant.Code)
	if err != nil {
		return err
	}
	tenant.Code = normalized
	return wrapRepositoryError(r.db.WithContext(ctx).Create(tenant).Error)
}

func (r *Repository) CreateTenantMembership(ctx context.Context, membership *TenantMembership) error {
	if membership == nil {
		return fmt.Errorf("%w: tenant membership is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(membership).Error)
}

func (r *Repository) CreateAuditLog(ctx context.Context, auditLog *AuditLog) error {
	if auditLog == nil {
		return fmt.Errorf("%w: audit log is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(auditLog).Error)
}

func (r *Repository) CreateContextSelectionTicket(
	ctx context.Context,
	ticket *ContextSelectionTicket,
) error {
	if ticket == nil {
		return fmt.Errorf("%w: context selection ticket is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(ticket).Error)
}

func (r *Repository) CreateContextSelectionOption(
	ctx context.Context,
	option *ContextSelectionOption,
) error {
	if option == nil {
		return fmt.Errorf("%w: context selection option is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(option).Error)
}

func (r *Repository) CreateRefreshTokenFamily(ctx context.Context, family *RefreshTokenFamily) error {
	if family == nil {
		return fmt.Errorf("%w: refresh token family is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(family).Error)
}

func (r *Repository) CreateAccessToken(ctx context.Context, token *AccessToken) error {
	if token == nil {
		return fmt.Errorf("%w: access token is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(token).Error)
}

func (r *Repository) CreateRefreshToken(ctx context.Context, token *RefreshToken) error {
	if token == nil {
		return fmt.Errorf("%w: refresh token is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(token).Error)
}

func (r *Repository) CreateResourceAccessTicket(
	ctx context.Context,
	ticket *ResourceAccessTicket,
) error {
	if ticket == nil {
		return fmt.Errorf("%w: resource access ticket is required", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(ticket).Error)
}

func (r *Repository) CreateDelegatedAccessToken(
	ctx context.Context,
	token *DelegatedAccessToken,
) error {
	if token == nil {
		return fmt.Errorf("%w: delegated access token is required", commonapi.ErrBadRequest)
	}
	err := r.db.WithContext(ctx).Create(token).Error
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" &&
		postgresError.ConstraintName == "delegated_access_tokens_agent_run_id_tool_call_id_key" {
		return ErrDelegationConflict
	}
	return wrapRepositoryError(err)
}

func (r *Repository) CreateExecutionAuthorization(
	ctx context.Context,
	authorization *ExecutionAuthorization,
) error {
	if authorization == nil {
		return fmt.Errorf("%w: execution authorization is required", commonapi.ErrBadRequest)
	}
	err := r.db.WithContext(ctx).Create(authorization).Error
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" &&
		postgresError.ConstraintName == "execution_authorizations_audience_execution_id_key" {
		return ErrExecutionAuthorizationConflict
	}
	return wrapRepositoryError(err)
}

func (r *Repository) GetExecutionAuthorization(
	ctx context.Context,
	authorizationID int64,
) (*ExecutionAuthorization, error) {
	var authorization ExecutionAuthorization
	if authorizationID <= 0 {
		return nil, commonapi.ErrNotFound
	}
	if err := r.db.WithContext(ctx).First(&authorization, authorizationID).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &authorization, nil
}

func (r *Repository) LockExecutionAuthorization(
	ctx context.Context,
	authorizationID int64,
) (*ExecutionAuthorization, error) {
	var authorization ExecutionAuthorization
	if authorizationID <= 0 {
		return nil, commonapi.ErrNotFound
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&authorization, authorizationID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &authorization, nil
}

func (r *Repository) CreateNotebookSessionAuthorization(
	ctx context.Context,
	authorization *NotebookSessionAuthorization,
) error {
	if authorization == nil {
		return fmt.Errorf("%w: notebook session authorization is required", commonapi.ErrBadRequest)
	}
	err := r.db.WithContext(ctx).Create(authorization).Error
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" &&
		postgresError.ConstraintName == "notebook_session_authorizations_session_id_key" {
		return ErrNotebookSessionAuthorizationConflict
	}
	return wrapRepositoryError(err)
}

func (r *Repository) LockNotebookSessionAuthorization(
	ctx context.Context,
	authorizationID uuid.UUID,
) (*NotebookSessionAuthorization, error) {
	if authorizationID == uuid.Nil {
		return nil, commonapi.ErrNotFound
	}
	var authorization NotebookSessionAuthorization
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", authorizationID).
		Take(&authorization).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &authorization, nil
}

func (r *Repository) GetNotebookSessionAuthorization(
	ctx context.Context,
	authorizationID uuid.UUID,
) (*NotebookSessionAuthorization, error) {
	if authorizationID == uuid.Nil {
		return nil, commonapi.ErrNotFound
	}
	var authorization NotebookSessionAuthorization
	if err := r.db.WithContext(ctx).
		Where("id = ?", authorizationID).
		Take(&authorization).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &authorization, nil
}

// RevokeNotebookSessionAuthorization is intentionally idempotent and does not
// reveal whether a UUID belongs to another Tenant or Session.
func (r *Repository) RevokeNotebookSessionAuthorization(
	ctx context.Context,
	authorizationID, sessionID uuid.UUID,
	tenantID int64,
	revokedAt time.Time,
	reason string,
) error {
	if authorizationID == uuid.Nil || sessionID == uuid.Nil || tenantID <= 0 ||
		strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: invalid notebook session authorization revocation", commonapi.ErrBadRequest)
	}
	return wrapRepositoryError(r.db.WithContext(ctx).
		Model(&NotebookSessionAuthorization{}).
		Where("id = ? AND session_id = ? AND tenant_id = ? AND revoked_at IS NULL", authorizationID, sessionID, tenantID).
		Updates(map[string]any{"revoked_at": revokedAt, "revoked_reason": reason}).Error)
}

func (r *Repository) FindExecutionAuthorization(
	ctx context.Context,
	audience string,
	executionID uuid.UUID,
) (*ExecutionAuthorization, error) {
	var authorization ExecutionAuthorization
	if executionID == uuid.Nil || strings.TrimSpace(audience) == "" {
		return nil, commonapi.ErrNotFound
	}
	if err := r.db.WithContext(ctx).
		Where("audience = ? AND execution_id = ?", audience, executionID).
		Take(&authorization).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &authorization, nil
}

func (r *Repository) ExecutionAuthorizationEngineAvailable(
	ctx context.Context,
	tenantID int64,
	engineID int64,
) (bool, error) {
	if tenantID <= 0 || engineID <= 0 {
		return false, commonapi.ErrBadRequest
	}
	var available bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM system.engines engine
			WHERE engine.id = ?
			  AND engine.lifecycle_state = 'active'
			  AND (
				  engine.tenant_id = ?
				  OR (engine.tenant_id IS NULL AND engine.is_builtin = true)
			  )
		)
	`, engineID, tenantID).Scan(&available).Error
	return available, wrapRepositoryError(err)
}

type ExecutionAuthorizationProvenance struct {
	ParentExecutionID          uuid.UUID `gorm:"column:parent_execution_id"`
	ExecutionID                uuid.UUID `gorm:"column:execution_id"`
	TenantID                   int64     `gorm:"column:tenant_id"`
	ActorPrincipalID           int64     `gorm:"column:actor_principal_id"`
	ActorTenantMembershipID    int64     `gorm:"column:actor_tenant_membership_id"`
	IssuedAuthorizationVersion int64     `gorm:"column:issued_authorization_version"`
}

func (r *Repository) GetExecutionAuthorizationProvenance(
	ctx context.Context,
	parentExecutionID uuid.UUID,
	executionID uuid.UUID,
	audience string,
) (*ExecutionAuthorizationProvenance, error) {
	if parentExecutionID == uuid.Nil || executionID == uuid.Nil || strings.TrimSpace(audience) == "" {
		return nil, commonapi.ErrBadRequest
	}
	var provenance ExecutionAuthorizationProvenance
	err := r.db.WithContext(ctx).Raw(`
		SELECT parent.execution_id AS parent_execution_id,
		       child.execution_id,
		       child.tenant_id,
		       child.actor_principal_id,
		       child.actor_tenant_membership_id,
		       child.issued_authorization_version
		FROM common.task_executions child
		JOIN common.task_executions parent
		  ON parent.execution_id = child.parent_execution_id
		 AND parent.tenant_id = child.tenant_id
		WHERE parent.execution_id = ?
		  AND child.execution_id = ?
		  AND parent.module = 'orchestrator'
		  AND child.module = ?
		  AND parent.status = 'running'
		  AND child.status = 'pending'
		  AND parent.actor_principal_id = child.actor_principal_id
		  AND parent.actor_tenant_membership_id = child.actor_tenant_membership_id
		  AND parent.issued_authorization_version = child.issued_authorization_version
		  AND child.actor_principal_id IS NOT NULL
		  AND child.actor_tenant_membership_id IS NOT NULL
		  AND child.issued_authorization_version IS NOT NULL
	`, parentExecutionID, executionID, audience).Take(&provenance).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &provenance, nil
}

func (r *Repository) LockTaskAuthorizationSubject(
	ctx context.Context,
	ownerModule string,
	taskRef uuid.UUID,
) (*TaskAuthorizationSubject, error) {
	var subject TaskAuthorizationSubject
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("owner_module = ? AND task_ref = ?", ownerModule, taskRef).
		Take(&subject).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &subject, nil
}

func (r *Repository) CreateTaskAuthorizationSubject(
	ctx context.Context,
	subject *TaskAuthorizationSubject,
) error {
	if subject == nil {
		return commonapi.ErrBadRequest
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Create(subject).Error)
}

func (r *Repository) UpdateTaskAuthorizationSubject(
	ctx context.Context,
	subject *TaskAuthorizationSubject,
) error {
	if subject == nil || subject.ID <= 0 {
		return commonapi.ErrBadRequest
	}
	result := r.db.WithContext(ctx).Model(&TaskAuthorizationSubject{}).
		Where("id = ? AND tenant_id = ?", subject.ID, subject.TenantID).
		Updates(map[string]any{
			"definition_hash":       subject.DefinitionHash,
			"principal_id":          subject.PrincipalID,
			"tenant_membership_id":  subject.TenantMembershipID,
			"authorization_version": subject.AuthorizationVersion,
			"authorized_at":         subject.AuthorizedAt,
			"updated_at":            subject.UpdatedAt,
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *Repository) GetTaskAuthorizationSubject(
	ctx context.Context,
	id int64,
) (*TaskAuthorizationSubject, error) {
	var subject TaskAuthorizationSubject
	if id <= 0 {
		return nil, commonapi.ErrNotFound
	}
	if err := r.db.WithContext(ctx).First(&subject, id).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &subject, nil
}

func (r *Repository) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var token RefreshToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&token).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &token, nil
}

func (r *Repository) GetAccessTokenByHash(ctx context.Context, tokenHash string) (*AccessToken, error) {
	var token AccessToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Take(&token).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &token, nil
}

func (r *Repository) GetRefreshTokenFamily(ctx context.Context, familyID int64) (*RefreshTokenFamily, error) {
	var family RefreshTokenFamily
	if err := r.db.WithContext(ctx).First(&family, familyID).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &family, nil
}

func (r *Repository) LockRefreshTokenFamily(ctx context.Context, familyID int64) (*RefreshTokenFamily, error) {
	var family RefreshTokenFamily
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&family, familyID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &family, nil
}

func (r *Repository) AdvanceRefreshTokenFamilyAuthorizationVersion(
	ctx context.Context,
	familyID int64,
	currentVersion int64,
) error {
	result := r.db.WithContext(ctx).Model(&RefreshTokenFamily{}).
		Where("id = ? AND revoked_at IS NULL AND issued_authorization_version < ?", familyID, currentVersion).
		Update("issued_authorization_version", currentVersion)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: token family authorization version was not advanced", commonapi.ErrConflict)
	}
	return nil
}

func (r *Repository) RefreshTokenFamilyContextIsActive(
	ctx context.Context,
	principal *Principal,
	family *RefreshTokenFamily,
	at time.Time,
) (bool, error) {
	if principal == nil || family == nil || principal.ID != family.PrincipalID ||
		principal.Status != PrincipalStatusActive || !family.ExpiresAt.After(at) || family.RevokedAt != nil {
		return false, nil
	}
	switch family.ContextType {
	case ContextTypePlatform:
		if family.TenantMembershipID != nil ||
			!validPlatformContextAssurance(principal.PrincipalType, family.AssuranceLevel) {
			return false, nil
		}
		return r.HasEffectivePlatformRole(ctx, principal.ID, at)
	case ContextTypeTenant:
		if family.TenantMembershipID == nil {
			return false, nil
		}
		membership, err := r.GetTenantMembershipByID(ctx, *family.TenantMembershipID)
		if err != nil {
			if errors.Is(err, commonapi.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		if membership.PrincipalID != principal.ID || membership.Status != TenantMembershipStatusActive ||
			(membership.ExpiresAt != nil && !membership.ExpiresAt.After(at)) {
			return false, nil
		}
		tenant, err := r.GetTenant(ctx, membership.TenantID)
		if err != nil {
			if errors.Is(err, commonapi.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		return tenant.Status == TenantStatusActive, nil
	default:
		return false, nil
	}
}

func (r *Repository) LockRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var token RefreshToken
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("token_hash = ?", tokenHash).
		Take(&token).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &token, nil
}

func (r *Repository) LockAccessToken(ctx context.Context, tokenID int64) (*AccessToken, error) {
	var token AccessToken
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&token, tokenID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &token, nil
}

func (r *Repository) LockActiveResourceAccessTickets(
	ctx context.Context,
	familyID int64,
) ([]ResourceAccessTicket, error) {
	var tickets []ResourceAccessTicket
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Order("id ASC").
		Find(&tickets).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return tickets, nil
}

func (r *Repository) MarkRefreshTokenUsed(ctx context.Context, tokenID int64, usedAt time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("id = ? AND used_at IS NULL AND revoked_at IS NULL", tokenID).
		Update("used_at", usedAt)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: refresh token is no longer current", commonapi.ErrConflict)
	}
	return nil
}

func (r *Repository) LinkRefreshTokenReplacement(
	ctx context.Context,
	tokenID int64,
	replacementTokenID int64,
) error {
	result := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("id = ? AND replaced_by_token_id IS NULL", tokenID).
		Update("replaced_by_token_id", replacementTokenID)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: refresh token replacement is already linked", commonapi.ErrConflict)
	}
	return nil
}

func (r *Repository) MarkRefreshTokenReuseDetected(
	ctx context.Context,
	tokenID int64,
	detectedAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&RefreshToken{}).
		Where("id = ? AND used_at IS NOT NULL AND reuse_detected_at IS NULL", tokenID).
		Update("reuse_detected_at", detectedAt)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: refresh token reuse was already handled", commonapi.ErrConflict)
	}
	return nil
}

func (r *Repository) RevokeAccessToken(ctx context.Context, tokenID int64, revokedAt time.Time) error {
	return wrapRepositoryError(r.db.WithContext(ctx).
		Model(&AccessToken{}).
		Where("id = ? AND revoked_at IS NULL", tokenID).
		Update("revoked_at", revokedAt).Error)
}

func (r *Repository) RevokeActiveResourceAccessTickets(
	ctx context.Context,
	familyID int64,
	revokedAt time.Time,
) error {
	return wrapRepositoryError(r.db.WithContext(ctx).
		Model(&ResourceAccessTicket{}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Update("revoked_at", revokedAt).Error)
}

func (r *Repository) RevokeTokenFamily(
	ctx context.Context,
	familyID int64,
	revokedAt time.Time,
	reason string,
) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: token family revocation reason is required", commonapi.ErrBadRequest)
	}
	result := r.db.WithContext(ctx).
		Model(&RefreshTokenFamily{}).
		Where("id = ? AND revoked_at IS NULL", familyID).
		Updates(map[string]any{"revoked_at": revokedAt, "revoked_reason": reason})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: token family is already revoked", commonapi.ErrConflict)
	}
	return nil
}

func (r *Repository) GetPrincipal(ctx context.Context, principalID int64) (*Principal, error) {
	var principal Principal
	err := r.db.WithContext(ctx).First(&principal, principalID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &principal, nil
}

func (r *Repository) GetUser(ctx context.Context, userID int64) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).First(&user, userID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &user, nil
}

func (r *Repository) GetTenant(ctx context.Context, tenantID int64) (*Tenant, error) {
	var tenant Tenant
	err := r.db.WithContext(ctx).First(&tenant, tenantID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &tenant, nil
}

func (r *Repository) LockPrincipal(ctx context.Context, principalID int64) (*Principal, error) {
	var principal Principal
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&principal, principalID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &principal, nil
}

func (r *Repository) LockTenant(ctx context.Context, tenantID int64) (*Tenant, error) {
	var tenant Tenant
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "SHARE"}).
		First(&tenant, tenantID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &tenant, nil
}

func (r *Repository) HasEffectivePlatformRole(
	ctx context.Context,
	principalID int64,
	at time.Time,
) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (
		    SELECT 1
		    FROM system.role_assignments assignment
		    JOIN system.roles role ON role.id = assignment.role_id
		    WHERE assignment.principal_id = ?
		      AND assignment.scope_type = 'platform'
		      AND assignment.status = 'active'
		      AND assignment.valid_from <= ?
		      AND (assignment.valid_until IS NULL OR assignment.valid_until > ?)
		      AND role.status = 'active'
		)
	`, principalID, at, at).Scan(&exists).Error
	return exists, wrapRepositoryError(err)
}

func (r *Repository) GetContextSelectionTicketByHash(
	ctx context.Context,
	tokenHash string,
) (*ContextSelectionTicket, error) {
	var ticket ContextSelectionTicket
	err := r.db.WithContext(ctx).
		Where("token_hash = ?", tokenHash).
		Take(&ticket).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &ticket, nil
}

func (r *Repository) LockContextSelectionTicketByHash(
	ctx context.Context,
	tokenHash string,
) (*ContextSelectionTicket, error) {
	var ticket ContextSelectionTicket
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("token_hash = ?", tokenHash).
		Take(&ticket).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &ticket, nil
}

func (r *Repository) GetContextSelectionOption(
	ctx context.Context,
	ticketID int64,
	contextType ContextType,
	tenantMembershipID *int64,
) (*ContextSelectionOption, error) {
	var option ContextSelectionOption
	query := r.db.WithContext(ctx).
		Where("ticket_id = ? AND context_type = ?", ticketID, contextType)
	if tenantMembershipID == nil {
		query = query.Where("tenant_membership_id IS NULL")
	} else {
		query = query.Where("tenant_membership_id = ?", *tenantMembershipID)
	}
	if err := query.Take(&option).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &option, nil
}

func (r *Repository) ConsumeContextSelectionTicket(
	ctx context.Context,
	ticketID int64,
	consumedAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&ContextSelectionTicket{}).
		Where("id = ? AND consumed_at IS NULL", ticketID).
		Update("consumed_at", consumedAt)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: context selection ticket is already consumed", commonapi.ErrConflict)
	}
	return nil
}

func (r *Repository) LockTenantMembershipByID(
	ctx context.Context,
	membershipID int64,
) (*TenantMembership, error) {
	var membership TenantMembership
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&membership, membershipID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &membership, nil
}

func (r *Repository) GetTenantMembershipByID(
	ctx context.Context,
	membershipID int64,
) (*TenantMembership, error) {
	var membership TenantMembership
	if err := r.db.WithContext(ctx).First(&membership, membershipID).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &membership, nil
}

func (r *Repository) GetLocalAccountByNormalizedUsername(
	ctx context.Context,
	normalizedUsername string,
) (*LocalAccount, error) {
	var account LocalAccount
	err := r.db.WithContext(ctx).
		Where("normalized_username = ?", normalizedUsername).
		Take(&account).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &account, nil
}

func (r *Repository) LockLocalAccountByUserID(ctx context.Context, userID int64) (*LocalAccount, error) {
	var account LocalAccount
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		Take(&account).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &account, nil
}

func (r *Repository) GetLocalAccountByUserID(ctx context.Context, userID int64) (*LocalAccount, error) {
	var account LocalAccount
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Take(&account).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &account, nil
}

func (r *Repository) UpdateLocalAccountLastAuthenticated(
	ctx context.Context,
	accountID int64,
	authenticatedAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&LocalAccount{}).
		Where("id = ?", accountID).
		Update("last_authenticated_at", authenticatedAt)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *Repository) UpdateLocalAccountPassword(
	ctx context.Context,
	accountID int64,
	passwordHash string,
	changedAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&LocalAccount{}).
		Where("id = ?", accountID).
		Updates(map[string]any{
			"password_hash":       passwordHash,
			"password_changed_at": changedAt,
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *Repository) IncrementPrincipalAuthorizationVersion(
	ctx context.Context,
	principalID int64,
) (int64, error) {
	var authorizationVersion int64
	err := r.db.WithContext(ctx).Raw(`
		UPDATE system.principals
		SET authorization_version = authorization_version + 1,
		    updated_at = now()
		WHERE id = ?
		RETURNING authorization_version
	`, principalID).Row().Scan(&authorizationVersion)
	if err != nil {
		return 0, wrapRepositoryError(err)
	}
	return authorizationVersion, nil
}

func (r *Repository) RevokeActiveTokenFamilies(
	ctx context.Context,
	principalID int64,
	revokedAt time.Time,
	reason string,
) (int64, error) {
	if reason == "" {
		return 0, fmt.Errorf("%w: token family revocation reason is required", commonapi.ErrBadRequest)
	}
	result := r.db.WithContext(ctx).
		Table("system.refresh_token_families").
		Where("principal_id = ? AND revoked_at IS NULL", principalID).
		Updates(map[string]any{
			"revoked_at":     revokedAt,
			"revoked_reason": reason,
		})
	return result.RowsAffected, wrapRepositoryError(result.Error)
}

func (r *Repository) LockTenantMembership(
	ctx context.Context,
	tenantID int64,
	principalID int64,
) (*TenantMembership, error) {
	var membership TenantMembership
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND principal_id = ?", tenantID, principalID).
		Take(&membership).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &membership, nil
}

func (r *Repository) UpdateTenantMembershipLifecycle(
	ctx context.Context,
	membershipID int64,
	status TenantMembershipStatus,
	endedAt *time.Time,
	expiresAt *time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&TenantMembership{}).
		Where("id = ?", membershipID).
		Updates(map[string]any{
			"status":     status,
			"ended_at":   endedAt,
			"expires_at": expiresAt,
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *Repository) GetLocalUserIdentityByUsername(ctx context.Context, username string) (*LocalUserIdentity, error) {
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return nil, err
	}

	var identity LocalUserIdentity
	err = r.db.WithContext(ctx).
		Table("system.local_accounts AS account").
		Select(`
			principal.id AS principal_id,
			principal.status AS principal_status,
			principal.authorization_version,
			user_profile.display_name,
			user_profile.primary_email,
			user_profile.locale,
			account.id AS account_id,
			account.username,
			account.normalized_username,
			account.password_hash,
			account.status AS account_status,
			account.locked_until,
			account.password_changed_at,
			account.last_authenticated_at
		`).
		Joins("JOIN system.users AS user_profile ON user_profile.id = account.user_id").
		Joins("JOIN system.principals AS principal ON principal.id = user_profile.id AND principal.principal_type = ?", PrincipalTypeUser).
		Where("account.normalized_username = ?", normalized).
		Take(&identity).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &identity, nil
}

func (r *Repository) ListEffectiveTenantMemberships(
	ctx context.Context,
	principalID int64,
	at time.Time,
) ([]EffectiveTenantMembership, error) {
	var memberships []EffectiveTenantMembership
	err := r.db.WithContext(ctx).
		Table("system.tenant_memberships AS membership").
		Select(`
			membership.id AS membership_id,
			tenant.id AS tenant_id,
			tenant.code AS tenant_code,
			tenant.name AS tenant_name,
			membership.status AS membership_status,
			tenant.status AS tenant_status,
			membership.joined_at,
			membership.expires_at
		`).
		Joins("JOIN system.principals AS principal ON principal.id = membership.principal_id").
		Joins("JOIN system.tenants AS tenant ON tenant.id = membership.tenant_id").
		Where("membership.principal_id = ?", principalID).
		Where("principal.status = ?", PrincipalStatusActive).
		Where("membership.status = ?", TenantMembershipStatusActive).
		Where("tenant.status = ?", TenantStatusActive).
		Where("membership.joined_at <= ?", at).
		Where("membership.expires_at IS NULL OR membership.expires_at > ?", at).
		Order("tenant.code ASC, tenant.id ASC").
		Scan(&memberships).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return memberships, nil
}

func wrapRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	mapped := commonrepo.WrapDBError(err)
	if errors.Is(mapped, commonapi.ErrNotFound) || errors.Is(mapped, commonapi.ErrConflict) {
		return mapped
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return commonapi.ErrConflict
		case "23502", "23503", "23514":
			return fmt.Errorf("%w: database constraint %s", commonapi.ErrBadRequest, postgresError.ConstraintName)
		}
	}
	return err
}
