package iam

import (
	"context"
	"fmt"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

func cloneInternalTaskScope(scope *execution.InternalTaskScope) *execution.InternalTaskScope {
	if scope == nil {
		return nil
	}
	copy := *scope
	return &copy
}

func normalizeUserExecutionAuthorizationRequest(input IssueExecutionAuthorizationInput) (string, []ExecutionEngineAccessScope, []string, time.Duration, error) {
	if input.InternalTask == nil {
		return normalizeExecutionAuthorizationRequest(input.Audience, input.ExecutionID, input.Accesses, input.ExpiresIn)
	}
	if input.InternalTask.Validate(input.Audience) != nil || len(input.Accesses) != 0 || input.ExecutionID == uuid.Nil {
		return "", nil, nil, 0, fmt.Errorf("%w: invalid internal task authorization boundary", commonapi.ErrBadRequest)
	}
	ttl := input.ExpiresIn
	if ttl == 0 {
		ttl = defaultExecutionAuthorizationTTL
	}
	if ttl <= 0 || ttl > maximumExecutionAuthorizationTTL || ttl%time.Second != 0 {
		return "", nil, nil, 0, fmt.Errorf("%w: invalid execution authorization expiry", commonapi.ErrBadRequest)
	}
	return input.Audience, nil, nil, ttl, nil
}

type AuthorizeInternalTaskInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	InternalTask       execution.InternalTaskScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

type AuthorizedInternalTask struct {
	AuthorizationID int64
	ExecutionID     uuid.UUID
	TenantID        int64
	Audience        string
	InternalTask    execution.InternalTaskScope
	Attempt         int
	ExpiresAt       time.Time
}

// AuthorizeInternalTask rechecks live IAM and lease facts. The owner must still
// validate resource policy, withdrawal and fencing immediately before activation.
func (s *ExecutionAuthorizationService) AuthorizeInternalTask(ctx context.Context, input AuthorizeInternalTaskInput) (*AuthorizedInternalTask, error) {
	if s == nil || s.repository == nil || input.AuthorizationID <= 0 || input.ExecutionID == uuid.Nil ||
		input.Attempt <= 0 || input.LeaseToken == uuid.Nil || input.ServicePrincipalID <= 0 || input.TenantID <= 0 ||
		input.InternalTask.Validate(execution.AudienceOntology) != nil {
		return nil, fmt.Errorf("%w: invalid internal task consumption", commonapi.ErrBadRequest)
	}
	if input.Audit.PrincipalID == nil || *input.Audit.PrincipalID != input.ServicePrincipalID ||
		input.Audit.PrincipalType == nil || *input.Audit.PrincipalType != PrincipalTypeServicePrincipal ||
		input.Audit.ContextType == nil || *input.Audit.ContextType != ContextTypeTenant ||
		input.Audit.TenantID == nil || *input.Audit.TenantID != input.TenantID {
		return nil, fmt.Errorf("%w: invalid internal task actor", commonapi.ErrBadRequest)
	}
	var result *AuthorizedInternalTask
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		snapshot, err := tx.GetExecutionAuthorization(ctx, input.AuthorizationID)
		if err != nil {
			return ErrExecutionAuthorizationUnavailable
		}
		if snapshot.InternalTask == nil || snapshot.TenantID != input.TenantID ||
			snapshot.ExecutionID != input.ExecutionID || snapshot.Audience != execution.AudienceOntology ||
			snapshot.SourceType != executionAuthorizationSourceUser || *snapshot.InternalTask != input.InternalTask ||
			input.ServiceClientID != executionAudienceClients[snapshot.Audience] {
			return ErrExecutionAuthorizationPermissionDenied
		}
		// Issuance and revocation lock the user first. Do not invert this order.
		principal, membership, _, err := lockAndValidateExecutionSource(ctx, tx, snapshot)
		if err != nil {
			return err
		}
		authorization, err := tx.LockExecutionAuthorization(ctx, snapshot.ID)
		if err != nil {
			return err
		}
		now, err := tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if authorization.SealedAt == nil || authorization.RevokedAt != nil || !authorization.ExpiresAt.After(now) ||
			(membership.ExpiresAt != nil && !membership.ExpiresAt.After(now)) {
			return ErrExecutionAuthorizationUnavailable
		}
		permissions, err := tx.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType,
			ContextTypeTenant, &membership.TenantID, &membership.ID, now)
		if err != nil {
			return err
		}
		if !containsAllExecutionPermissions(permissions, authorization.Audience, nil) {
			return ErrExecutionAuthorizationPermissionDenied
		}
		leaseExpiry, err := tx.lockInternalTaskExecution(ctx, authorization, input)
		if err != nil {
			return err
		}
		expiresAt := authorization.ExpiresAt
		if leaseExpiry.Before(expiresAt) {
			expiresAt = leaseExpiry
		}
		if membership.ExpiresAt != nil && membership.ExpiresAt.Before(expiresAt) {
			expiresAt = *membership.ExpiresAt
		}
		now, err = tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !expiresAt.After(now) {
			return ErrExecutionAuthorizationUnavailable
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.execution_authorization.consumed",
			Result: AuditResultSucceeded, RiskLevel: "medium", ModuleName: "system",
			EntityType: "execution_authorization", EntityID: strconv.FormatInt(authorization.ID, 10),
			Details: map[string]any{"audience": authorization.Audience, "execution_id": input.ExecutionID.String(),
				"internal_task": input.InternalTask, "attempt": input.Attempt},
		}); err != nil {
			return err
		}
		result = &AuthorizedInternalTask{AuthorizationID: authorization.ID, ExecutionID: input.ExecutionID,
			TenantID: input.TenantID, Audience: authorization.Audience, InternalTask: input.InternalTask,
			Attempt: input.Attempt, ExpiresAt: expiresAt.UTC()}
		return nil
	})
	return result, err
}
