package service

import (
	"context"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
)

func (s *ExecutionQueryService) SetReadResolver(resolver commonclient.ExecutionReadResolver) {
	s.readResolver = resolver
}

func (s *ExecutionQueryService) AuthorizeReadRequest(ctx context.Context, tenantID int, principalID int64, bearer, module string) (context.Context, error) {
	owners, err := s.repo.ListOwnerModules(ctx, tenantID)
	if err != nil {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	if module != "" {
		selected := []string{}
		for _, owner := range owners {
			if owner == module {
				selected = append(selected, owner)
			}
		}
		owners = selected
	}
	if len(owners) == 0 {
		return execution.WithReadScopes(ctx, []execution.ReadScope{}), nil
	}
	if s.readResolver == nil {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	scopes, err := s.readResolver.Resolve(ctx, owners, tenantID, principalID, bearer)
	if err != nil {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	return execution.WithReadScopes(ctx, scopes), nil
}
