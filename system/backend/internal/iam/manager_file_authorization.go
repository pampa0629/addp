package iam

import "context"

type ManagerFileAuthorizationService struct {
	kernel *managerSourceAuthorizationService
}

func NewManagerModel3DAuthorizationService(repository *Repository, prepare ExecutionSourceReadPreparer) *ManagerFileAuthorizationService {
	return &ManagerFileAuthorizationService{kernel: newManagerSourceAuthorizationService(repository, prepare, managerSourceModel3D)}
}

func NewManagerRasterCOGAuthorizationService(repository *Repository, prepare ExecutionSourceReadPreparer) *ManagerFileAuthorizationService {
	return &ManagerFileAuthorizationService{kernel: newManagerSourceAuthorizationService(repository, prepare, managerSourceRasterCOG)}
}

func (s *ManagerFileAuthorizationService) Issue(ctx context.Context, input IssueManagerFileAuthorizationInput) (*IssuedManagerFileAuthorization, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	row, err := s.kernel.issue(ctx, issueManagerSourceInput(input))
	if err != nil {
		return nil, err
	}
	scope := row.SourceReadScope.File()
	if scope == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return &IssuedManagerFileAuthorization{ID: row.ID, ExecutionID: row.ExecutionID, TenantID: row.TenantID, ExpiresAt: row.ExpiresAt, SourceReadScope: *scope}, nil
}

func (s *ManagerFileAuthorizationService) Authorize(ctx context.Context, input AuthorizeManagerFileInput) (*ManagerFileAccessObservation, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	scope := NewManagerFileSourceScope(&input.SourceReadScope)
	observation, err := s.kernel.authorize(ctx, authorizeManagerSourceInput{AuthorizationID: input.AuthorizationID, ExecutionID: input.ExecutionID, Attempt: input.Attempt, LeaseToken: input.LeaseToken, SourceReadScope: *scope, ServicePrincipalID: input.ServicePrincipalID, ServiceClientID: input.ServiceClientID, TenantID: input.TenantID, Audit: input.Audit})
	if err != nil {
		return nil, err
	}
	if observation.Engine == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return &ManagerFileAccessObservation{ObservedAt: observation.ObservedAt, Engine: observation.Engine}, nil
}
