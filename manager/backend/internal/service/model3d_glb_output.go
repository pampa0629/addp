package service

import (
	"context"
	"fmt"
	"path"
	"strings"

	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/google/uuid"
)

// The target is allocated before pending persistence and signed with the whole
// conversion config. Rebuilding a source cannot overwrite its previous object.
func model3DGLBExecutionOutput(tenantID uint, executionID, bucket, fileName string) (Model3DGLBResultConfig, error) {
	id, err := uuid.Parse(executionID)
	if err != nil || id == uuid.Nil || id.String() != executionID || tenantID == 0 || bucket == "" || bucket != strings.TrimSpace(bucket) || strings.ContainsAny(bucket, "/\\\x00") {
		return Model3DGLBResultConfig{}, ErrModel3DGLBSourceAuthorizationRequired
	}
	name := safeGLBFileName(fileName)
	object := fmt.Sprintf("tenant_%d/model3d-quick-view/executions/%s/%s", tenantID, executionID, name)
	return Model3DGLBResultConfig{StorageRef: rastercogref.ObjectStorageRef(bucket, object), FileName: name}, nil
}

func validateModel3DGLBExecutionOutput(tenantID uint, executionID, bucket string, result Model3DGLBResultConfig) error {
	expected, err := model3DGLBExecutionOutput(tenantID, executionID, bucket, result.FileName)
	if err != nil || result != expected {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	return nil
}

func (s *Model3DGLBTaskService) cleanupExecutionOutputs(ctx context.Context) error {
	if s == nil || s.repo == nil || s.cleaner == nil || s.bucket == "" {
		return nil
	}
	_, err := s.repo.CleanupExecutionOutputs(ctx, 100, func(ctx context.Context, tenantID uint, storageRef string) error {
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
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	return s.cleaner.DeleteByStorageRef(ctx, storageRef)
}
