package repository

import (
	"context"
	"encoding/json"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
)

func (r *RasterCOGRepository) BindSourceAuthorization(ctx context.Context, expected *commonExecution.TaskExecution, scope commonExecution.ManagerFileReadScope, issued *commonClient.IssuedManagerFileAuthorization) error {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeRasterCOGGeneration}).BindSourceAuthorization(ctx, expected, scope, issued)
}
func (r *RasterCOGRepository) TaskConfigSnapshot(ctx context.Context, taskID, tenantID uint) (*FileTaskSnapshot, error) {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeRasterCOGGeneration}).TaskConfigSnapshot(ctx, taskID, tenantID)
}
func (r *RasterCOGRepository) RawExecutionConfig(ctx context.Context, tenantID int, executionID string) (json.RawMessage, error) {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeRasterCOGGeneration}).RawExecutionConfig(ctx, tenantID, executionID)
}
func (r *RasterCOGRepository) GetExecution(ctx context.Context, tenantID int, executionID string) (*commonExecution.TaskExecution, error) {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeRasterCOGGeneration}).GetExecution(ctx, tenantID, executionID)
}
func (r *RasterCOGRepository) FailUnboundExecution(ctx context.Context, expected *commonExecution.TaskExecution) error {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeRasterCOGGeneration}).FailUnboundExecution(ctx, expected)
}
