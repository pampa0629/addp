package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	objectminio "github.com/addp/common/infra/objectstore/minio"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/minio/minio-go/v7"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNotebookFreezeUsesTenantBytesAndLeavesDefinitionUnchanged(t *testing.T) {
	content := []byte(`{"cells":[]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/develop/tenant_7/notebooks/analysis.ipynb" {
			t.Errorf("unexpected storage path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("ETag", `"test-content"`)
		w.Write(content)
	}))
	t.Cleanup(server.Close)
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Region: "us-east-1", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	svc := &NotebookExecutionService{store: objectminio.New(client, "develop")}
	original := models.DevTaskContent{"engine_id": 9, "notebook_content_sha256": "untrusted-client-value"}
	task := &models.DevTask{TenantID: 7, DevType: "script", Content: models.DevTaskContent{"notebook_locator": "addp-infra://object/develop/tenant_7/notebooks/analysis.ipynb?type=object"}, ExecutionConfig: original}
	if err := svc.FreezeContent(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if task.ExecutionConfig["notebook_content_sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatal("snapshot digest does not match selected bytes")
	}
	if original["notebook_content_sha256"] != "untrusted-client-value" {
		t.Fatal("task definition changed during execution preparation")
	}
	for _, value := range []string{"", ".", "..", "../other.ipynb", "a/../other.ipynb", "a\\other.ipynb", "/other.ipynb"} {
		task.Content["notebook_locator"] = value
		if err := svc.FreezeContent(context.Background(), task); err == nil {
			t.Fatalf("unsafe path accepted: %q", value)
		}
	}
}

func TestNotebookFileIdentityIsOwnerBound(t *testing.T) {
	for _, value := range []string{
		"addp-infra://object/manager/tenant_7/notebooks/a.ipynb?type=object",
		"addp-infra://object/develop/tenant_8/notebooks/a.ipynb?type=object",
		"addp-infra://object/develop/tenant_7/executions/a.ipynb?type=object",
		"addp-infra://object/develop/tenant_7/notebooks/a.ipynb?type=prefix",
	} {
		if _, err := notebookRef(7, value); err == nil {
			t.Fatal("accepted foreign Notebook scope", value)
		}
	}
	for _, field := range []string{"notebook_path", "minio_path"} {
		if err := validateDevTaskContent("script", map[string]interface{}{field: "a.ipynb"}); err == nil {
			t.Fatal("legacy field accepted", field)
		}
	}
	svc := &DevTaskService{}
	_, err := svc.CreateDevTask(context.Background(), "", &models.CreateDevTaskRequest{DevType: "script", Content: models.DevTaskContent{"notebook_locator": "addp-infra://object/develop/tenant_7/notebooks/a.ipynb?type=object"}}, 7, 1)
	if err == nil {
		t.Fatal("generic create accepted external Notebook reference")
	}
}

func TestGenericTaskUpdateCannotIntroduceOrChangeNotebookReference(t *testing.T) {
	for _, current := range []string{`{"source":"print(1)"}`, `{"notebook_locator":"addp-infra://object/develop/tenant_7/notebooks/original.ipynb?type=object"}`} {
		t.Run(current, func(t *testing.T) {
			db := newNotebookBindingTestDB(t)
			if err := db.Exec(`INSERT INTO develop.dev_tasks (id,tenant_id,name,dev_type,content,execution_config,editor_layout,timeout,tags,status) VALUES (1,7,'script','script',CAST(? AS BLOB),CAST('{"engine_id":8}' AS BLOB),CAST('{}' AS BLOB),300,'{}','active')`, current).Error; err != nil {
				t.Fatal(err)
			}
			svc := NewDevTaskService(repository.NewDevTaskRepository(db), nil)
			_, err := svc.UpdateDevTask(context.Background(), "", 1, &models.UpdateDevTaskRequest{Content: models.DevTaskContent{"notebook_locator": "addp-infra://object/develop/tenant_7/notebooks/foreign.ipynb?type=object"}}, 7, 1)
			if !errors.Is(err, ErrNotebookFileIdentity) {
				t.Fatalf("generic update accepted a different file: %v", err)
			}
			stored, err := svc.GetDevTask(1, 7)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Content["notebook_locator"] == "addp-infra://object/develop/tenant_7/notebooks/foreign.ipynb?type=object" {
				t.Fatal("rejected update changed persisted file identity")
			}
		})
	}
}
