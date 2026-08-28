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

var ErrOAuthClientVersionConflict = errors.Join(commonapi.ErrConflict, errors.New("OAuth client version conflict"))

type OAuthClientStatus string

const (
	OAuthClientStatusActive   OAuthClientStatus = "active"
	OAuthClientStatusDisabled OAuthClientStatus = "disabled"
)

type ManagedOAuthClient struct {
	ClientID           string
	DisplayName        string
	RedirectURIs       []string
	GrantTypes         []string
	ResponseTypes      []string
	AllowedScopes      []string
	AllowedAudiences   []string
	TokenAuthMethod    string
	Status             OAuthClientStatus
	Version            int64
	CreatedByPrincipal int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type managedOAuthClientRow struct {
	ClientID             string         `gorm:"column:client_id"`
	DisplayName          string         `gorm:"column:display_name"`
	RedirectURIs         pq.StringArray `gorm:"column:redirect_uris;type:text[]"`
	GrantTypes           pq.StringArray `gorm:"column:grant_types;type:text[]"`
	ResponseTypes        pq.StringArray `gorm:"column:response_types;type:text[]"`
	AllowedScopes        pq.StringArray `gorm:"column:allowed_scopes;type:text[]"`
	AllowedAudiences     pq.StringArray `gorm:"column:allowed_audiences;type:text[]"`
	TokenAuthMethod      string         `gorm:"column:token_endpoint_auth_method"`
	Status               OAuthClientStatus
	Version              int64
	CreatedByPrincipalID int64 `gorm:"column:created_by_principal_id"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (managedOAuthClientRow) TableName() string { return "system.oauth_clients" }

func (r *Repository) ListManagedOAuthClients(
	ctx context.Context,
	tenantID int64,
	page int,
	pageSize int,
	search string,
	status *OAuthClientStatus,
) ([]ManagedOAuthClient, int64, error) {
	query := r.db.WithContext(ctx).Model(&managedOAuthClientRow{}).
		Where("owner_scope = 'tenant' AND owner_tenant_id = ?", tenantID)
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		query = query.Where("client_id ILIKE ? OR display_name ILIKE ?", pattern, pattern)
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var rows []managedOAuthClientRow
	if err := query.Order("updated_at DESC, client_id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	clients := make([]ManagedOAuthClient, 0, len(rows))
	for _, row := range rows {
		clients = append(clients, mapManagedOAuthClientRow(row))
	}
	return clients, total, nil
}

func (r *Repository) GetManagedOAuthClient(ctx context.Context, tenantID int64, clientID string) (*ManagedOAuthClient, error) {
	row, err := r.getManagedOAuthClientRow(ctx, tenantID, clientID, false)
	if err != nil {
		return nil, err
	}
	client := mapManagedOAuthClientRow(*row)
	return &client, nil
}

func (r *Repository) LockManagedOAuthClient(ctx context.Context, tenantID int64, clientID string) (*ManagedOAuthClient, error) {
	row, err := r.getManagedOAuthClientRow(ctx, tenantID, clientID, true)
	if err != nil {
		return nil, err
	}
	client := mapManagedOAuthClientRow(*row)
	return &client, nil
}

func (r *Repository) getManagedOAuthClientRow(ctx context.Context, tenantID int64, clientID string, lock bool) (*managedOAuthClientRow, error) {
	query := r.db.WithContext(ctx).
		Where("owner_scope = 'tenant' AND owner_tenant_id = ? AND client_id = ?", tenantID, clientID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row managedOAuthClientRow
	if err := query.Take(&row).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &row, nil
}

func (r *Repository) CreateManagedOAuthClient(ctx context.Context, tenantID int64, client *ManagedOAuthClient) error {
	if client == nil {
		return commonapi.ErrBadRequest
	}
	row := managedOAuthClientRow{
		ClientID: client.ClientID, DisplayName: client.DisplayName,
		RedirectURIs:     pq.StringArray(append([]string(nil), client.RedirectURIs...)),
		GrantTypes:       pq.StringArray(append([]string(nil), client.GrantTypes...)),
		ResponseTypes:    pq.StringArray(append([]string(nil), client.ResponseTypes...)),
		AllowedScopes:    pq.StringArray(append([]string(nil), client.AllowedScopes...)),
		AllowedAudiences: pq.StringArray(append([]string(nil), client.AllowedAudiences...)),
		TokenAuthMethod:  client.TokenAuthMethod, Status: client.Status, Version: client.Version,
		CreatedByPrincipalID: client.CreatedByPrincipal,
	}
	return wrapRepositoryError(r.db.WithContext(ctx).Table("system.oauth_clients").Create(map[string]any{
		"client_id": row.ClientID, "display_name": row.DisplayName, "client_type": "public",
		"client_secret_hash": nil, "service_principal_id": nil,
		"redirect_uris": row.RedirectURIs, "grant_types": row.GrantTypes,
		"response_types": row.ResponseTypes, "allowed_scopes": row.AllowedScopes,
		"allowed_audiences": row.AllowedAudiences, "token_endpoint_auth_method": row.TokenAuthMethod,
		"request_uris": pq.StringArray{}, "status": row.Status, "owner_scope": "tenant",
		"owner_tenant_id": tenantID, "version": row.Version,
		"created_by_principal_id": row.CreatedByPrincipalID,
	}).Error)
}

func (r *Repository) UpdateManagedOAuthClient(
	ctx context.Context,
	tenantID int64,
	clientID string,
	version int64,
	displayName string,
	redirectURIs []string,
) error {
	result := r.db.WithContext(ctx).Table("system.oauth_clients").
		Where("owner_scope = 'tenant' AND owner_tenant_id = ? AND client_id = ? AND version = ?", tenantID, clientID, version).
		Updates(map[string]any{
			"display_name":  displayName,
			"redirect_uris": pq.StringArray(append([]string(nil), redirectURIs...)),
			"version":       gorm.Expr("version + 1"),
		})
	return r.managedOAuthClientWriteResult(ctx, result, tenantID, clientID)
}

func (r *Repository) UpdateManagedOAuthClientStatus(
	ctx context.Context,
	tenantID int64,
	clientID string,
	version int64,
	from OAuthClientStatus,
	to OAuthClientStatus,
) error {
	result := r.db.WithContext(ctx).Table("system.oauth_clients").
		Where("owner_scope = 'tenant' AND owner_tenant_id = ? AND client_id = ? AND version = ? AND status = ?", tenantID, clientID, version, from).
		Updates(map[string]any{"status": to, "version": gorm.Expr("version + 1")})
	return r.managedOAuthClientWriteResult(ctx, result, tenantID, clientID)
}

func (r *Repository) managedOAuthClientWriteResult(ctx context.Context, result *gorm.DB, tenantID int64, clientID string) error {
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Table("system.oauth_clients").
		Where("owner_scope = 'tenant' AND owner_tenant_id = ? AND client_id = ?", tenantID, clientID).
		Count(&count).Error; err != nil {
		return wrapRepositoryError(err)
	}
	if count == 0 {
		return commonapi.ErrNotFound
	}
	return ErrOAuthClientVersionConflict
}

func (r *Repository) CancelPendingAuthorizationRequestsByClient(ctx context.Context, clientID string, completedAt time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Table("system.oauth_authorization_requests").
		Where("client_id = ? AND status = 'pending' AND expires_at >= ?", clientID, completedAt).
		Updates(map[string]any{"status": "cancelled", "completed_at": completedAt})
	return result.RowsAffected, wrapRepositoryError(result.Error)
}

func (r *Repository) RevokeActiveTokenFamiliesByClient(ctx context.Context, clientID string, revokedAt time.Time, reason string) (int64, error) {
	if strings.TrimSpace(reason) == "" {
		return 0, commonapi.ErrBadRequest
	}
	result := r.db.WithContext(ctx).Table("system.refresh_token_families").
		Where("client_id = ? AND revoked_at IS NULL", clientID).
		Updates(map[string]any{"revoked_at": revokedAt, "revoked_reason": reason})
	return result.RowsAffected, wrapRepositoryError(result.Error)
}

func mapManagedOAuthClientRow(row managedOAuthClientRow) ManagedOAuthClient {
	return ManagedOAuthClient{
		ClientID: row.ClientID, DisplayName: row.DisplayName,
		RedirectURIs:     append([]string(nil), row.RedirectURIs...),
		GrantTypes:       append([]string(nil), row.GrantTypes...),
		ResponseTypes:    append([]string(nil), row.ResponseTypes...),
		AllowedScopes:    append([]string(nil), row.AllowedScopes...),
		AllowedAudiences: append([]string(nil), row.AllowedAudiences...),
		TokenAuthMethod:  row.TokenAuthMethod, Status: row.Status, Version: row.Version,
		CreatedByPrincipal: row.CreatedByPrincipalID,
		CreatedAt:          row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
}
