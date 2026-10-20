package iam

import "context"

type ManagerModel3DAuthorizationService struct {
	kernel *managerSourceAuthorizationService
}

func NewManagerModel3DAuthorizationService(repository *Repository, prepare ExecutionSourceReadPreparer) *ManagerModel3DAuthorizationService {
	return &ManagerModel3DAuthorizationService{kernel: newManagerSourceAuthorizationService(repository, prepare, managerSourceModel3D)}
}

func (s *ManagerModel3DAuthorizationService) Issue(ctx context.Context, input IssueManagerModel3DAuthorizationInput) (*IssuedManagerModel3DAuthorization, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	row, err := s.kernel.issue(ctx, issueManagerSourceInput(input))
	if err != nil {
		return nil, err
	}
	scope := row.SourceReadScope.Model3D()
	if scope == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return &IssuedManagerModel3DAuthorization{ID: row.ID, ExecutionID: row.ExecutionID, TenantID: row.TenantID, ExpiresAt: row.ExpiresAt, SourceReadScope: *scope}, nil
}

func (s *ManagerModel3DAuthorizationService) Authorize(ctx context.Context, input AuthorizeManagerModel3DInput) (*ManagerModel3DAccessObservation, error) {
	if s == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	scope := NewManagerModel3DSourceScope(&input.SourceReadScope)
	observation, err := s.kernel.authorize(ctx, authorizeManagerSourceInput{AuthorizationID: input.AuthorizationID, ExecutionID: input.ExecutionID, Attempt: input.Attempt, LeaseToken: input.LeaseToken, SourceReadScope: *scope, ServicePrincipalID: input.ServicePrincipalID, ServiceClientID: input.ServiceClientID, TenantID: input.TenantID, Audit: input.Audit})
	if err != nil {
		return nil, err
	}
	if observation.Engine == nil {
		return nil, ErrExecutionAuthorizationUnavailable
	}
	return &ManagerModel3DAccessObservation{ObservedAt: observation.ObservedAt, Engine: observation.Engine}, nil
}
