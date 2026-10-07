package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Exercise the real handler, repository and MinIO SDK; the object server is a T1
// protocol fixture. Real MinIO deletion is independently checked by Hosted T4.
func TestPPTXPDFDeleteManagedObjectLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:pptx_delete_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE manager.pptx_pdf (
 id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, item_fingerprint TEXT NOT NULL,
 artifact_variant TEXT NOT NULL, source_version TEXT NOT NULL, source_engine_id INTEGER NOT NULL,
 item_id INTEGER NOT NULL, locator TEXT NOT NULL, task_id INTEGER, last_execution_id TEXT,
 storage_ref TEXT NOT NULL, file_name TEXT NOT NULL, size_bytes INTEGER NOT NULL,
 page_count INTEGER NOT NULL, content_url TEXT, status TEXT NOT NULL, metadata JSON,
 error_message TEXT, created_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	var objectMu sync.Mutex
	objects := map[string][]byte{"/business/source.pptx": []byte("source PPTX")}
	deletePaths := []string{}
	failDelete := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		objectMu.Lock()
		defer objectMu.Unlock()
		if r.URL.Query().Has("location") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<LocationConstraint>us-east-1</LocationConstraint>`)
			return
		}
		if r.Method == http.MethodDelete {
			deletePaths = append(deletePaths, r.URL.Path)
			if failDelete {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>fixture deletion denied</Message></Error>`)
				return
			}
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		content, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("ETag", `"pdf-test"`)
		w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		if r.Method != http.MethodHead {
			_, _ = w.Write(content)
		}
	}))
	defer server.Close()
	client := model3DTilesMinIOClient(t, server.URL)
	repo := repository.NewPPTXPDFRepository(db)
	svc := service.NewPPTXPDFTaskService(repo)
	svc.SetCleaner(service.NewMinIOManagedObjectCleaner(client, "manager"))
	handler := NewPPTXPDFHandler(svc, client, "manager")
	router := gin.New()
	router.Use(func(c *gin.Context) { setTenantAuthContextForTest(c, 7, 1); c.Next() })
	router.DELETE("/pptx_pdf/:id", handler.DeleteResult)
	router.GET("/pptx_pdf/:id/content", handler.GetContent)
	create := func(tenant, taskID uint, name string) *models.PPTXPDF {
		object := fmt.Sprintf("tenant_%d/document-preview/%s/slides.pdf", tenant, name)
		objectMu.Lock()
		objects["/manager/"+object] = []byte("%PDF-1.7 preview")
		objectMu.Unlock()
		result := &models.PPTXPDF{TenantID: tenant, TaskID: &taskID, ItemID: taskID + 1000, ItemFingerprint: name,
			ArtifactVariant: models.PPTXPDFArtifactVariant, SourceVersion: "v1", SourceEngineID: 12,
			Locator: "addp://engine/12/path/business/source.pptx?type=object", StorageRef: cog.ObjectStorageRef("manager", object),
			FileName: "slides.pdf", SizeBytes: 16, PageCount: 3, Status: models.PPTXPDFStatusReady, Metadata: commonModels.JSONMap{}}
		if err := repo.CreateResult(context.Background(), result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	target := create(7, 101, "target")
	otherTask := create(7, 102, "other-task")
	foreign := create(8, 101, "foreign")
	failure := create(7, 103, "failure")
	request := func(method string, result *models.PPTXPDF, content bool, want int) *httptest.ResponseRecorder {
		path := fmt.Sprintf("/pptx_pdf/%d", result.ID)
		if content {
			path += "/content"
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != want {
			t.Fatalf("%s %s = %d: %s, want %d", method, path, response.Code, response.Body.String(), want)
		}
		return response
	}
	request(http.MethodGet, target, true, 200)
	request(http.MethodDelete, foreign, false, 404)
	request(http.MethodGet, foreign, true, 404)
	objectMu.Lock()
	if len(deletePaths) != 0 {
		t.Fatalf("foreign tenant reached object delete: %v", deletePaths)
	}
	objectMu.Unlock()
	// A task ID is not a result ID; no object may be selected by task ownership alone.
	request(http.MethodDelete, &models.PPTXPDF{ID: 101}, false, 404)
	request(http.MethodDelete, target, false, 200)
	objectMu.Lock()
	if len(deletePaths) != 1 || deletePaths[0] != "/manager/tenant_7/document-preview/target/slides.pdf" {
		t.Fatalf("deleted paths = %v", deletePaths)
	}
	if _, exists := objects[deletePaths[0]]; exists {
		t.Fatal("target object remains")
	}
	objectMu.Unlock()
	if current, err := repo.GetResult(context.Background(), target.ID, 7); err != nil || current != nil {
		t.Fatalf("deleted result = %#v, %v", current, err)
	}
	request(http.MethodGet, target, true, 404)
	request(http.MethodGet, otherTask, true, 200)
	objectMu.Lock()
	if _, exists := objects["/manager/tenant_8/document-preview/foreign/slides.pdf"]; !exists {
		t.Fatal("foreign object deleted")
	}
	if string(objects["/business/source.pptx"]) != "source PPTX" {
		t.Fatal("source changed")
	}
	failDelete = true
	objectMu.Unlock()
	response := request(http.MethodDelete, failure, false, 500)
	if !strings.Contains(response.Body.String(), "fixture deletion denied") || !strings.Contains(response.Body.String(), "failure/slides.pdf") {
		t.Fatalf("missing deletion diagnostic: %s", response.Body.String())
	}
	current, err := repo.GetResult(context.Background(), failure.ID, 7)
	if err != nil || current == nil || current.Status != models.PPTXPDFStatusReady || current.StorageRef != failure.StorageRef {
		t.Fatalf("failed deletion changed result = %#v, %v", current, err)
	}
	request(http.MethodGet, failure, true, 200)
	objectMu.Lock()
	failDelete = false
	objectMu.Unlock()
	request(http.MethodDelete, failure, false, 200)
	request(http.MethodGet, failure, true, 404)
}
