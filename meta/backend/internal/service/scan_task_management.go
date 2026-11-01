package service

import (
	"context"
	commonapi "github.com/addp/common/api"
	"github.com/addp/meta/internal/models"
)

type scanManagementContextKey struct{}
type ScanManagementCheck func(context.Context, *models.ScanTask, string) error

// The callback carries only the current synchronous request's User identity.
// It is invoked against the locked/current target, never persisted in a task.
func WithScanManagementCheck(ctx context.Context, check ScanManagementCheck) context.Context {
	return context.WithValue(ctx, scanManagementContextKey{}, check)
}
func checkScanTaskManagement(ctx context.Context, task *models.ScanTask, operation string) error {
	check, ok := ctx.Value(scanManagementContextKey{}).(ScanManagementCheck)
	if !ok || check == nil || task == nil {
		return commonapi.ErrForbidden
	}
	return check(ctx, task, operation)
}
