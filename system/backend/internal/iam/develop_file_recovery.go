package iam

import (
	"context"
	"reflect"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *DevelopFileAuthorizationService) SetRecoveryEngine(resolve func(context.Context, *gorm.DB, int64, execution.DevelopBoundFileTarget) (*models.Engine, error)) {
	s.recoveryEngine = resolve
}

func (s *DevelopFileAuthorizationService) PublicationRecoveryAccess(ctx context.Context, input CheckDevelopFileInput, operation uuid.UUID) (*client.DevelopFilePublicationRecoveryAccess, error) {
	history, err := s.ReadPublicationHistory(ctx, input, operation)
	if err != nil {
		return nil, err
	}
	if s.owner == nil || s.recoveryEngine == nil {
		return nil, ErrDevelopFileDependencyUnavailable
	}
	ea, err := s.repository.GetExecutionAuthorization(ctx, input.AuthorizationID)
	if err != nil {
		return nil, err
	}
	facts, err := s.readOwner(ctx, ea, ea.ExecutionID)
	if err != nil {
		return nil, err
	}
	if facts.Publication == nil || facts.AuthorizationID != history.AuthorizationID || facts.ScopeDigest != history.ScopeDigest ||
		!reflect.DeepEqual(facts.BoundScope, ea.DevelopFileScope) || facts.Publication.ApprovalID != history.ApprovalID ||
		facts.Publication.Request.OperationID != history.OperationID {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	digest, err := client.DevelopFilePublicationRequestDigest(facts.Publication.Request)
	if err != nil || digest != history.RequestDigest || (history.ReleasedAt != nil && facts.Publication.PreparationDigest != history.PreparationDigest) {
		return nil, ErrExecutionAuthorizationPermissionDenied
	}
	var result *client.DevelopFilePublicationRecoveryAccess
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		if _, err := tx.developFileService(ctx, input); err != nil {
			return err
		}
		engine, err := s.recoveryEngine(ctx, tx.db, input.TenantID, *ea.DevelopFileScope.Target)
		if err != nil {
			return err
		}
		now, err := tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		current, err := tx.developFileService(ctx, input)
		if err != nil {
			return err
		}
		expires := now.Add(15 * time.Second)
		if current.Token.ExpiresAt.Before(expires) {
			expires = current.Token.ExpiresAt
		}
		if !expires.After(now) {
			return ErrExecutionAuthorizationUnavailable
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{Metadata: input.Audit, EventName: "iam.develop_file_publication.recovery",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "develop_file_publication",
			EntityID: operation.String(), Details: map[string]any{"approval_id": history.ApprovalID, "execution_id": history.ExecutionID}}); err != nil {
			return err
		}
		finished, err := tx.internalTaskDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !expires.After(finished) {
			return ErrExecutionAuthorizationUnavailable
		}
		result = &client.DevelopFilePublicationRecoveryAccess{History: *history, ObservedAt: now, ExpiresAt: expires, Engine: engine}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
