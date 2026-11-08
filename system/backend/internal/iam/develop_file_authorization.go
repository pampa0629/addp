package iam

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDevelopFileDependencyUnavailable = errors.New("Develop file authorization dependency unavailable")

type DevelopFileExecutionReader interface {
	ReadFileExecution(context.Context, uint, string) (*client.DevelopFileExecutionFacts, error)
}

type DevelopFileAuthorizationService struct {
	repository *Repository
	owner      DevelopFileExecutionReader
	prepare    DevelopFileReadPreparer
}

func NewDevelopFileAuthorizationService(repository *Repository, owner DevelopFileExecutionReader, prepare DevelopFileReadPreparer) *DevelopFileAuthorizationService {
	return &DevelopFileAuthorizationService{repository: repository, owner: owner, prepare: prepare}
}

// SetOwnerReader is composition-root wiring only, never caller input.
func (s *DevelopFileAuthorizationService) SetOwnerReader(owner DevelopFileExecutionReader) {
	s.owner = owner
}

type IssueDevelopFileAuthorizationInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	IntentDigest      string
	Audit             AuditMetadata
}

type CheckDevelopFileInput struct {
	ServiceAccessToken string
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	TenantID           int64
	ExpectedAttempt    int
	ScopeDigest        string
	LeaseToken         uuid.UUID
	Source             *plugin.EngineCatalogPath
	Audit              AuditMetadata
}

func (s *DevelopFileAuthorizationService) Issue(ctx context.Context, input IssueDevelopFileAuthorizationInput) (*client.IssuedDevelopFileAuthorization, error) {
	if s == nil || s.repository == nil || input.ExecutionID == uuid.Nil || !validDevelopDigest(input.IntentDigest) {
		return nil, commonapi.ErrBadRequest
	}
	if s.owner == nil || s.prepare == nil {
		return nil, ErrDevelopFileDependencyUnavailable
	}
	identity, _, err := s.repository.developFileUser(ctx, input.SourceAccessToken, false)
	if err != nil {
		return nil, err
	}
	facts, err := s.readOwner(ctx, identity, input.ExecutionID)
	if err != nil {
		return nil, err
	}
	digest, _ := facts.Intent.Digest()
	if digest != input.IntentDigest || facts.AdmissionState == "rejected" {
		return nil, ErrExecutionAuthorizationConflict
	}
	prepared, err := s.prepare(ctx, identity, facts.Intent)
	if err != nil {
		return nil, err
	}
	if prepared == nil || prepared.Verify == nil || prepared.Scope.ValidateAgainstIntent(facts.Intent) != nil {
		return nil, ErrDevelopFileDependencyUnavailable
	}
	var result *client.IssuedDevelopFileAuthorization
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		row, user, err := tx.developFileUser(ctx, input.SourceAccessToken, true)
		if err != nil {
			return err
		}
		if !sameDevelopActor(row, identity) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		row.ExecutionID = input.ExecutionID
		row.DevelopFileScope = prepared.Scope.Clone()
		var prior ExecutionAuthorization
		found := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("audience='develop' AND execution_id=?", input.ExecutionID).Take(&prior)
		if found.Error == nil {
			if !sameDevelopActor(&prior, row) || prior.DevelopFileScope == nil || !reflect.DeepEqual(prior.DevelopFileScope, row.DevelopFileScope) || prior.SealedAt == nil || prior.RevokedAt != nil {
				return ErrExecutionAuthorizationConflict
			}
			row = &prior
		} else if !errors.Is(found.Error, gorm.ErrRecordNotFound) {
			return wrapRepositoryError(found.Error)
		}
		if facts.AuthorizationID != "" && facts.AuthorizationID != strconv.FormatInt(row.ID, 10) {
			return ErrExecutionAuthorizationConflict
		}
		if _, err := tx.developFileExecution(ctx, row, facts, 0, uuid.Nil, true); err != nil {
			return err
		}
		observed, err := prepared.Verify(ctx, tx.db, row, nil)
		if err != nil {
			return err
		}
		now, err := tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if observed == nil || observed.ObservedAt.IsZero() || !user.Token.ExpiresAt.After(now) {
			return ErrExecutionAuthorizationUnavailable
		}
		if row.ID == 0 {
			row.CreatedAt = now
			row.ExpiresAt = now.Add(defaultExecutionAuthorizationTTL)
			accesses := developFileAccessRows(row.DevelopFileScope)
			if err := tx.CreateExecutionAuthorization(ctx, row, accesses); err != nil {
				return err
			}
			if err := writeDevelopFileAudit(ctx, tx, row, input.Audit, "iam.execution_authorization.issued", 0); err != nil {
				return err
			}
		}
		// Audit/lock waits cannot turn a stale token, role or source rule into
		// an issuance. Recheck the original credential and common pending row.
		current, currentUser, err := tx.developFileUser(ctx, input.SourceAccessToken, true)
		if err != nil {
			return err
		}
		if !sameDevelopActor(current, row) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		if _, err := tx.developFileExecution(ctx, row, facts, 0, uuid.Nil, true); err != nil {
			return err
		}
		observed, err = prepared.Verify(ctx, tx.db, row, nil)
		if err != nil {
			return err
		}
		now, err = tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if observed == nil || observed.ObservedAt.IsZero() || !row.ExpiresAt.After(now) || !currentUser.Token.ExpiresAt.After(now) || (observed.ExpiresAt != nil && !observed.ExpiresAt.After(now)) {
			return ErrExecutionAuthorizationUnavailable
		}
		result = developFileIssued(row)
		return result.Validate()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *DevelopFileAuthorizationService) Check(ctx context.Context, input CheckDevelopFileInput) (*client.DevelopFileAdmissionObservation, *client.DevelopFileSourceAccessObservation, error) {
	if s == nil || s.repository == nil || input.AuthorizationID <= 0 || input.TenantID <= 0 || input.ExecutionID == uuid.Nil || input.ExpectedAttempt <= 0 || !validDevelopDigest(input.ScopeDigest) || (input.Source != nil && input.LeaseToken == uuid.Nil) {
		return nil, nil, commonapi.ErrBadRequest
	}
	if s.owner == nil || s.prepare == nil {
		return nil, nil, ErrDevelopFileDependencyUnavailable
	}
	service, err := s.repository.developFileService(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	pre, err := s.repository.GetExecutionAuthorization(ctx, input.AuthorizationID)
	if err != nil {
		return nil, nil, ErrExecutionAuthorizationUnavailable
	}
	if err := matchDevelopFileAuthorization(pre, input); err != nil {
		return nil, nil, err
	}
	if input.Source != nil {
		target := *input.Source
		target.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
		input.Source = &target
		found := false
		for _, source := range pre.DevelopFileScope.Sources {
			if reflect.DeepEqual(source.Target, target) {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, ErrExecutionAuthorizationPermissionDenied
		}
	}
	// No publication-approval consumer exists in this read-only slice. Do not
	// manufacture ready, awaiting approval, or a reusable write credential.
	if pre.DevelopFileScope.Target != nil {
		return nil, nil, ErrDevelopFileDependencyUnavailable
	}
	facts, err := s.readOwner(ctx, pre, input.ExecutionID)
	if err != nil {
		return nil, nil, err
	}
	if facts.AuthorizationID != strconv.FormatInt(pre.ID, 10) || facts.ScopeDigest != input.ScopeDigest || !reflect.DeepEqual(facts.BoundScope, pre.DevelopFileScope) || facts.AdmissionState == "rejected" {
		return nil, nil, ErrExecutionAuthorizationUnavailable
	}
	if input.Source != nil && facts.AdmissionState != "ready" {
		return nil, nil, ErrExecutionAuthorizationUnavailable
	}
	prepared, err := s.prepare(ctx, pre, facts.Intent)
	if err != nil {
		return nil, nil, err
	}
	if prepared == nil || prepared.Verify == nil || !reflect.DeepEqual(&prepared.Scope, pre.DevelopFileScope) {
		return nil, nil, ErrDevelopFileDependencyUnavailable
	}
	var admission *client.DevelopFileAdmissionObservation
	var access *client.DevelopFileSourceAccessObservation
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		row, err := tx.GetExecutionAuthorization(ctx, input.AuthorizationID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if err := matchDevelopFileAuthorization(row, input); err != nil {
			return err
		}
		principal, membership, _, err := lockAndValidateExecutionSource(ctx, tx, row)
		if err != nil {
			return err
		}
		row, err = tx.LockExecutionAuthorization(ctx, row.ID)
		if err != nil {
			return err
		}
		if err := matchDevelopFileAuthorization(row, input); err != nil {
			return err
		}
		var observation *DevelopFileReadObservation
		var expires, observedAt, businessDeadline time.Time
		check := func() error {
			currentService, err := tx.developFileService(ctx, input)
			if err != nil {
				return err
			}
			if currentService.Principal.ID != service.Principal.ID {
				return ErrExecutionAuthorizationPermissionDenied
			}
			window, err := tx.developFileExecution(ctx, row, facts, input.ExpectedAttempt, input.LeaseToken, input.Source == nil)
			if err != nil {
				return err
			}
			observation, err = prepared.Verify(ctx, tx.db, row, input.Source)
			if err != nil {
				return err
			}
			if observation == nil || observation.ObservedAt.IsZero() {
				return ErrExecutionAuthorizationUnavailable
			}
			observedAt, err = tx.internalTaskDatabaseTime(ctx)
			if err != nil {
				return err
			}
			permissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant, &membership.TenantID, &membership.ID, observedAt)
			if err != nil {
				return err
			}
			if !developFilePermissions(permissions) {
				return ErrExecutionAuthorizationPermissionDenied
			}
			expires = row.ExpiresAt
			for _, bound := range []*time.Time{membership.ExpiresAt, observation.ExpiresAt} {
				if bound != nil && !bound.IsZero() && bound.Before(expires) {
					expires = *bound
				}
			}
			for _, key := range []string{developExecutionPermission, executionEffectPermissions["read"]} {
				var latest *time.Time
				unlimited := false
				for _, p := range permissions {
					if p.ScopeType == "tenant" && p.PermissionKey == key {
						if p.ValidUntil == nil {
							unlimited = true
							break
						}
						if latest == nil || p.ValidUntil.After(*latest) {
							v := *p.ValidUntil
							latest = &v
						}
					}
				}
				if !unlimited && latest != nil && latest.Before(expires) {
					expires = *latest
				}
			}
			if input.Source != nil {
				businessDeadline = window.BudgetDeadline
				if expires.Before(businessDeadline) {
					businessDeadline = expires
				}
				if row.DevelopFileBusinessDeadline == nil {
					if !businessDeadline.After(observedAt) {
						return ErrExecutionAuthorizationUnavailable
					}
					result := tx.db.WithContext(ctx).Model(&ExecutionAuthorization{}).
						Where("id=? AND develop_file_business_deadline IS NULL", row.ID).
						UpdateColumn("develop_file_business_deadline", businessDeadline)
					if result.Error != nil || result.RowsAffected != 1 {
						return ErrExecutionAuthorizationUnavailable
					}
					fixed := businessDeadline
					row.DevelopFileBusinessDeadline = &fixed
				}
				businessDeadline = *row.DevelopFileBusinessDeadline
				if businessDeadline.After(window.BudgetDeadline) {
					return ErrExecutionAuthorizationUnavailable
				}
				for _, bound := range []time.Time{businessDeadline, window.LeaseExpiresAt} {
					if bound.Before(expires) {
						expires = bound
					}
				}
			}
			if currentService.Token.ExpiresAt.Before(expires) {
				expires = currentService.Token.ExpiresAt
			}
			if !expires.After(observedAt) {
				return ErrExecutionAuthorizationUnavailable
			}
			return nil
		}
		if err := check(); err != nil {
			return err
		}
		if err := writeDevelopFileAudit(ctx, tx, row, input.Audit, "iam.execution_authorization.consumed", input.ExpectedAttempt); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		if input.Source == nil {
			admission = &client.DevelopFileAdmissionObservation{AuthorizationID: strconv.FormatInt(row.ID, 10), ExecutionID: row.ExecutionID.String(), ExpectedAttempt: input.ExpectedAttempt, ScopeDigest: input.ScopeDigest, State: "ready", ObservedAt: observedAt, ExpiresAt: expires}
		} else {
			access = &client.DevelopFileSourceAccessObservation{AuthorizationID: strconv.FormatInt(row.ID, 10), ExecutionID: row.ExecutionID.String(), Attempt: input.ExpectedAttempt, ScopeDigest: input.ScopeDigest, Source: *input.Source, ObservedAt: observedAt, ExpiresAt: expires, BusinessDeadline: businessDeadline, Engine: observation.Engine}
			if access.Engine == nil {
				return ErrExecutionAuthorizationUnavailable
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return admission, access, nil
}

func (s *DevelopFileAuthorizationService) readOwner(ctx context.Context, auth *ExecutionAuthorization, id uuid.UUID) (*client.DevelopFileExecutionFacts, error) {
	facts, err := s.owner.ReadFileExecution(ctx, uint(auth.TenantID), id.String())
	if err != nil || facts.Validate() != nil {
		return nil, ErrDevelopFileDependencyUnavailable
	}
	if facts.ExecutionID != id.String() || facts.TenantID != strconv.FormatInt(auth.TenantID, 10) || facts.Actor.PrincipalID != strconv.FormatInt(auth.ActorPrincipalID, 10) || facts.Actor.TenantMembershipID != strconv.FormatInt(auth.TenantMembershipID, 10) || facts.Actor.AuthorizationVersion != strconv.FormatInt(auth.IssuedAuthorizationVersion, 10) {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	return facts.Clone(), nil
}

func (r *Repository) developFileUser(ctx context.Context, credential string, lock bool) (*ExecutionAuthorization, *authorization.AuthContext, error) {
	snapshot, err := resolveDelegationSourceAccessTokenSnapshot(ctx, r, credential)
	if err != nil {
		return nil, nil, err
	}
	if lock {
		principal, err := r.LockPrincipal(ctx, snapshot.FamilyPrincipalID)
		if err != nil {
			return nil, nil, err
		}
		family, err := r.LockRefreshTokenFamily(ctx, snapshot.CredentialFamilyID)
		if err != nil {
			return nil, nil, err
		}
		token, err := r.LockAccessToken(ctx, snapshot.CredentialID)
		if err != nil {
			return nil, nil, err
		}
		snapshot, err = resolveDelegationSourceAccessTokenSnapshot(ctx, r, credential)
		if err != nil {
			return nil, nil, err
		}
		if !sameLockedUserAccessTokenSnapshot(snapshot, principal, family, token) {
			return nil, nil, commonapi.ErrUnauthorized
		}
	}
	s, err := NewAuthContextService(r)
	if err != nil {
		return nil, nil, err
	}
	current, err := s.ResolveUserAccessToken(ctx, credential)
	if err != nil {
		return nil, nil, err
	}
	if !developFileUserCredential(current) || snapshot.TenantID == nil || snapshot.TenantMembershipID == nil {
		return nil, nil, ErrExecutionAuthorizationPermissionDenied
	}
	return &ExecutionAuthorization{ActorPrincipalID: snapshot.FamilyPrincipalID, TenantID: *snapshot.TenantID, TenantMembershipID: *snapshot.TenantMembershipID, IssuedAuthorizationVersion: snapshot.FamilyAuthorizationVersion, SourceType: "user", Audience: "develop"}, current, nil
}

func developFileUserCredential(auth *authorization.AuthContext) bool {
	if auth == nil || auth.Principal.Type != "user" || auth.Context.Type != "tenant" || auth.Delegation != nil || len(auth.Client.Audiences) != 1 || auth.Client.Audiences[0] != "addp.api" || !authorization.HasContextPermissions(*auth, developExecutionPermission, executionEffectPermissions["read"]) {
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

func (r *Repository) developFileService(ctx context.Context, input CheckDevelopFileInput) (*authorization.AuthContext, error) {
	s, err := NewAuthContextService(r)
	if err != nil {
		return nil, err
	}
	current, err := s.ResolveAccessToken(ctx, input.ServiceAccessToken)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Principal.Type != "service_principal" || current.Context.Type != "tenant" || current.Context.TenantID == nil || *current.Context.TenantID != strconv.FormatInt(input.TenantID, 10) || current.Token.Type != "service_access_token" || current.Client.ClientID == nil || *current.Client.ClientID != "addp-develop" || current.Delegation != nil || len(current.Client.Audiences) != 1 || current.Client.Audiences[0] != "addp.api" || current.Client.ScopeMode != "unrestricted" || len(current.Client.Scopes) != 0 || !authorization.HasContextPermissions(*current, "system.execution_authorization.execute") {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	if input.Audit.PrincipalID == nil || strconv.FormatInt(*input.Audit.PrincipalID, 10) != current.Principal.ID || input.Audit.TenantID == nil || *input.Audit.TenantID != input.TenantID || input.Audit.PrincipalType == nil || *input.Audit.PrincipalType != PrincipalTypeServicePrincipal || input.Audit.ContextType == nil || *input.Audit.ContextType != ContextTypeTenant {
		return nil, commonapi.ErrBadRequest
	}
	return current, nil
}

// The fixed budget and renewable claim window have distinct meanings.
type developFileExecutionWindow struct {
	BudgetDeadline time.Time
	LeaseExpiresAt time.Time
}

func (r *Repository) developFileExecution(ctx context.Context, auth *ExecutionAuthorization, facts *client.DevelopFileExecutionFacts, attempt int, lease uuid.UUID, pending bool) (developFileExecutionWindow, error) {
	var row struct {
		Status                   string
		Attempt                  int
		StartedAt                *time.Time
		LeaseToken               *string
		LeaseOwner               *string
		LeaseExpiresAt           *time.Time
		ExecutionAuthorizationID *int64
		AuthorizationExpiresAt   *time.Time
		ExecutionConfig          json.RawMessage
	}
	query := r.db.WithContext(ctx).Table("common.task_executions e").Select("e.status,e.attempt,e.started_at,e.lease_token,e.lease_owner,e.lease_expires_at,e.execution_authorization_id,e.authorization_expires_at,e.execution_config").Where("e.execution_id=? AND e.tenant_id=? AND e.module='develop' AND e.source='develop' AND e.task_type='workflow' AND e.execution_boundary='bounded' AND e.trigger_type='manual' AND e.parent_execution_id IS NULL AND e.retry_of_execution_id IS NULL AND e.max_attempts=1 AND e.triggered_by=e.actor_principal_id AND e.actor_principal_id=? AND e.actor_tenant_membership_id=? AND e.issued_authorization_version=?", facts.ExecutionID, auth.TenantID, auth.ActorPrincipalID, auth.TenantMembershipID, auth.IssuedAuthorizationVersion)
	if err := query.Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "e"}}).Take(&row).Error; err != nil {
		return developFileExecutionWindow{}, ErrExecutionAuthorizationUnavailable
	}
	intent, err := execution.NewDevelopFileIntent(row.ExecutionConfig, facts.Intent.ExpectedAttempt, facts.Intent.TimeoutSeconds, facts.Intent.Sources, facts.Intent.Target)
	if err != nil || !reflect.DeepEqual(intent, &facts.Intent) || facts.Intent.ExpectedAttempt != 1 {
		return developFileExecutionWindow{}, ErrExecutionAuthorizationUnavailable
	}
	now, err := r.internalTaskDatabaseTime(ctx)
	if err != nil {
		return developFileExecutionWindow{}, err
	}
	bound := row.ExecutionAuthorizationID != nil && auth.ID > 0 && *row.ExecutionAuthorizationID == auth.ID && row.AuthorizationExpiresAt != nil && row.AuthorizationExpiresAt.Equal(auth.ExpiresAt)
	if pending {
		if row.Status != "pending" || row.Attempt != 0 || row.StartedAt != nil || row.LeaseToken != nil || row.LeaseOwner != nil || row.LeaseExpiresAt != nil || (row.ExecutionAuthorizationID != nil || row.AuthorizationExpiresAt != nil) && !bound || attempt > 0 && !bound {
			return developFileExecutionWindow{}, ErrExecutionAuthorizationUnavailable
		}
		return developFileExecutionWindow{}, nil
	}
	if !bound || row.Status != "running" || attempt != facts.Intent.ExpectedAttempt || row.Attempt != attempt || row.StartedAt == nil || row.LeaseToken == nil || *row.LeaseToken != lease.String() || row.LeaseOwner == nil || strings.TrimSpace(*row.LeaseOwner) == "" || row.LeaseExpiresAt == nil || !row.LeaseExpiresAt.After(now) {
		return developFileExecutionWindow{}, ErrExecutionAuthorizationUnavailable
	}
	deadline, err := auth.DevelopFileScope.Deadline(*row.StartedAt)
	if err != nil || !deadline.After(now) {
		return developFileExecutionWindow{}, ErrExecutionAuthorizationUnavailable
	}
	return developFileExecutionWindow{BudgetDeadline: deadline, LeaseExpiresAt: *row.LeaseExpiresAt}, nil
}

func sameDevelopActor(a, b *ExecutionAuthorization) bool {
	return a != nil && b != nil && a.Audience == "develop" && b.Audience == "develop" && a.SourceType == "user" && b.SourceType == "user" && a.TenantID == b.TenantID && a.ActorPrincipalID == b.ActorPrincipalID && a.TenantMembershipID == b.TenantMembershipID && a.IssuedAuthorizationVersion == b.IssuedAuthorizationVersion
}
func validDevelopDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func developFilePermissions(rows []RoleAssignmentPermissionProjection) bool {
	var tenant []RoleAssignmentPermissionProjection
	for _, row := range rows {
		if row.ScopeType == "tenant" {
			tenant = append(tenant, row)
		}
	}
	return containsAllExecutionPermissions(tenant, "develop", []string{"read"})
}
func matchDevelopFileAuthorization(row *ExecutionAuthorization, input CheckDevelopFileInput) error {
	if row == nil || row.Audience != "develop" || row.SourceType != "user" || row.DevelopFileScope.Validate() != nil || row.TenantID != input.TenantID || row.ExecutionID != input.ExecutionID || row.InternalTask != nil || row.SourceReadScope != nil || row.CleanupScope != nil || row.SealedAt == nil || row.RevokedAt != nil || row.DevelopFileScope.ExpectedAttempt != input.ExpectedAttempt {
		return ErrExecutionAuthorizationPermissionDenied
	}
	digest, _ := row.DevelopFileScope.Digest()
	if digest != input.ScopeDigest {
		return ErrExecutionAuthorizationPermissionDenied
	}
	return nil
}
func developFileAccessRows(scope *execution.DevelopBoundFileScope) []ExecutionAuthorizationEngineAccess {
	set := map[int64]bool{}
	for _, source := range scope.Sources {
		set[int64(source.Target.EngineID)] = true
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows := make([]ExecutionAuthorizationEngineAccess, len(ids))
	for i, id := range ids {
		rows[i] = ExecutionAuthorizationEngineAccess{EngineID: id, Effects: []string{"read"}}
	}
	return rows
}
func developFileIssued(row *ExecutionAuthorization) *client.IssuedDevelopFileAuthorization {
	digest, _ := row.DevelopFileScope.Digest()
	result := &client.IssuedDevelopFileAuthorization{Scope: *row.DevelopFileScope.Clone(), ScopeDigest: digest, Authorization: client.IssuedExecutionAuthorization{ID: strconv.FormatInt(row.ID, 10), ExecutionID: row.ExecutionID.String(), Audience: "develop", TenantID: strconv.FormatInt(row.TenantID, 10), ActorPrincipalID: strconv.FormatInt(row.ActorPrincipalID, 10), TenantMembershipID: strconv.FormatInt(row.TenantMembershipID, 10), IssuedAuthorizationVersion: strconv.FormatInt(row.IssuedAuthorizationVersion, 10), SourceType: "user", ExpiresAt: row.ExpiresAt}}
	for _, a := range developFileAccessRows(row.DevelopFileScope) {
		result.Authorization.Accesses = append(result.Authorization.Accesses, client.ExecutionEngineAccessScope{EngineID: strconv.FormatInt(a.EngineID, 10), Effects: []string{"read"}})
	}
	return result
}
func writeDevelopFileAudit(ctx context.Context, r *Repository, row *ExecutionAuthorization, metadata AuditMetadata, event string, attempt int) error {
	if event == "iam.execution_authorization.issued" {
		p, t, c := row.ActorPrincipalID, PrincipalTypeUser, ContextTypeTenant
		metadata.PrincipalID = &p
		metadata.PrincipalType = &t
		metadata.ContextType = &c
		metadata.TenantID = &row.TenantID
	}
	digest, _ := row.DevelopFileScope.Digest()
	return NewAuditWriter(r).Write(ctx, AuditEvent{Metadata: metadata, EventName: event, Result: AuditResultSucceeded, RiskLevel: AuditRiskLow, ModuleName: "system", EntityType: "execution_authorization", EntityID: strconv.FormatInt(row.ID, 10), Details: map[string]any{"audience": "develop", "execution_id": row.ExecutionID.String(), "scope_digest": digest, "attempt": attempt}})
}
