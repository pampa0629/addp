package service

import (
	"context"
	"fmt"

	"github.com/addp/develop/backend/internal/models"
)

// Only server-prepared executions place this immutable budget in a context.
type querySnapshotKey struct{}
type querySnapshot struct{ Timeout, ResultLimit int }

func contextWithQuerySnapshot(ctx context.Context, task *models.DevTask) context.Context {
	return context.WithValue(ctx, querySnapshotKey{}, querySnapshot{task.Timeout, task.QueryResultLimit})
}

func (e *DevExecutor) freezeExecutionPolicy(ctx context.Context, task *models.DevTask, tenantID uint) error {
	if e.sqlEngine == nil {
		return fmt.Errorf("query policy service is unavailable")
	}
	defaultTimeout, maxTimeout, resultLimit, err := e.sqlEngine.resolveExecutionPolicy(ctx, tenantID)
	if err != nil {
		return err
	}
	if task.Timeout <= 0 {
		task.Timeout = defaultTimeout
	}
	if task.Timeout > maxTimeout {
		task.Timeout = maxTimeout
	}
	task.QueryResultLimit = resultLimit
	return nil
}

func (s *SQLEngineService) resolveExecutionPolicy(ctx context.Context, tenantID uint) (int, int, int, error) {
	if s.executionPolicy == nil {
		defaults := defaultExecutionPolicyModel("platform", nil)
		return defaults.DefaultQueryTimeout, defaults.MaxQueryTimeout, defaults.QueryResultLimit, nil
	}
	return s.executionPolicy.ResolveRuntime(ctx, tenantID)
}
