package iam

import (
	"context"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ManagedUser struct {
	ID                   int64
	Status               PrincipalStatus
	AuthorizationVersion int64
	DisplayName          string
	PrimaryEmail         *string
	Locale               *string
	AccountID            *int64
	Username             *string
	LocalAccountStatus   *LocalAccountStatus
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ManagedTenantMembership struct {
	ID                   int64
	TenantID             int64
	PrincipalID          int64
	PrincipalType        PrincipalType
	PrincipalStatus      PrincipalStatus
	DisplayName          string
	Username             *string
	Status               TenantMembershipStatus
	SourceType           TenantMembershipSource
	SourceRef            *string
	JoinedAt             time.Time
	ExpiresAt            *time.Time
	EndedAt              *time.Time
	CreatedByPrincipalID *int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AuditQuery struct {
	TenantID    *int64
	StartTime   *time.Time
	EndTime     *time.Time
	EventName   string
	Result      string
	RiskLevel   string
	ModuleName  string
	PrincipalID *int64
	RequestID   string
}

type AuditSummary struct {
	Total     int64 `json:"total"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Denied    int64 `json:"denied"`
	Ignored   int64 `json:"ignored"`
	HighRisk  int64 `json:"high_risk"`
}

type AuditTrendPoint struct {
	Date      time.Time `json:"date"`
	Total     int64     `json:"total"`
	Succeeded int64     `json:"succeeded"`
	Failed    int64     `json:"failed"`
	Denied    int64     `json:"denied"`
}

func (r *Repository) ListManagedTenants(
	ctx context.Context,
	page int,
	pageSize int,
	search string,
	status *TenantStatus,
) ([]Tenant, int64, error) {
	query := r.db.WithContext(ctx).Model(&Tenant{})
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", pattern, pattern)
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var tenants []Tenant
	if err := query.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tenants).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	return tenants, total, nil
}

func (r *Repository) LockTenantForUpdate(ctx context.Context, tenantID int64) (*Tenant, error) {
	var tenant Tenant
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&tenant, tenantID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &tenant, nil
}

func (r *Repository) UpdateTenantDetails(
	ctx context.Context,
	tenantID int64,
	name string,
	description string,
) error {
	result := r.db.WithContext(ctx).Model(&Tenant{}).Where("id = ?", tenantID).Updates(map[string]any{
		"name":        name,
		"description": description,
	})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *Repository) UpdateTenantStatus(ctx context.Context, tenantID int64, status TenantStatus) error {
	result := r.db.WithContext(ctx).Model(&Tenant{}).Where("id = ?", tenantID).Update("status", status)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *Repository) LockTenantPrincipalIDs(ctx context.Context, tenantID int64) ([]int64, error) {
	var principalIDs []int64
	err := r.db.WithContext(ctx).Raw(`
		SELECT principal.id
		FROM system.principals principal
		WHERE EXISTS (
			SELECT 1
			FROM system.tenant_memberships membership
			WHERE membership.tenant_id = ?
			  AND membership.principal_id = principal.id
		)
		ORDER BY principal.id
		FOR UPDATE OF principal
	`, tenantID).Scan(&principalIDs).Error
	return principalIDs, wrapRepositoryError(err)
}

func (r *Repository) ListManagedUsers(
	ctx context.Context,
	page int,
	pageSize int,
	search string,
	status *PrincipalStatus,
) ([]ManagedUser, int64, error) {
	base := r.db.WithContext(ctx).
		Table("system.principals principal").
		Joins("JOIN system.users user_profile ON user_profile.id = principal.id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = principal.id").
		Where("principal.principal_type = ?", PrincipalTypeUser)
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		base = base.Where(
			"user_profile.display_name ILIKE ? OR user_profile.primary_email ILIKE ? OR account.username ILIKE ?",
			pattern, pattern, pattern,
		)
	}
	if status != nil {
		base = base.Where("principal.status = ?", *status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var users []ManagedUser
	err := base.Select(`
		principal.id,
		principal.status,
		principal.authorization_version,
		user_profile.display_name,
		user_profile.primary_email,
		user_profile.locale,
		account.id AS account_id,
		account.username,
		account.status AS local_account_status,
		user_profile.created_at,
		user_profile.updated_at
	`).Order("principal.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&users).Error
	if err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	return users, total, nil
}

func (r *Repository) GetManagedUser(ctx context.Context, userID int64) (*ManagedUser, error) {
	var user ManagedUser
	err := r.db.WithContext(ctx).
		Table("system.principals principal").
		Select(`
			principal.id,
			principal.status,
			principal.authorization_version,
			user_profile.display_name,
			user_profile.primary_email,
			user_profile.locale,
			account.id AS account_id,
			account.username,
			account.status AS local_account_status,
			user_profile.created_at,
			user_profile.updated_at
		`).
		Joins("JOIN system.users user_profile ON user_profile.id = principal.id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = principal.id").
		Where("principal.id = ? AND principal.principal_type = ?", userID, PrincipalTypeUser).
		Take(&user).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &user, nil
}

func (r *Repository) UpdateUserProfile(
	ctx context.Context,
	userID int64,
	displayName string,
	primaryEmail *string,
	locale *string,
) error {
	result := r.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"display_name":  displayName,
		"primary_email": primaryEmail,
		"locale":        locale,
	})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrNotFound
	}
	return nil
}

func (r *Repository) UpdatePrincipalStatus(
	ctx context.Context,
	principalID int64,
	status PrincipalStatus,
	deactivatedAt *time.Time,
	changeRequestID *int64,
) (int64, error) {
	updates := map[string]any{
		"status":         status,
		"deactivated_at": deactivatedAt,
	}
	if changeRequestID != nil {
		updates["status_change_request_id"] = *changeRequestID
	}
	result := r.db.WithContext(ctx).Model(&Principal{}).Where("id = ?", principalID).Updates(updates)
	if result.Error != nil {
		return 0, wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return 0, commonapi.ErrNotFound
	}
	principal, err := r.GetPrincipal(ctx, principalID)
	if err != nil {
		return 0, err
	}
	return principal.AuthorizationVersion, nil
}

func (r *Repository) ListManagedTenantMemberships(
	ctx context.Context,
	tenantID int64,
	page int,
	pageSize int,
	search string,
	status *TenantMembershipStatus,
) ([]ManagedTenantMembership, int64, error) {
	base := r.db.WithContext(ctx).
		Table("system.tenant_memberships membership").
		Joins("JOIN system.principals principal ON principal.id = membership.principal_id").
		Joins("LEFT JOIN system.users user_profile ON user_profile.id = principal.id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = principal.id").
		Joins("LEFT JOIN system.service_principals service_principal ON service_principal.id = principal.id").
		Where("membership.tenant_id = ?", tenantID)
	if normalized := strings.TrimSpace(search); normalized != "" {
		pattern := "%" + normalized + "%"
		base = base.Where(
			"user_profile.display_name ILIKE ? OR account.username ILIKE ? OR service_principal.name ILIKE ?",
			pattern, pattern, pattern,
		)
	}
	if status != nil {
		base = base.Where("membership.status = ?", *status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var memberships []ManagedTenantMembership
	err := base.Select(`
		membership.id,
		membership.tenant_id,
		membership.principal_id,
		principal.principal_type,
		principal.status AS principal_status,
		COALESCE(user_profile.display_name, service_principal.name) AS display_name,
		account.username,
		membership.status,
		membership.source_type,
		membership.source_ref,
		membership.joined_at,
		membership.expires_at,
		membership.ended_at,
		membership.created_by_principal_id,
		membership.created_at,
		membership.updated_at
	`).Order("membership.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&memberships).Error
	if err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	return memberships, total, nil
}

func (r *Repository) GetManagedTenantMembership(
	ctx context.Context,
	tenantID int64,
	membershipID int64,
) (*ManagedTenantMembership, error) {
	var membership ManagedTenantMembership
	err := r.db.WithContext(ctx).
		Table("system.tenant_memberships membership").
		Select(`
			membership.id,
			membership.tenant_id,
			membership.principal_id,
			principal.principal_type,
			principal.status AS principal_status,
			COALESCE(user_profile.display_name, service_principal.name) AS display_name,
			account.username,
			membership.status,
			membership.source_type,
			membership.source_ref,
			membership.joined_at,
			membership.expires_at,
			membership.ended_at,
			membership.created_by_principal_id,
			membership.created_at,
			membership.updated_at
		`).
		Joins("JOIN system.principals principal ON principal.id = membership.principal_id").
		Joins("LEFT JOIN system.users user_profile ON user_profile.id = principal.id").
		Joins("LEFT JOIN system.local_accounts account ON account.user_id = principal.id").
		Joins("LEFT JOIN system.service_principals service_principal ON service_principal.id = principal.id").
		Where("membership.tenant_id = ? AND membership.id = ?", tenantID, membershipID).
		Take(&membership).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &membership, nil
}

func (r *Repository) ListAuditLogs(
	ctx context.Context,
	query AuditQuery,
	page int,
	pageSize int,
) ([]AuditLog, int64, error) {
	base := applyAuditQuery(r.db.WithContext(ctx).Model(&AuditLog{}), query)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	var logs []AuditLog
	err := base.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs).Error
	if err != nil {
		return nil, 0, wrapRepositoryError(err)
	}
	return logs, total, nil
}

func (r *Repository) GetAuditLog(ctx context.Context, auditID int64, tenantID *int64) (*AuditLog, error) {
	query := r.db.WithContext(ctx).Model(&AuditLog{}).Where("id = ?", auditID)
	if tenantID != nil {
		query = query.Where("tenant_id = ?", *tenantID)
	}
	var log AuditLog
	if err := query.Take(&log).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &log, nil
}

func (r *Repository) GetAuditSummary(ctx context.Context, query AuditQuery) (*AuditSummary, error) {
	var summary AuditSummary
	err := applyAuditQuery(r.db.WithContext(ctx).Model(&AuditLog{}), query).Select(`
		count(*) AS total,
		count(*) FILTER (WHERE result = 'succeeded') AS succeeded,
		count(*) FILTER (WHERE result = 'failed') AS failed,
		count(*) FILTER (WHERE result = 'denied') AS denied,
		count(*) FILTER (WHERE result = 'ignored') AS ignored,
		count(*) FILTER (WHERE risk_level IN ('high', 'critical')) AS high_risk
	`).Scan(&summary).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &summary, nil
}

func (r *Repository) GetAuditTrends(ctx context.Context, query AuditQuery) ([]AuditTrendPoint, error) {
	var points []AuditTrendPoint
	err := applyAuditQuery(r.db.WithContext(ctx).Model(&AuditLog{}), query).Select(`
		date_trunc('day', created_at) AS date,
		count(*) AS total,
		count(*) FILTER (WHERE result = 'succeeded') AS succeeded,
		count(*) FILTER (WHERE result = 'failed') AS failed,
		count(*) FILTER (WHERE result = 'denied') AS denied
	`).Group("date_trunc('day', created_at)").Order("date ASC").Scan(&points).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return points, nil
}

func applyAuditQuery(query *gorm.DB, filter AuditQuery) *gorm.DB {
	if filter.TenantID != nil {
		query = query.Where("tenant_id = ?", *filter.TenantID)
	}
	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("created_at < ?", *filter.EndTime)
	}
	if filter.EventName != "" {
		query = query.Where("event_name = ?", filter.EventName)
	}
	if filter.Result != "" {
		query = query.Where("result = ?", filter.Result)
	}
	if filter.RiskLevel != "" {
		query = query.Where("risk_level = ?", filter.RiskLevel)
	}
	if filter.ModuleName != "" {
		query = query.Where("module_name = ?", filter.ModuleName)
	}
	if filter.PrincipalID != nil {
		query = query.Where("principal_id = ?", *filter.PrincipalID)
	}
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	return query
}
