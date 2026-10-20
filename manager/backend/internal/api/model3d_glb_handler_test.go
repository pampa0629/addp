package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	commonModels "github.com/addp/common/models"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestGLBContentRejectsTicketsAndIncompleteFTPBoundaryBeforeObjectIO(t *testing.T) {
	for _, name := range []string{"ticket only", "delegated", "missing saved scope", "changed to nfs"} {
		t.Run(name, func(t *testing.T) {
			db := newTaskProviderHandlerTestDB(t)
			repo := repository.NewModel3DGLBRepository(db)
			result := &models.Model3DGLB{TenantID: 7, ItemFingerprint: "ftp-rendered", Locator: "addp://engine/33/path/models/model.3ds?type=file&item_id=41", SourceEngineID: 33, SourceFormat: "3ds", Status: models.Model3DGLBStatusReady, StorageRef: rastercogref.ObjectStorageRef("manager", "tenant_7/model3d-quick-view/executions/unused/model.glb"), Metadata: commonModels.JSONMap{"model3d_output_owner": "manager_execution"}}
			if err := repo.Create(t.Context(), result); err != nil {
				t.Fatal(err)
			}
			var objectCalls atomic.Int64
			objects := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { objectCalls.Add(1); w.WriteHeader(500) }))
			t.Cleanup(objects.Close)
			minioClient, err := minio.New(strings.TrimPrefix(objects.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("fixture", "fixture", "")})
			if err != nil {
				t.Fatal(err)
			}
			svc := service.NewModel3DGLBTaskService(repo)
			svc.SetSourceEngineClient(glbFTPEngineFacts{})
			if name == "changed to nfs" {
				svc.SetSourceEngineClient(glbNFSEngineFacts{})
			}
			handler := NewModel3DGLBHandler(repo, minioClient, "manager")
			handler.SetSourceService(svc)
			router := gin.New()
			router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 7, 31); c.Next() })
			router.GET("/model_3d_glb/:id/content", handler.GetModel3DGLBContent)
			request := httptest.NewRequest("GET", "/model_3d_glb/1/content", nil)
			request.AddCookie(&http.Cookie{Name: "addp_rat_manager", Value: "old-ticket"})
			want := http.StatusForbidden
			switch name {
			case "ticket only":
				want = http.StatusUnauthorized
			case "delegated":
				request.Header.Set("Authorization", "Bearer addp_dat_tool")
				want = http.StatusUnauthorized
			default:
				request.Header.Set("Authorization", "Bearer addp_at_current_human")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != want || objectCalls.Load() != 0 {
				t.Fatalf("status=%d objectCalls=%d body=%s", response.Code, objectCalls.Load(), response.Body.String())
			}
		})
	}
}
