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
