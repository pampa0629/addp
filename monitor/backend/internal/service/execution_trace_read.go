package service

import (
	"context"
	"time"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
)

// ReadExecutionForTrace runs inside TraceService's one deadline/upstream slot.
// Only this execution's Owner is contacted; no parent or other Owner is read.
// The returned private row is for the internal projector, never a response DTO.
func (s *ExecutionQueryService) ReadExecutionForTrace(ctx context.Context, id string, tenantID int, principalID int64, bearer string) (*execution.TaskExecution, error) {
	owner, err := s.repo.OwnerByExecutionID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if s.readResolver == nil {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	scopes, err := s.readResolver.Resolve(ctx, []string{owner}, tenantID, principalID, bearer)
	if err != nil {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	ctx = execution.WithReadScopes(ctx, scopes)
	var item *execution.TaskExecution
	err = s.repo.ReadSnapshot(ctx, func(repo *execution.TaskExecutionRepository, _ time.Time) error {
		var err error
		item, err = repo.GetByExecutionID(ctx, id, tenantID)
		if err != nil {
			return err
		}
		if item.Module != owner {
			return commonapi.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}
