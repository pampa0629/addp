package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/addp/develop/backend/internal/models"
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
	svc := &NotebookExecutionService{minioClient: client}
	original := models.DevTaskContent{"engine_id": 9, "notebook_content_sha256": "untrusted-client-value"}
	task := &models.DevTask{TenantID: 7, DevType: "script", Content: models.DevTaskContent{"notebook_path": "analysis.ipynb"}, ExecutionConfig: original}
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
		task.Content["notebook_path"] = value
		if err := svc.FreezeContent(context.Background(), task); err == nil {
			t.Fatalf("unsafe path accepted: %q", value)
		}
	}
}
