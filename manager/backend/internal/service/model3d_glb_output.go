package service

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/repository"
	"path"
	"strconv"
	"strings"

	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/google/uuid"
)

// The target is allocated before pending persistence and signed with the whole
// conversion config. Rebuilding a source cannot overwrite its previous object.
func model3DGLBExecutionOutput(tenantID uint, executionID, bucket, fileName string) (Model3DGLBResultConfig, error) {
	id, err := uuid.Parse(executionID)
	if err != nil || id == uuid.Nil || id.String() != executionID || tenantID == 0 || bucket == "" || bucket != strings.TrimSpace(bucket) || strings.ContainsAny(bucket, "/\\\x00") {
		return Model3DGLBResultConfig{}, ErrFileSourceAuthorizationRequired
	}
	name := safeGLBFileName(fileName)
	object := fmt.Sprintf("tenant_%d/model3d-quick-view/executions/%s/%s", tenantID, executionID, name)
	return Model3DGLBResultConfig{StorageRef: rastercogref.ObjectStorageRef(bucket, object), FileName: name}, nil
}

func validateModel3DGLBExecutionOutput(tenantID uint, executionID, bucket string, result Model3DGLBResultConfig) error {
	expected, err := model3DGLBExecutionOutput(tenantID, executionID, bucket, result.FileName)
	if err != nil || result != expected {
		return ErrFileSourceAuthorizationRequired
	}
	return nil
}

func (s *Model3DGLBTaskService) cleanupExecutionOutputs(ctx context.Context) error {
	if s == nil || s.repo == nil || s.cleaner == nil || s.bucket == "" {
		return nil
	}
	_, err := s.repo.CleanupExecutionOutputs(ctx, 100, s.deleteOwnedExecutionInput, func(ctx context.Context, tenantID uint, storageRef string) error {
		return s.deleteOwnedExecutionOutput(ctx, tenantID, storageRef)
	})
	return err
}

func (s *Model3DGLBTaskService) deleteOwnedExecutionOutput(ctx context.Context, tenantID uint, storageRef string) error {
	bucket, object, err := rastercogref.ObjectLocation(storageRef, s.bucket)
	prefix := fmt.Sprintf("tenant_%d/model3d-quick-view/", tenantID)
	if err != nil || tenantID == 0 || bucket != s.bucket || !strings.HasPrefix(object, prefix) ||
		path.Clean(object) != object || path.Ext(object) != ".glb" ||
		rastercogref.ObjectStorageRef(bucket, object) != storageRef || s.cleaner == nil {
		return ErrFileSourceAuthorizationRequired
	}
	return s.cleaner.DeleteByStorageRef(ctx, storageRef)
}

// Input ownership is persistence only; source authorization is checked separately.
type fileStagingOwnership interface {
	prepare(context.Context, repository.FileInputPreparation) error
	finish(context.Context, repository.FileInputPreparation, bool, bool) error
}

type model3DGLBInputOwner struct {
	repo                *repository.Model3DGLBRepository
	resultID, tenantID  uint
	executionID, bucket string
}

func (o *model3DGLBInputOwner) validate(p repository.FileInputPreparation) error {
	if o == nil || o.repo == nil || o.resultID == 0 || p.ExecutionID != o.executionID || p.ValidateForTask(o.tenantID, commonExecution.TaskTypeModel3DGLBGeneration) != nil {
		return ErrFileSourceAuthorizationRequired
	}
	for _, object := range p.Objects {
		if object.Bucket != o.bucket {
			return ErrFileSourceAuthorizationRequired
		}
	}
	return nil
}
func (o *model3DGLBInputOwner) prepare(ctx context.Context, p repository.FileInputPreparation) error {
	if err := o.validate(p); err != nil {
		return err
	}
	return o.repo.PrepareExecutionInputs(ctx, o.resultID, p)
}
func (o *model3DGLBInputOwner) finish(ctx context.Context, p repository.FileInputPreparation, settled, cleaned bool) error {
	if err := o.validate(p); err != nil {
		return err
	}
	return o.repo.FinishExecutionInputs(ctx, o.resultID, p, settled, cleaned)
}

func (s *Model3DGLBTaskService) deleteOwnedExecutionInput(ctx context.Context, tenantID uint, object repository.FileInputObject) error {
	parts := strings.Split(object.Object, "/")
	if len(parts) < 7 || object.Bucket != s.bucket || s.cleaner == nil {
		return ErrFileSourceAuthorizationRequired
	}
	attempt, err := strconv.Atoi(strings.TrimPrefix(parts[4], "attempt_"))
	p := repository.FileInputPreparation{ExecutionID: parts[3], Attempt: attempt, PreparationID: parts[5], Objects: []repository.FileInputObject{object}}
	if err != nil || p.ValidateForTask(tenantID, commonExecution.TaskTypeModel3DGLBGeneration) != nil {
		return ErrFileSourceAuthorizationRequired
	}
	return s.cleaner.DeleteExactObject(ctx, object.Bucket, object.Object)
}
