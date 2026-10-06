package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExecutionSourceReadVerifier is wired by System's composition root, not by an
// HTTP caller. It must use this transaction and the source-rule authority; it
// must not write source rules or return cached observations.
type ExecutionSourceReadVerifier func(context.Context, *gorm.DB, *ExecutionAuthorization) (time.Time, error)

type ManagerProfileAuthorizationService struct {
	repository *Repository
	verify     ExecutionSourceReadVerifier
}

func NewManagerProfileAuthorizationService(repository *Repository, verify ExecutionSourceReadVerifier) *ManagerProfileAuthorizationService {
	return &ManagerProfileAuthorizationService{repository: repository, verify: verify}
}

type IssueManagerProfileAuthorizationInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssuedManagerProfileAuthorization struct {
	ID              int64
	ExecutionID     uuid.UUID
	TenantID        int64
	SourceReadScope execution.ManagerProfileReadScope
	ExpiresAt       time.Time
}

type AuthorizeManagerProfileInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	SourceReadScope    execution.ManagerProfileReadScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

// Issue derives the source scope from the exact pending owner execution. No
// caller-supplied actor, tenant, configuration or target list is accepted.
func (s *ManagerProfileAuthorizationService) Issue(ctx context.Context, input IssueManagerProfileAuthorizationInput) (*IssuedManagerProfileAuthorization, error) {
	if s == nil || s.repository == nil || s.verify == nil || input.ExecutionID == uuid.Nil {
		return nil, commonapi.ErrBadRequest
	}
	ttl := input.ExpiresIn
	if ttl == 0 {
		ttl = defaultExecutionAuthorizationTTL
	}
	if ttl <= 0 || ttl > maximumExecutionAuthorizationTTL || ttl%time.Second != 0 {
		return nil, commonapi.ErrBadRequest
	}
	snapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, s.repository, input.SourceAccessToken)
	if err != nil {
		return nil, err
	}
	var result *IssuedManagerProfileAuthorization
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, snapshot.FamilyPrincipalID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		family, err := tx.LockRefreshTokenFamily(ctx, snapshot.CredentialFamilyID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		token, err := tx.LockAccessToken(ctx, snapshot.CredentialID)
		if err != nil {
			return hideTokenLookupError(err)
		}
		current, err := resolveDelegationSourceAccessTokenSnapshot(ctx, tx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if !sameLockedUserAccessTokenSnapshot(current, principal, family, token) ||
			current.FamilyContextType != ContextTypeTenant || current.TenantID == nil || current.TenantMembershipID == nil {
			return commonapi.ErrUnauthorized
		}
		// Resolve the ordinary credential here too: OAuth tool-only and preview
		// delegates cannot acquire an unrelated background execution capability.
		authService, err := NewAuthContextService(tx)
		if err != nil {
			return err
		}
		auth, err := authService.ResolveUserAccessToken(ctx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if !managerProfileUserCredential(auth) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		row := &ExecutionAuthorization{ActorPrincipalID: principal.ID, TenantID: *current.TenantID,
			TenantMembershipID: *current.TenantMembershipID, IssuedAuthorizationVersion: principal.AuthorizationVersion,
			SourceType: executionAuthorizationSourceUser, Audience: "manager", ExecutionID: input.ExecutionID,
			CreatedAt: current.DatabaseTime.UTC(), ExpiresAt: current.DatabaseTime.UTC().Add(ttl)}
		raw, err := tx.managerProfileExecutionConfig(ctx, row, 0, uuid.Nil, true)
		if err != nil {
			return err
		}
		row.SourceReadScope, err = managerProfileScope(raw)
		if err != nil {
			return err
		}
		observedAt, err := s.verify(ctx, tx.db, row)
		if err != nil {
			return err
		}
		if observedAt.IsZero() || !token.ExpiresAt.After(observedAt) || !row.ExpiresAt.After(observedAt) {
			return ErrExecutionAuthorizationUnavailable
		}
		accesses := []ExecutionAuthorizationEngineAccess{{EngineID: int64(row.SourceReadScope.ReadSet.Paths[0].EngineID), Effects: []string{"read"}}}
		if err := tx.CreateExecutionAuthorization(ctx, row, accesses); err != nil {
			return err
		}
		audit := input.Audit
		principalID, principalType, contextType := principal.ID, principal.PrincipalType, family.ContextType
		audit.PrincipalID, audit.PrincipalType, audit.ContextType, audit.TenantID = &principalID, &principalType, &contextType, &row.TenantID
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: audit,
			EventName: "iam.execution_authorization.issued", Result: AuditResultSucceeded, RiskLevel: AuditRiskLow,
			ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(row.ID, 10),
			Details: map[string]any{"audience": "manager", "execution_id": row.ExecutionID.String(), "source_read_scope": row.SourceReadScope}}); err != nil {
			return err
		}
		// A slow audit must not turn expired credentials, assignments or source
		// Grants into a newly committed execution capability.
		auth, err = authService.ResolveUserAccessToken(ctx, input.SourceAccessToken)
		if err != nil {
			return err
		}
		if !managerProfileUserCredential(auth) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		observedAt, err = s.verify(ctx, tx.db, row)
		if err != nil {
			return err
		}
		if observedAt.IsZero() || !token.ExpiresAt.After(observedAt) || !row.ExpiresAt.After(observedAt) {
			return ErrExecutionAuthorizationUnavailable
		}
		result = &IssuedManagerProfileAuthorization{ID: row.ID, ExecutionID: row.ExecutionID, TenantID: row.TenantID,
			SourceReadScope: *row.SourceReadScope.Clone(), ExpiresAt: row.ExpiresAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Authorize validates current source rules and the exact active claim. The
// result is for this owner operation, not general engine connection access.
func (s *ManagerProfileAuthorizationService) Authorize(ctx context.Context, input AuthorizeManagerProfileInput) (time.Time, error) {
	if s == nil || s.repository == nil || s.verify == nil || input.AuthorizationID <= 0 || input.ExecutionID == uuid.Nil ||
		input.Attempt <= 0 || input.LeaseToken == uuid.Nil || input.TenantID <= 0 || input.ServicePrincipalID <= 0 ||
		input.ServiceClientID != "addp-manager" || input.SourceReadScope.Validate() != nil {
		return time.Time{}, commonapi.ErrBadRequest
	}
	if input.Audit.PrincipalID == nil || *input.Audit.PrincipalID != input.ServicePrincipalID ||
		input.Audit.PrincipalType == nil || *input.Audit.PrincipalType != PrincipalTypeServicePrincipal ||
		input.Audit.ContextType == nil || *input.Audit.ContextType != ContextTypeTenant ||
		input.Audit.TenantID == nil || *input.Audit.TenantID != input.TenantID {
		return time.Time{}, commonapi.ErrBadRequest
	}
	var observedAt time.Time
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		snapshot, err := tx.GetExecutionAuthorization(ctx, input.AuthorizationID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if snapshot.Audience != "manager" || snapshot.TenantID != input.TenantID || snapshot.ExecutionID != input.ExecutionID ||
			snapshot.SourceType != executionAuthorizationSourceUser || !reflect.DeepEqual(snapshot.SourceReadScope, &input.SourceReadScope) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		principal, membership, _, err := lockAndValidateExecutionSource(ctx, tx, snapshot)
		if err != nil {
			return err
		}
		row, err := tx.LockExecutionAuthorization(ctx, snapshot.ID)
		if err != nil {
			return err
		}
		if row.SealedAt == nil || row.RevokedAt != nil || row.SourceReadScope == nil {
			return ErrExecutionAuthorizationUnavailable
		}
		raw, err := tx.managerProfileExecutionConfig(ctx, row, input.Attempt, input.LeaseToken, false)
		if err != nil {
			return err
		}
		currentScope, err := managerProfileScope(raw)
		if err != nil || !reflect.DeepEqual(currentScope, row.SourceReadScope) {
			return ErrExecutionAuthorizationUnavailable
		}
		if err := tx.currentManagerProfileConsumer(ctx, input); err != nil {
			return err
		}
		observedAt, err = s.verify(ctx, tx.db, row)
		if err != nil {
			return err
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, observedAt)
		if err != nil {
			return err
		}
		if !managerProfilePermissions(permissions) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if observedAt.IsZero() || !row.ExpiresAt.After(observedAt) || (membership.ExpiresAt != nil && !membership.ExpiresAt.After(observedAt)) {
			return ErrExecutionAuthorizationUnavailable
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: input.Audit,
			EventName: "iam.execution_authorization.consumed", Result: AuditResultSucceeded, RiskLevel: AuditRiskLow,
			ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(row.ID, 10),
			Details: map[string]any{"audience": "manager", "execution_id": row.ExecutionID.String(), "attempt": input.Attempt,
				"source_read_scope": row.SourceReadScope}}); err != nil {
			return err
		}
		// Auditing and lock waits may cross a deadline; never commit success then.
		observedAt, err = s.verify(ctx, tx.db, row)
		if err != nil {
			return err
		}
		permissions, err = tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant,
			&membership.TenantID, &membership.ID, observedAt)
		if err != nil {
			return err
		}
		if !managerProfilePermissions(permissions) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if err := tx.currentManagerProfileConsumer(ctx, input); err != nil {
			return err
		}
		if observedAt.IsZero() || !row.ExpiresAt.After(observedAt) || (membership.ExpiresAt != nil && !membership.ExpiresAt.After(observedAt)) {
			return ErrExecutionAuthorizationUnavailable
		}
		_, err = tx.managerProfileExecutionConfig(ctx, row, input.Attempt, input.LeaseToken, false)
		return err
	})
	if err != nil {
		return time.Time{}, err
	}
	return observedAt.UTC(), nil
}

func managerProfileUserCredential(auth *authorization.AuthContext) bool {
	if auth == nil || auth.Principal.Type != "user" || auth.Context.Type != "tenant" || auth.Delegation != nil ||
		len(auth.Client.Audiences) != 1 || auth.Client.Audiences[0] != "addp.api" ||
		!authorization.HasContextPermissions(*auth, "manager.data_profile.execute", "manager.data_item.read") {
		return false
	}
	if auth.Token.Type == "first_party_access_token" {
		return auth.Client.ScopeMode == "unrestricted" && len(auth.Client.Scopes) == 0
	}
	if auth.Token.Type == "oauth_access_token" && auth.Client.ScopeMode == "restricted" {
		for _, scope := range auth.Client.Scopes {
			if scope == "addp.api" {
				return true
			}
		}
	}
	return false
}

func managerProfilePermissions(rows []RoleAssignmentPermissionProjection) bool {
	permissions := make(map[string]bool)
	for _, row := range rows {
		if row.ScopeType == "tenant" {
			permissions[row.PermissionKey] = true
		}
	}
	return permissions["manager.data_profile.execute"] && permissions["manager.data_item.read"]
}

func managerProfileScope(raw json.RawMessage) (*execution.ManagerProfileReadScope, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	var version string
	var engineID uint
	if json.Unmarshal(fields["config_version"], &version) != nil || version != "data-profile-config/v6" ||
		json.Unmarshal(fields["engine_id"], &engineID) != nil || engineID == 0 {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	var readSet plugin.QueryReadSet
	decoder := json.NewDecoder(bytes.NewReader(fields["read_set"]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&readSet) != nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	scope, err := execution.NewManagerProfileReadScope(raw, &readSet)
	if err != nil || readSet.Paths[0].EngineID != engineID {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return scope, nil
}

func (r *Repository) managerProfileExecutionConfig(ctx context.Context, auth *ExecutionAuthorization, attempt int, lease uuid.UUID, pending bool) (json.RawMessage, error) {
	var row struct{ ExecutionConfig json.RawMessage }
	query := r.db.WithContext(ctx).Table("common.task_executions e").Select("e.execution_config").
		Where("e.execution_id = ? AND e.tenant_id = ? AND e.module = 'manager' AND e.source = 'manager' AND e.task_type = 'data_profiling' AND e.execution_boundary = 'bounded' AND e.trigger_type = 'manual'", auth.ExecutionID.String(), auth.TenantID).
		Where("e.actor_principal_id = ? AND e.actor_tenant_membership_id = ? AND e.issued_authorization_version = ?", auth.ActorPrincipalID, auth.TenantMembershipID, auth.IssuedAuthorizationVersion).
		Where("e.source_task_id IS NULL AND e.parent_execution_id IS NULL")
	if pending {
		query = query.Where("e.status = 'pending' AND e.attempt = 0 AND e.execution_authorization_id IS NULL AND e.authorization_expires_at IS NULL")
	} else {
		query = query.Where("e.status = 'running' AND e.attempt = ? AND e.lease_token = ? AND e.lease_expires_at > clock_timestamp() AND e.execution_authorization_id = ? AND e.authorization_expires_at = ? AND e.authorization_expires_at > clock_timestamp()", attempt, lease, auth.ID, auth.ExpiresAt)
	}
	result := query.Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "e"}}).Take(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	if result.Error != nil {
		return nil, wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return row.ExecutionConfig, nil
}

func (r *Repository) currentManagerProfileConsumer(ctx context.Context, input AuthorizeManagerProfileInput) error {
	var id int64
	result := r.db.WithContext(ctx).Raw(`SELECT member.id FROM system.oauth_clients client
		JOIN system.principals principal ON principal.id=client.service_principal_id
		JOIN system.service_principals service ON service.id=principal.id
		JOIN system.tenant_memberships member ON member.principal_id=principal.id
		WHERE client.client_id='addp-manager' AND client.status='active' AND principal.id=? AND principal.status='active'
		AND service.name='addp-manager' AND service.owner_scope='platform' AND member.tenant_id=? AND member.status='active'
		AND (member.expires_at IS NULL OR member.expires_at>clock_timestamp())
		FOR SHARE OF client,principal,service,member`, input.ServicePrincipalID, input.TenantID).Scan(&id)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrExecutionAuthorizationUnavailable
	}
	now, err := r.internalTaskDatabaseTime(ctx)
	if err != nil {
		return err
	}
	permissions, err := r.ListEffectiveRoleAssignmentPermissions(ctx, input.ServicePrincipalID, PrincipalTypeServicePrincipal,
		ContextTypeTenant, &input.TenantID, &id, now)
	if err != nil {
		return err
	}
	for _, row := range permissions {
		if row.ScopeType == "tenant" && row.PermissionKey == "system.execution_authorization.execute" {
			return nil
		}
	}
	return fmt.Errorf("%w: Manager Runtime execution permission is required", ErrExecutionAuthorizationPermissionDenied)
}
