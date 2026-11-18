package iam

import (
	"context"
	"time"

	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

// TransferObjectAuthorizationService is the fixed source authorization for a
// User-initiated Transfer object import. The common kernel owns the IAM lifecycle.
type TransferObjectAuthorizationService struct {
	kernel *managerSourceAuthorizationService
}

func NewTransferObjectAuthorizationService(repository *Repository, prepare ExecutionSourceReadPreparer) *TransferObjectAuthorizationService {
	return &TransferObjectAuthorizationService{kernel: newManagerSourceAuthorizationService(repository, prepare, managerSourceObject)}
}

type IssueTransferObjectAuthorizationInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssuedTransferObjectAuthorization struct {
	ID              int64
	ExecutionID     uuid.UUID
	TenantID        int64
	SourceReadScope execution.TransferObjectReadScope
	ExpiresAt       time.Time
}

func (s *TransferObjectAuthorizationService) Issue(ctx context.Context, input IssueTransferObjectAuthorizationInput) (*IssuedTransferObjectAuthorization, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	row, err := s.kernel.issue(ctx, issueManagerSourceInput(input))
	if err != nil {
		return nil, err
	}
	scope := row.SourceReadScope.Object()
	if scope == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return &IssuedTransferObjectAuthorization{ID: row.ID, ExecutionID: row.ExecutionID, TenantID: row.TenantID, SourceReadScope: *scope, ExpiresAt: row.ExpiresAt}, nil
}

type AuthorizeTransferObjectInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	SourceReadScope    execution.TransferObjectReadScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

func (s *TransferObjectAuthorizationService) Authorize(ctx context.Context, input AuthorizeTransferObjectInput) (*ExecutionSourceReadObservation, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	observed, err := s.kernel.authorize(ctx, authorizeManagerSourceInput{
		AuthorizationID: input.AuthorizationID, ExecutionID: input.ExecutionID, Attempt: input.Attempt, LeaseToken: input.LeaseToken,
		SourceReadScope:    *NewTransferObjectSourceScope(&input.SourceReadScope),
		ServicePrincipalID: input.ServicePrincipalID, ServiceClientID: input.ServiceClientID, TenantID: input.TenantID, Audit: input.Audit,
	})
	if err != nil {
		return nil, err
	}
	if observed == nil || observed.Engine == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return observed, nil
}
