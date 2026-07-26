package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModel3DTilesHandlerServesReadyAssetsWithContentTypeAndRange(t *testing.T) {
	objects := map[string][]byte{
		"/manager/tenant_7/model3d-tiles/fp/3d_tiles/tileset.json": []byte(`{"asset":{"version":"1.1"}}`),
		"/manager/tenant_7/model3d-tiles/fp/s3m/config/scene.scp":  []byte("<SuperMapCache/>"),
	}
	server := newModel3DTilesObjectServer(t, objects, nil)
	db := newModel3DTilesHandlerTestDB(t)
	threeDTiles := createModel3DTilesHandlerResult(t, db, 7, models.Model3DTilesStatusReady, "tenant_7/model3d-tiles/fp/3d_tiles")
	s3m := createModel3DTilesHandlerResult(t, db, 7, models.Model3DTilesStatusReady, "tenant_7/model3d-tiles/fp/s3m")
	router := newModel3DTilesHandlerTestRouter(t, db, server.URL, 7)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/model3d_tiles/%d/assets/tileset.json", threeDTiles.ID), nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("tileset response: status=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("tileset cache headers: cache-control=%q pragma=%q", recorder.Header().Get("Cache-Control"), recorder.Header().Get("Pragma"))
	}
	if recorder.Body.String() != string(objects["/manager/tenant_7/model3d-tiles/fp/3d_tiles/tileset.json"]) {
		t.Fatalf("tileset body = %q", recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/model3d_tiles/%d/assets/config/scene.scp", s3m.ID), nil)
	request.Header.Set("Range", "bytes=0-7")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("S3M range status = %d, want 206; body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "application/vnd.supermap.s3m-config" {
		t.Fatalf("S3M content-type = %q", recorder.Header().Get("Content-Type"))
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("S3M cache headers: cache-control=%q pragma=%q", recorder.Header().Get("Cache-Control"), recorder.Header().Get("Pragma"))
	}
	if recorder.Header().Get("Content-Range") != "bytes 0-7/16" || recorder.Body.String() != "<SuperMa" {
		t.Fatalf("S3M range headers/body: content-range=%q body=%q", recorder.Header().Get("Content-Range"), recorder.Body.String())
	}
}

func TestModel3DTilesHandlerRejectsUnavailableTenantAndTraversal(t *testing.T) {
	var objectRequests atomic.Int64
	server := newModel3DTilesObjectServer(t, nil, &objectRequests)
	db := newModel3DTilesHandlerTestDB(t)
	failed := createModel3DTilesHandlerResult(t, db, 7, models.Model3DTilesStatusFailed, "tenant_7/model3d-tiles/fp/3d_tiles")
	otherTenant := createModel3DTilesHandlerResult(t, db, 8, models.Model3DTilesStatusReady, "tenant_8/model3d-tiles/fp/3d_tiles")
	ready := createModel3DTilesHandlerResult(t, db, 7, models.Model3DTilesStatusReady, "tenant_7/model3d-tiles/fp/3d_tiles")
	router := newModel3DTilesHandlerTestRouter(t, db, server.URL, 7)

	for _, id := range []uint{failed.ID, otherTenant.ID} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/model3d_tiles/%d/assets/tileset.json", id), nil)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("result %d status = %d, want 404", id, recorder.Code)
		}
	}

	handler := NewModel3DTilesHandler(repository.NewModel3DTilesRepository(db), model3DTilesMinIOClient(t, server.URL), "manager")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	setTenantAuthContextForTest(ctx, 7, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(ready.ID), 10)}, {Key: "asset_path", Value: "../secret"}}
	handler.GetAsset(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, want 400", recorder.Code)
	}
	if objectRequests.Load() != 0 {
		t.Fatalf("object store requests = %d, want 0 for rejected requests", objectRequests.Load())
	}
}

func newModel3DTilesHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:model3d_tiles_handler_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.model3d_tiles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER,
		locator TEXT,
		task_id INTEGER,
		last_execution_id TEXT,
		source_engine_id INTEGER NOT NULL,
		source_format TEXT NOT NULL,
		source_size_bytes INTEGER,
		target_format TEXT NOT NULL,
		storage_ref TEXT NOT NULL,
		manifest_ref TEXT NOT NULL,
		file_count INTEGER,
		size_bytes INTEGER,
		status TEXT NOT NULL,
		metadata JSON,
		error_message TEXT,
		created_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create model3d tiles result table: %v", err)
	}
	return db
}

func createModel3DTilesHandlerResult(t *testing.T, db *gorm.DB, tenantID uint, status, prefix string) *models.Model3DTiles {
	t.Helper()
	targetFormat := models.Model3DTilesTargetFormat3DTiles
	manifestRef := "tileset.json"
	if strings.HasSuffix(prefix, "/s3m") {
		targetFormat = models.Model3DTilesTargetFormatS3M
		manifestRef = "config/scene.scp"
	}
	result := &models.Model3DTiles{
		TenantID: tenantID, ItemFingerprint: fmt.Sprintf("fp-%d-%d", tenantID, time.Now().UnixNano()), Locator: "addp://engine/26/path/site?type=directory",
		SourceEngineID: 26, SourceFormat: "osgb_scene", TargetFormat: targetFormat,
		StorageRef:  fmt.Sprintf(`{"type":"object","provider":"addp_object_storage","bucket":"manager","object":%q}`, prefix),
		ManifestRef: manifestRef, Status: status, Metadata: commonModels.JSONMap{},
	}
	if err := db.Create(result).Error; err != nil {
		t.Fatalf("create model3d tiles result: %v", err)
	}
	return result
}

func newModel3DTilesHandlerTestRouter(t *testing.T, db *gorm.DB, serverURL string, tenantID uint) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := NewModel3DTilesHandler(repository.NewModel3DTilesRepository(db), model3DTilesMinIOClient(t, serverURL), "manager")
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setTenantAuthContextForTest(c, tenantID, 1)
		c.Next()
	})
	router.GET("/model3d_tiles/:id/assets/*asset_path", handler.GetAsset)
	return router
}

func model3DTilesMinIOClient(t *testing.T, serverURL string) *minio.Client {
	t.Helper()
	client, err := minio.New(strings.TrimPrefix(serverURL, "http://"), &minio.Options{
		Creds:  credentials.NewStaticV4("test-access", "test-secret", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("create MinIO client: %v", err)
	}
	return client
}

func newModel3DTilesObjectServer(t *testing.T, objects map[string][]byte, requests *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			requests.Add(1)
		}
		if r.Method == http.MethodGet && r.URL.Path == "/manager/" {
			if _, ok := r.URL.Query()["location"]; ok {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`))
				return
			}
		}
		content, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"model3d-test"`)
		w.Header().Set("Last-Modified", time.Unix(1_700_000_000, 0).UTC().Format(http.TimeFormat))
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			return
		}
		start, end := 0, len(content)-1
		if rawRange := r.Header.Get("Range"); strings.HasPrefix(rawRange, "bytes=") {
			parts := strings.Split(strings.TrimPrefix(rawRange, "bytes="), "-")
			if len(parts) == 2 {
				if value, err := strconv.Atoi(parts[0]); err == nil {
					start = value
				}
				if parts[1] != "" {
					if value, err := strconv.Atoi(parts[1]); err == nil {
						end = value
					}
				}
			}
			if start < 0 || end < start || start >= len(content) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if end >= len(content) {
				end = len(content) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(content[start : end+1])
	}))
	t.Cleanup(server.Close)
	return server
}
