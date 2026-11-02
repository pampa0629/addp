package capture

import (
	"context"
	"fmt"

	"github.com/addp/common/client"
	"github.com/addp/transfer/internal/models"
)

type cleanupConnectionContextKey struct{}
type cleanupConnectionContext struct {
	engineID uint
	resolve  func(context.Context, uint) (*client.CleanupEngineConnection, error)
}

// WithCleanupEngineConnection binds the exact retiring Engine only. A failed
// decision on that target must never fall back to ordinary Engine resolution.
func WithCleanupEngineConnection(ctx context.Context, engineID uint, resolve func(context.Context, uint) (*client.CleanupEngineConnection, error)) context.Context {
	return context.WithValue(ctx, cleanupConnectionContextKey{}, &cleanupConnectionContext{engineID: engineID, resolve: resolve})
}

type cleanupObserverKey struct{}

type cleanupConnectionError struct{ cause error }

func (e *cleanupConnectionError) Error() string { return "capture cleanup failed" }
func (e *cleanupConnectionError) Unwrap() error { return e.cause }

func safeCleanupFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if _, bound := ctx.Value(cleanupConnectionContextKey{}).(*cleanupConnectionContext); bound {
		return &cleanupConnectionError{cause: err}
	}
	return err
}

func WithCleanupObserver(ctx context.Context, observe func(string)) context.Context {
	return context.WithValue(ctx, cleanupObserverKey{}, observe)
}

func noteCleanupStage(ctx context.Context, stage string) {
	switch stage {
	case "transfer_cleanup_capture_plan", "transfer_cleanup_capture_connector", "transfer_cleanup_capture_source", "transfer_cleanup_capture_group", "transfer_cleanup_capture_topic", "transfer_cleanup_capture_acl", "transfer_cleanup_capture_history_topic", "transfer_cleanup_capture_history_acl":
		if observe, ok := ctx.Value(cleanupObserverKey{}).(func(string)); ok && observe != nil {
			observe(stage)
		}
	}
}

func validateCleanupSource(plan *CapturePlan, resource *models.CaptureResource, task *models.TransferTask) error {
	if plan == nil || resource == nil || task == nil || resource.TaskID != task.ID || resource.TenantID != task.TenantID || resource.Generation == 0 || plan.SourceEngineID == 0 || plan.SourceType != resource.SourceType || plan.SourceEngineID != resource.SourceEngineID || plan.SourceDatabase != resource.SourceDatabase || plan.SourceSchema != resource.SourceSchema || plan.SourceTable != resource.SourceTable || plan.SourceIdentity == "" || plan.SourceIdentity != resource.SourceIdentity || plan.SourceConnectionFingerprint == "" || plan.SourceConnectionFingerprint != resource.SourceConnectionFingerprint {
		return fmt.Errorf("cleanup plan does not match the frozen capture generation")
	}
	return nil
}
