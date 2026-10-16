package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestBuildShapefileImportTaskConfigUsesEndpointSpec(t *testing.T) {
	service := &ImportService{minioBucket: "manager"}
	req := &ImportShapefileRequest{
		TargetEngineID: 7,
		Encoding:       "GBK",
	}

	config := service.buildShapefileImportTaskConfig("tenant_1/import/20260619/upload/roads.shp", req, "public", "roads", false)
	if _, ok := config["source_config"]; ok {
		t.Fatalf("config = %#v, must not contain legacy source_config", config)
	}
	if _, ok := config["target_config"]; ok {
		t.Fatalf("config = %#v, must not contain legacy target_config", config)
	}

	source := config["source"].(map[string]interface{})
	if source["representation"] != "encoded" || source["format"] != "shapefile" {
		t.Fatalf("source = %#v, want encoded shapefile", source)
	}
	if source["locator"] != "addp-infra://minio/manager/tenant_1/import/20260619/upload/roads.shp?type=object" {
		t.Fatalf("source locator = %#v, want infra manager object locator", source["locator"])
	}
	sourceOptions := source["options"].(map[string]interface{})
	if sourceOptions["encoding"] != "GBK" {
		t.Fatalf("source options = %#v, want GBK encoding", sourceOptions)
	}

	target := config["target"].(map[string]interface{})
	if target["parent_locator"] != "addp://engine/7/path/public?type=schema" || target["name"] != "roads" {
		t.Fatalf("target = %#v, want public.roads native table locator endpoint", target)
	}
}

func TestImportShapefileCleansVectorMaterializedViewsBeforeUpload(t *testing.T) {
	cleaner := &recordingImportOptimizationCleaner{}
	minioClient, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds:  credentials.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("new minio client: %v", err)
	}
	service := &ImportService{
		minioClient:                   minioClient,
		minioBucket:                   "manager",
		vectorMaterializedViewCleaner: cleaner,
	}
	req := &ImportShapefileRequest{
		Files:          completeImportShapefileUploadFiles("roads"),
		TargetEngineID: 7,
		TargetSchema:   "public",
		TargetTable:    "roads",
		TenantID:       3,
		UploaderID:     31,
	}

	_, err = service.ImportShapefile(context.Background(), req, "current-user")
	if err == nil || !strings.Contains(err.Error(), "failed to upload") {
		t.Fatalf("ImportShapefile() error = %v, want upload failure after cleanup", err)
	}
	if cleaner.calls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleaner.calls)
	}
	if cleaner.tenantID != 3 || cleaner.engineID != 7 || cleaner.schema != "public" || cleaner.table != "roads" {
		t.Fatalf("cleanup args = tenant:%d engine:%d %s.%s", cleaner.tenantID, cleaner.engineID, cleaner.schema, cleaner.table)
	}
}

func TestImportShapefileStopsWhenVectorMaterializedViewCleanupFails(t *testing.T) {
	cleaner := &recordingImportOptimizationCleaner{err: errors.New("cleanup failed")}
	transferClient := &recordingImportTransferClient{}
	service := &ImportService{
		minioBucket:                   "manager",
		transferClient:                transferClient,
		vectorMaterializedViewCleaner: cleaner,
	}
	req := &ImportShapefileRequest{
		Files:          completeImportShapefileUploadFiles("roads"),
		TargetEngineID: 7,
		TargetSchema:   "public",
		TargetTable:    "roads",
		TenantID:       3,
		UploaderID:     31,
	}

	_, err := service.ImportShapefile(context.Background(), req, "current-user")
	if err == nil || !strings.Contains(err.Error(), "cleanup manager vector materialized view") {
		t.Fatalf("ImportShapefile() error = %v, want cleanup failure", err)
	}
	if transferClient.createCalls != 0 || transferClient.triggerCalls != 0 {
		t.Fatalf("transfer calls = create:%d trigger:%d, want none", transferClient.createCalls, transferClient.triggerCalls)
	}
}

func TestBuildShapefileImportTaskConfigUsesTargetNodeLocator(t *testing.T) {
	service := &ImportService{minioBucket: "manager"}
	req := &ImportShapefileRequest{
		TargetEngineID:    7,
		TargetNodeLocator: "addp://engine/7/path/app?type=database&node_id=3",
	}

	config := service.buildShapefileImportTaskConfig("tenant_1/import/20260619/upload/roads.shp", req, "app", "roads", false)
	target := config["target"].(map[string]interface{})
	if target["parent_locator"] != req.TargetNodeLocator {
		t.Fatalf("target parent_locator = %#v, want target node locator", target["parent_locator"])
	}
}

func TestBuildShapefileImportTaskConfigSkipsEncodingWhenCPGExists(t *testing.T) {
	service := &ImportService{minioBucket: "manager"}
	req := &ImportShapefileRequest{
		TargetEngineID: 7,
		Encoding:       "GBK",
	}

	config := service.buildShapefileImportTaskConfig("tenant_1/import/20260619/upload/roads.shp", req, "public", "roads", true)
	source := config["source"].(map[string]interface{})
	if _, ok := source["options"]; ok {
		t.Fatalf("source = %#v, must not contain encoding options when cpg exists", source)
	}
}

func TestManagerImportStagingPrefixUsesSingleDateDirectory(t *testing.T) {
	got := managerImportStagingPrefix(7, "upload-uuid", time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC))
	want := "tenant_7/import/20260619/upload-uuid/"
	if got != want {
		t.Fatalf("managerImportStagingPrefix() = %q, want %q", got, want)
	}
}

func TestExtractShapefileZipRequiresSameBasenameComponents(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"roads.shp", "other.dbf", "roads.shx"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	_, err := extractShapefileZip(buf.Bytes())
	if err == nil {
		t.Fatal("extractShapefileZip() error = nil, want missing same-basename component set")
	}
	if !errors.Is(err, ErrImportZipBasenameMismatch) {
		t.Fatalf("extractShapefileZip() error = %v, want ErrImportZipBasenameMismatch", err)
	}
}

func TestExtractShapefileZipRejectsMultipleComponentSets(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"roads.shp", "roads.dbf", "roads.shx", "rails.shp", "rails.dbf", "rails.shx"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	_, err := extractShapefileZip(buf.Bytes())
	if err == nil {
		t.Fatal("extractShapefileZip() error = nil, want multiple component sets error")
	}
	if !errors.Is(err, ErrImportZipBasenameMismatch) {
		t.Fatalf("extractShapefileZip() error = %v, want ErrImportZipBasenameMismatch", err)
	}
}

func TestShapefileImportExtensionsComeFromFormatSpecs(t *testing.T) {
	t.Parallel()

	allowed := shapefileImportAllowedExtensions()
	for _, ext := range []string{".shp", ".shx", ".dbf", ".prj", ".qpj", ".cpg"} {
		if !allowed[ext] {
			t.Fatalf("allowed extensions = %#v, missing %s", allowed, ext)
		}
	}

	required := map[string]bool{}
	for _, ext := range shapefileImportRequiredExtensions() {
		required[format.NormalizeExtension(ext)] = true
	}
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		if !required[ext] {
			t.Fatalf("required extensions = %#v, missing %s", required, ext)
		}
	}
	for _, ext := range []string{".prj", ".qpj", ".cpg"} {
		if required[ext] {
			t.Fatalf("required extensions = %#v, should not require %s", required, ext)
		}
	}
}

func TestExtractShapefileZipKeepsQPJSidecar(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"roads.shp", "roads.dbf", "roads.shx", "roads.qpj"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	files, err := extractShapefileZip(buf.Bytes())
	if err != nil {
		t.Fatalf("extractShapefileZip() error = %v", err)
	}
	if _, ok := files["roads.qpj"]; !ok {
		t.Fatalf("files = %#v, missing qpj sidecar", files)
	}
}

func TestHasShapefileCPG(t *testing.T) {
	t.Parallel()

	if !hasShapefileCPG(map[string][]byte{"roads.CPG": []byte("GBK")}) {
		t.Fatal("hasShapefileCPG() = false, want true")
	}
	if hasShapefileCPG(map[string][]byte{"roads.prj": []byte("wkt")}) {
		t.Fatal("hasShapefileCPG() = true, want false")
	}
}

func TestManagerInfraMinioObjectLocatorNormalizesPath(t *testing.T) {
	got := managerInfraMinioObjectLocator("manager", "/tenant_7/import/20260619/upload/roads.shp")
	want := "addp-infra://minio/manager/tenant_7/import/20260619/upload/roads.shp?type=object"
	if got != want {
		t.Fatalf("managerInfraMinioObjectLocator() = %q, want %q", got, want)
	}
}

type recordingImportOptimizationCleaner struct {
	calls    int
	tenantID uint
	engineID uint
	schema   string
	table    string
	err      error
}

func (c *recordingImportOptimizationCleaner) DeleteResultsForSourceTable(_ context.Context, tenantID uint, engineID uint, schema string, table string) error {
	c.calls++
	c.tenantID = tenantID
	c.engineID = engineID
	c.schema = schema
	c.table = table
	return c.err
}

type recordingImportTransferClient struct {
	createCalls  int
	triggerCalls int
}

func (c *recordingImportTransferClient) CreateTask(context.Context, string, *client.CreateTransferTaskRequest) (*client.TransferTaskResponse, error) {
	c.createCalls++
	return &client.TransferTaskResponse{ID: 1}, nil
}

func (c *recordingImportTransferClient) TriggerTask(ctx context.Context, userBearer string, taskID uint) (*client.TriggerTaskResponse, error) {
	c.triggerCalls++
	return &client.TriggerTaskResponse{ID: taskID, ExecutionID: "execution-1", Status: "running"}, nil
}

func completeImportShapefileUploadFiles(base string) []ImportUploadFile {
	return []ImportUploadFile{
		{FileName: base + ".shp", Content: []byte("shp")},
		{FileName: base + ".shx", Content: []byte("shx")},
		{FileName: base + ".dbf", Content: []byte("dbf")},
	}
}

// Exercise the real Common HTTP client after staging, so a stale Manager task
// contract cannot pass merely because a fake Transfer client accepts any map.
func TestImportShapefileSubmitsCurrentBoundedSnapshotContract(t *testing.T) {
	uploads := map[string][]byte{}
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected storage method %s", r.Method)
			w.WriteHeader(400)
			return
		}
		var body io.Reader = r.Body
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-AWS4-") {
			body = httputil.NewChunkedReader(body)
		}
		data, err := io.ReadAll(body)
		if err != nil {
			t.Error(err)
		}
		uploads[r.URL.Path] = data
		w.Header().Set("ETag", `"fixture-etag"`)
		w.WriteHeader(200)
	}))
	defer storage.Close()
	store := &recordingImportSourceStore{}
	calls := 0
	transfer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer current-user" || r.Method != http.MethodPost {
			t.Error("Transfer did not receive the current User POST")
			w.WriteHeader(403)
			return
		}
		if len(uploads) != 3 {
			t.Errorf("staged files = %d, want 3 before submission", len(uploads))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/transfer/task-definitions":
			if store.source == nil || store.source.UploaderID != 31 {
				t.Error("source fact must be saved before task creation")
			}
			var request client.CreateTransferTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			raw, _ := json.Marshal(request.Config)
			var config client.TransferExecutionConfig
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&config); err != nil {
				t.Errorf("invalid current Transfer contract: %v", err)
				w.WriteHeader(400)
				return
			}
			if config.Runtime.Boundary != "bounded" || config.Load.Mode != "snapshot" || config.Target.Policy["apply_mode"] != "replace" {
				t.Errorf("unexpected execution/write semantics: %#v", config)
				w.WriteHeader(400)
				return
			}
			if _, ok := config.Target.Policy["write_mode"]; ok {
				t.Error("legacy write_mode submitted")
			}
			if request.TaskType != "sync" || request.AutoScanMetadata == nil || !*request.AutoScanMetadata || request.TenantID != 0 {
				t.Error("task identity, automatic scan or trusted tenant contract differs")
			}
			if config.Source.Format != "shapefile" || config.Source.Representation != "encoded" || !strings.HasPrefix(config.Source.Locator, "addp-infra://minio/manager/tenant_7/import/") || config.Target.ParentLocator != "addp://engine/8/path/public?type=schema" || config.Target.Name != "roads" {
				t.Errorf("wrong submitted source/target: %#v", config)
			}
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":42}`))
		case "/api/v1/transfer/task-definitions/42/start":
			if store.taskID != 42 {
				t.Error("source must be bound before execution starts")
			}
			if calls != 2 {
				t.Error("task must be created exactly once before start")
			}
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"execution_id":"694c4422-56b1-45f5-b367-9f7525760a23","status":"pending"}`))
		default:
			t.Errorf("unexpected Transfer path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer transfer.Close()
	minioClient, err := minio.New(strings.TrimPrefix(storage.URL, "http://"), &minio.Options{Region: "us-east-1", Creds: credentials.NewStaticV4("fixture", "fixture-secret", "")})
	if err != nil {
		t.Fatal(err)
	}
	service := NewImportService(minioClient, "manager", client.NewTransferClient(transfer.URL, nil), nil, store)
	result, err := service.ImportShapefile(context.Background(), &ImportShapefileRequest{Files: completeImportShapefileUploadFiles("roads"), TargetEngineID: 8, TargetSchema: "public", TargetTable: "roads", TenantID: 7, UploaderID: 31}, "current-user")
	if err != nil {
		t.Fatal(err)
	}
	if store.source == nil || store.source.UploaderID != 31 || store.source.TenantID != 7 || store.taskID != 42 {
		t.Fatal("upload source was not saved and bound")
	}
	if calls != 2 || result.TransferTaskID != 42 || result.TransferExecutionID != "694c4422-56b1-45f5-b367-9f7525760a23" {
		t.Fatalf("submission result = %#v, calls=%d", result, calls)
	}
	for _, file := range completeImportShapefileUploadFiles("roads") {
		key := "/manager/" + managerImportStagingPrefix(7, result.UploadUUID, time.Now()) + file.FileName
		if !bytes.Equal(uploads[key], file.Content) {
			t.Errorf("staged content differs for %s", file.FileName)
		}
	}
}

type recordingImportSourceStore struct {
	source  *models.ImportSource
	taskID  uint
	bindErr error
}

func (s *recordingImportSourceStore) Create(_ context.Context, source *models.ImportSource) error {
	s.source = source
	return nil
}
func (s *recordingImportSourceStore) BindTask(_ context.Context, id string, tenant, task uint) error {
	if s.bindErr != nil {
		return s.bindErr
	}
	if s.source == nil || s.source.UploadUUID != id || s.source.TenantID != tenant {
		return errors.New("wrong source binding")
	}
	s.taskID = task
	return nil
}
func (s *recordingImportSourceStore) Verify(context.Context, uint, client.ManagerImportSourceRequest) (*client.ManagerImportSource, error) {
	return nil, errors.New("not implemented")
}
