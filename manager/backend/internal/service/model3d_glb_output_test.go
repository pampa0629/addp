package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGLBInputCleanupUsesExactOwnedObjectNames(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("unexpected request %s", r.Method)
		}
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("fixture", "fixture-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewModel3DGLBTaskService(nil)
	svc.SetBucket("manager")
	svc.SetCleaner(NewMinIOManagedObjectCleaner(client, "manager"))
	object := repository.Model3DGLBInputObject{Bucket: "manager", Object: fmt.Sprintf("temp/model3d-input/tenant_7/%s/attempt_2/%s/颜色.PNG ", uuid.NewString(), uuid.NewString())}
	if err := svc.deleteOwnedExecutionInput(context.Background(), 7, object); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/manager/"+object.Object {
		t.Fatalf("exact object name changed: %q", paths)
	}
	for _, change := range []string{"tenant", "bucket", "prefix", "nonce", "traversal"} {
		altered := object
		tenant := uint(7)
		switch change {
		case "tenant":
			tenant = 8
		case "bucket":
			altered.Bucket = "business"
		case "prefix":
			altered.Object = strings.Replace(altered.Object, "model3d-input", "other", 1)
		case "nonce":
			parts := strings.Split(altered.Object, "/")
			parts[5] = "invented"
			altered.Object = strings.Join(parts, "/")
		case "traversal":
			altered.Object += "/../other"
		}
		if err := svc.deleteOwnedExecutionInput(t.Context(), tenant, altered); !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) {
			t.Fatalf("%s cleanup accepted: %v", change, err)
		}
	}
	if len(paths) != 1 {
		t.Fatal("invalid ownership reached object storage")
	}
}
