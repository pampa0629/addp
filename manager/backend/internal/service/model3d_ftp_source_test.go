package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/manager/internal/models"
)

func TestModel3DFTPConfigCannotFallThroughAfterEngineTypeChange(t *testing.T) {
	for _, scenario := range []struct {
		name, version, engineType string
		lookups                   int
		authorization             bool
	}{
		{"unknown config", "model3d-glb-ftp-config/other", "nfs", 0, false},
		{"frozen FTP changed to NFS", models.Model3DGLBFTPConfigVersion, "nfs", 1, false},
		{"frozen FTP changed to MinIO", models.Model3DGLBFTPConfigVersion, "minio", 1, false},
		{"orphan FTP authorization changed to NFS", "", "nfs", 1, true},
		{"noncanonical uppercase FTP", "", "FTP", 1, false},
		{"noncanonical padded FTP", "", " ftp ", 1, false},
		{"missing engine type", "", "", 1, false},
		{"real FTP missing marker", "", "ftp", 1, false},
		{"real FTP missing authorization", models.Model3DGLBFTPConfigVersion, "ftp", 1, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			lookups := 0
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				lookups++
				if r.URL.Path != "/api/v1/system/engines/26" {
					t.Errorf("unexpected source classification request: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":26,"tenant_id":7,"version":1,"engine_type":%q,"connection_info":{"export_path":"/must-not-be-read"},"lifecycle_state":"active","connection_status":"online"}`, scenario.engineType)
			}))
			defer system.Close()
			store := &stagingModelStore{}
			executor := NewManagerModel3DGLBExecutor(newTestSystemClient(system.URL), recordingWorkflowLister{}, store, "minio:9000", "infra-key", "infra-secret", false, "manager", 0)
			var auth *Model3DGLBSourceAuthorization
			if scenario.authorization {
				auth = &Model3DGLBSourceAuthorization{}
			}
			result, err := executor.BuildModel3DGLB(context.Background(), Model3DGLBExecutionRequest{
				Task:                &models.Model3DGLBTask{TenantID: 7},
				SourceAuthorization: auth,
				Config:              Model3DGLBExecutionConfig{ConfigVersion: scenario.version, Source: Model3DGLBSourceConfig{SourceEngineID: 26, ItemID: 77, ItemLocator: "addp://engine/26/path/models/model.ifc?type=file&item_id=77", Format: "ifc"}, Result: Model3DGLBResultConfig{FileName: "model.glb"}},
			})
			if result != nil || !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) {
				t.Fatalf("unauthorized source accepted or routed elsewhere: result=%v err=%v", result, err)
			}
			if lookups != scenario.lookups || store.puts != 0 || len(store.objects) != 0 {
				t.Fatalf("invalid FTP config performed unexpected I/O: lookups=%d puts=%d", lookups, store.puts)
			}
		})
	}
}
