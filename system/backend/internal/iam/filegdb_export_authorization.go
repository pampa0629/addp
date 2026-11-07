package iam

import (
	"context"
	"time"

	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

// FileGDBExportAuthorizationService is the fixed source authorization for a
// Manager-initiated Transfer export. The common kernel owns the IAM lifecycle.
type FileGDBExportAuthorizationService struct {
	kernel *managerSourceAuthorizationService
}

func NewFileGDBExportAuthorizationService(repository *Repository, prepare ExecutionSourceReadPreparer) *FileGDBExportAuthorizationService {
	return &FileGDBExportAuthorizationService{kernel: newManagerSourceAuthorizationService(repository, prepare, managerSourceFileGDB)}
}

type IssueFileGDBExportAuthorizationInput struct {
	SourceAccessToken string
	ExecutionID       uuid.UUID
	ExpiresIn         time.Duration
	Audit             AuditMetadata
}

type IssuedFileGDBExportAuthorization struct {
	ID              int64
	ExecutionID     uuid.UUID
	TenantID        int64
	SourceReadScope execution.FileGDBExportReadScope
	ExpiresAt       time.Time
}

func (s *FileGDBExportAuthorizationService) Issue(ctx context.Context, input IssueFileGDBExportAuthorizationInput) (*IssuedFileGDBExportAuthorization, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	row, err := s.kernel.issue(ctx, issueManagerSourceInput(input))
	if err != nil {
		return nil, err
	}
	scope := row.SourceReadScope.FileGDB()
	if scope == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return &IssuedFileGDBExportAuthorization{ID: row.ID, ExecutionID: row.ExecutionID, TenantID: row.TenantID, SourceReadScope: *scope, ExpiresAt: row.ExpiresAt}, nil
}

type AuthorizeFileGDBExportInput struct {
	AuthorizationID    int64
	ExecutionID        uuid.UUID
	Attempt            int
	LeaseToken         uuid.UUID
	SourceReadScope    execution.FileGDBExportReadScope
	ServicePrincipalID int64
	ServiceClientID    string
	TenantID           int64
	Audit              AuditMetadata
}

func (s *FileGDBExportAuthorizationService) Authorize(ctx context.Context, input AuthorizeFileGDBExportInput) (*ExecutionSourceReadObservation, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	observed, err := s.kernel.authorize(ctx, authorizeManagerSourceInput{
		AuthorizationID: input.AuthorizationID, ExecutionID: input.ExecutionID, Attempt: input.Attempt, LeaseToken: input.LeaseToken,
		SourceReadScope:    *NewFileGDBExportSourceScope(&input.SourceReadScope),
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
