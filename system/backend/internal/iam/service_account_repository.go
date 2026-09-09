package iam

import (
	"context"
	"errors"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrServiceAccountVersionConflict = errors.Join(commonapi.ErrConflict, errors.New("service account version conflict"))

type TenantServiceAccount struct {
	ID                   int64
	Name                 string
	Description          string
	OwnerScope           string
	Status               PrincipalStatus
	MembershipID         int64
	MembershipStatus     TenantMembershipStatus
	ClientID             string
	CredentialStatus     OAuthClientStatus
	Version              int64
	CreatedByPrincipalID int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type tenantServiceAccountRow struct {
	ID                   int64                  `gorm:"column:id"`
	Name                 string                 `gorm:"column:name"`
	Description          string                 `gorm:"column:description"`
	OwnerScope           string                 `gorm:"column:owner_scope"`
	Status               PrincipalStatus        `gorm:"column:status"`
	MembershipID         int64                  `gorm:"column:membership_id"`
	MembershipStatus     TenantMembershipStatus `gorm:"column:membership_status"`
	ClientID             string                 `gorm:"column:client_id"`
	CredentialStatus     OAuthClientStatus      `gorm:"column:credential_status"`
	Version              int64                  `gorm:"column:version"`
	CreatedByPrincipalID int64                  `gorm:"column:created_by_principal_id"`
	CreatedAt            time.Time              `gorm:"column:created_at"`
	UpdatedAt            time.Time              `gorm:"column:updated_at"`
}

func (r *Repository) ListTenantServiceAccounts(
	ctx context.Context,
	tenantID int64,
	page int,
	pageSize int,
	search string,
	status *PrincipalStatus,
	ownerScope *string,
) ([]TenantServiceAccount, int64, error) {
	query := r.tenantServiceAccountListQuery(ctx, tenantID)
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		query = query.Where("service_principal.name ILIKE ? OR service_principal.description ILIKE ? OR oauth_client.client_id ILIKE ?", pattern, pattern, pattern)
	}
	if status != nil {
		query = query.Where("principal.status = ?", *status)
	}
	if ownerScope != nil {
		query = query.Where("service_principal.owner_scope = ?", *ownerScope)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var rows []tenantServiceAccountRow
	if err := query.Select(tenantServiceAccountSelect).
		Order("CASE WHEN service_principal.owner_scope = 'tenant' THEN 0 ELSE 1 END, service_principal.updated_at DESC, service_principal.id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	accounts := make([]TenantServiceAccount, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, mapTenantServiceAccountRow(row))
	}
	return accounts, total, nil
}

func (r *Repository) GetTenantServiceAccount(ctx context.Context, tenantID, accountID int64) (*TenantServiceAccount, error) {
	return r.getTenantServiceAccount(ctx, tenantID, accountID, false)
}

func (r *Repository) LockTenantServiceAccount(ctx context.Context, tenantID, accountID int64) (*TenantServiceAccount, error) {
	return r.getTenantServiceAccount(ctx, tenantID, accountID, true)
}

func (r *Repository) getTenantServiceAccount(ctx context.Context, tenantID, accountID int64, lock bool) (*TenantServiceAccount, error) {
	query := r.tenantManagedServiceAccountQuery(ctx, tenantID).Where("service_principal.id = ?", accountID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "service_principal"}})
	}
	var row tenantServiceAccountRow
	if err := query.Select(tenantServiceAccountSelect).Take(&row).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	account := mapTenantServiceAccountRow(row)
	return &account, nil
}

const tenantServiceAccountSelect = `
	service_principal.id,
	service_principal.name,
	service_principal.description,
	service_principal.owner_scope,
	principal.status,
	membership.id AS membership_id,
	membership.status AS membership_status,
	oauth_client.client_id,
	oauth_client.status AS credential_status,
	service_principal.version,
	service_principal.created_by_principal_id,
	service_principal.created_at,
	service_principal.updated_at`

func (r *Repository) tenantServiceAccountListQuery(ctx context.Context, tenantID int64) *gorm.DB {
	return r.db.WithContext(ctx).Table("system.service_principals AS service_principal").
		Joins("JOIN system.principals AS principal ON principal.id = service_principal.id AND principal.principal_type = 'service_principal'").
		Joins("JOIN system.tenant_memberships AS membership ON membership.tenant_id = ? AND membership.principal_id = service_principal.id", tenantID).
		Joins(`JOIN system.oauth_clients AS oauth_client
			ON oauth_client.service_principal_id = service_principal.id
			AND ((service_principal.owner_scope = 'tenant' AND oauth_client.owner_scope = 'tenant' AND oauth_client.owner_tenant_id = ?)
				OR (service_principal.owner_scope = 'platform' AND oauth_client.owner_scope = 'platform'))`, tenantID).
		Where("(service_principal.owner_scope = 'tenant' AND service_principal.owner_tenant_id = ?) OR service_principal.owner_scope = 'platform'", tenantID)
}

func (r *Repository) tenantManagedServiceAccountQuery(ctx context.Context, tenantID int64) *gorm.DB {
	return r.db.WithContext(ctx).Table("system.service_principals AS service_principal").
		Joins("JOIN system.principals AS principal ON principal.id = service_principal.id AND principal.principal_type = 'service_principal'").
		Joins("JOIN system.tenant_memberships AS membership ON membership.tenant_id = service_principal.owner_tenant_id AND membership.principal_id = service_principal.id").
		Joins("JOIN system.oauth_clients AS oauth_client ON oauth_client.service_principal_id = service_principal.id AND oauth_client.owner_scope = 'tenant' AND oauth_client.owner_tenant_id = service_principal.owner_tenant_id").
		Where("service_principal.owner_scope = 'tenant' AND service_principal.owner_tenant_id = ?", tenantID)
}

func (r *Repository) CreateTenantServiceAccount(
	ctx context.Context,
	tenantID int64,
	actorPrincipalID int64,
	name string,
	description string,
	clientID string,
	clientSecretHash string,
	now time.Time,
) (int64, error) {
	principal := &Principal{PrincipalType: PrincipalTypeServicePrincipal, Status: PrincipalStatusActive, AuthorizationVersion: 1}
	if err := r.CreatePrincipal(ctx, principal); err != nil {
		return 0, err
	}
	membership := &TenantMembership{
		TenantID: tenantID, PrincipalID: principal.ID, Status: TenantMembershipStatusActive,
		SourceType: TenantMembershipSourceManual, JoinedAt: now, CreatedByPrincipalID: &actorPrincipalID,
	}
	if err := r.CreateTenantMembership(ctx, membership); err != nil {
		return 0, err
	}
	if err := r.db.WithContext(ctx).Table("system.service_principals").Create(map[string]any{
		"id": principal.ID, "name": name, "description": description,
		"owner_scope": "tenant", "owner_tenant_id": tenantID,
		"created_by_principal_id": actorPrincipalID, "version": int64(1),
	}).Error; err != nil {
		return 0, wrapRepositoryError(err)
	}
	if err := r.db.WithContext(ctx).Table("system.oauth_clients").Create(map[string]any{
		"client_id": clientID, "display_name": name, "client_type": "confidential",
		"client_secret_hash": clientSecretHash, "service_principal_id": principal.ID,
		"redirect_uris": pq.StringArray{}, "grant_types": pq.StringArray{"client_credentials"},
		"response_types": pq.StringArray{}, "allowed_scopes": pq.StringArray{"addp.api"},
		"allowed_audiences": pq.StringArray{"addp.api"}, "token_endpoint_auth_method": "client_secret_basic",
		"request_uris": pq.StringArray{}, "status": OAuthClientStatusActive,
		"owner_scope": "tenant", "owner_tenant_id": tenantID, "version": int64(1),
		"created_by_principal_id": actorPrincipalID,
	}).Error; err != nil {
		return 0, wrapRepositoryError(err)
	}
	return principal.ID, nil
}

func (r *Repository) UpdateTenantServiceAccountDefinition(ctx context.Context, tenantID, accountID, version int64, name, description string) error {
	result := r.db.WithContext(ctx).Table("system.service_principals").
		Where("id = ? AND owner_scope = 'tenant' AND owner_tenant_id = ? AND version = ?", accountID, tenantID, version).
		Updates(map[string]any{"name": name, "description": description, "version": gorm.Expr("version + 1")})
	if err := r.serviceAccountWriteResult(ctx, result, tenantID, accountID); err != nil {
		return err
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Table("system.oauth_clients").
		Where("service_principal_id = ? AND owner_scope = 'tenant' AND owner_tenant_id = ?", accountID, tenantID).
		Updates(map[string]any{"display_name": name, "version": gorm.Expr("version + 1")}).Error)
}

func (r *Repository) UpdateTenantServiceAccountLifecycle(
	ctx context.Context,
	tenantID, accountID, version int64,
	from, to PrincipalStatus,
	fromMembership, toMembership TenantMembershipStatus,
	fromCredential, toCredential OAuthClientStatus,
) error {
	if from == PrincipalStatusActive {
		if err := r.updateServiceAccountCredentialStatus(ctx, tenantID, accountID, fromCredential, toCredential); err != nil {
			return err
		}
		if err := r.updateServiceAccountMembershipStatus(ctx, tenantID, accountID, fromMembership, toMembership); err != nil {
			return err
		}
		if err := r.updateServiceAccountPrincipalStatus(ctx, accountID, from, to); err != nil {
			return err
		}
	} else {
		if err := r.updateServiceAccountPrincipalStatus(ctx, accountID, from, to); err != nil {
			return err
		}
		if err := r.updateServiceAccountMembershipStatus(ctx, tenantID, accountID, fromMembership, toMembership); err != nil {
			return err
		}
		if err := r.updateServiceAccountCredentialStatus(ctx, tenantID, accountID, fromCredential, toCredential); err != nil {
			return err
		}
	}
	result := r.db.WithContext(ctx).Table("system.service_principals").
		Where("id = ? AND owner_scope = 'tenant' AND owner_tenant_id = ? AND version = ?", accountID, tenantID, version).
		Updates(map[string]any{"version": gorm.Expr("version + 1")})
	return r.serviceAccountWriteResult(ctx, result, tenantID, accountID)
}

func (r *Repository) updateServiceAccountPrincipalStatus(ctx context.Context, accountID int64, from, to PrincipalStatus) error {
	result := r.db.WithContext(ctx).Table("system.principals").Where("id = ? AND status = ?", accountID, from).
		Updates(map[string]any{"status": to, "authorization_version": gorm.Expr("authorization_version + 1")})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) updateServiceAccountMembershipStatus(ctx context.Context, tenantID, accountID int64, from, to TenantMembershipStatus) error {
	result := r.db.WithContext(ctx).Table("system.tenant_memberships").
		Where("tenant_id = ? AND principal_id = ? AND status = ?", tenantID, accountID, from).
		Updates(map[string]any{"status": to})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) updateServiceAccountCredentialStatus(ctx context.Context, tenantID, accountID int64, from, to OAuthClientStatus) error {
	result := r.db.WithContext(ctx).Table("system.oauth_clients").
		Where("owner_scope = 'tenant' AND owner_tenant_id = ? AND service_principal_id = ? AND status = ?", tenantID, accountID, from).
		Updates(map[string]any{"status": to, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) RotateTenantServiceAccountSecret(ctx context.Context, tenantID, accountID, version int64, secretHash string) error {
	result := r.db.WithContext(ctx).Table("system.service_principals").
		Where("id = ? AND owner_scope = 'tenant' AND owner_tenant_id = ? AND version = ?", accountID, tenantID, version).
		Updates(map[string]any{"version": gorm.Expr("version + 1")})
	if err := r.serviceAccountWriteResult(ctx, result, tenantID, accountID); err != nil {
		return err
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Table("system.oauth_clients").
		Where("owner_scope = 'tenant' AND owner_tenant_id = ? AND service_principal_id = ?", tenantID, accountID).
		Updates(map[string]any{"client_secret_hash": secretHash, "version": gorm.Expr("version + 1")}).Error)
}

func (r *Repository) serviceAccountWriteResult(ctx context.Context, result *gorm.DB, tenantID, accountID int64) error {
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("system.service_principals").
		Where("id = ? AND owner_scope = 'tenant' AND owner_tenant_id = ?", accountID, tenantID).
		Count(&count).Error; err != nil {
		return wrapRepositoryError(err)
	}
	if count == 0 {
		return commonapi.ErrNotFound
	}
	return ErrServiceAccountVersionConflict
}

func mapTenantServiceAccountRow(row tenantServiceAccountRow) TenantServiceAccount {
	return TenantServiceAccount{
		ID: row.ID, Name: row.Name, Description: row.Description, OwnerScope: row.OwnerScope, Status: row.Status,
		MembershipID: row.MembershipID, MembershipStatus: row.MembershipStatus,
		ClientID: row.ClientID, CredentialStatus: row.CredentialStatus, Version: row.Version,
		CreatedByPrincipalID: row.CreatedByPrincipalID,
		CreatedAt:            row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
}
