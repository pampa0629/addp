package repository

import (
	"context"
	"encoding/json"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
)

func (r *Model3DGLBRepository) BindSourceAuthorization(ctx context.Context, expected *commonExecution.TaskExecution, scope commonExecution.ManagerFileReadScope, issued *commonClient.IssuedManagerFileAuthorization) error {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeModel3DGLBGeneration}).BindSourceAuthorization(ctx, expected, scope, issued)
}
func (r *Model3DGLBRepository) TaskConfigSnapshot(ctx context.Context, taskID, tenantID uint) (*FileTaskSnapshot, error) {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeModel3DGLBGeneration}).TaskConfigSnapshot(ctx, taskID, tenantID)
}
func (r *Model3DGLBRepository) RawExecutionConfig(ctx context.Context, tenantID int, executionID string) (json.RawMessage, error) {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeModel3DGLBGeneration}).RawExecutionConfig(ctx, tenantID, executionID)
}
func (r *Model3DGLBRepository) GetExecution(ctx context.Context, tenantID int, executionID string) (*commonExecution.TaskExecution, error) {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeModel3DGLBGeneration}).GetExecution(ctx, tenantID, executionID)
}
func (r *Model3DGLBRepository) FailUnboundExecution(ctx context.Context, expected *commonExecution.TaskExecution) error {
	return (fileSourceAuthorizationRepository{db: r.db, taskType: commonExecution.TaskTypeModel3DGLBGeneration}).FailUnboundExecution(ctx, expected)
}
