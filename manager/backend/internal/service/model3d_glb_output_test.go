package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestModel3DGLBExecutionOutputIsExactAndIsolated(t *testing.T) {
	id := uuid.NewString()
	first, err := model3DGLBExecutionOutput(7, id, "manager", "model.glb")
	if err != nil {
		t.Fatal(err)
	}
	other, err := model3DGLBExecutionOutput(7, uuid.NewString(), "manager", "model.glb")
	if err != nil || first.StorageRef == other.StorageRef {
		t.Fatalf("same output across executions: %v", err)
	}
	if err := validateModel3DGLBExecutionOutput(7, id, "manager", first); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"tenant", "execution", "bucket", "target", "filename"} {
		t.Run(change, func(t *testing.T) {
			tenant, execution, bucket, result := uint(7), id, "manager", first
			switch change {
			case "tenant":
				tenant = 8
			case "execution":
				execution = uuid.NewString()
			case "bucket":
				bucket = "business"
			case "target":
				result.StorageRef = "infra://manager/tenant_7/model3d-quick-view/shared/model.glb"
			case "filename":
				result.FileName = "other.glb"
			}
			if err := validateModel3DGLBExecutionOutput(tenant, execution, bucket, result); !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) {
				t.Fatalf("changed output accepted: %v", err)
			}
		})
	}
}
