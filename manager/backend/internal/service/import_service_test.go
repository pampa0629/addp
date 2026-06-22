package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/format"
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

func TestImportShapefileCleansQuickViewOptimizationsBeforeUpload(t *testing.T) {
	cleaner := &recordingImportOptimizationCleaner{}
	minioClient, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds:  credentials.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("new minio client: %v", err)
	}
	service := &ImportService{
		minioClient:                  minioClient,
		minioBucket:                  "manager",
		quickViewOptimizationCleaner: cleaner,
	}
	req := &ImportShapefileRequest{
		Files:          completeImportShapefileUploadFiles("roads"),
		TargetEngineID: 7,
		TargetSchema:   "public",
		TargetTable:    "roads",
		TenantID:       3,
	}

	_, err = service.ImportShapefile(context.Background(), req)
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

func TestImportShapefileStopsWhenQuickViewOptimizationCleanupFails(t *testing.T) {
	cleaner := &recordingImportOptimizationCleaner{err: errors.New("cleanup failed")}
	transferClient := &recordingImportTransferClient{}
	service := &ImportService{
		minioBucket:                  "manager",
		transferClient:               transferClient,
		quickViewOptimizationCleaner: cleaner,
	}
	req := &ImportShapefileRequest{
		Files:          completeImportShapefileUploadFiles("roads"),
		TargetEngineID: 7,
		TargetSchema:   "public",
		TargetTable:    "roads",
		TenantID:       3,
	}

	_, err := service.ImportShapefile(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "cleanup manager quick view optimization") {
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

func (c *recordingImportTransferClient) CreateTask(*client.CreateTransferTaskRequest) (*client.TransferTaskResponse, error) {
	c.createCalls++
	return &client.TransferTaskResponse{ID: 1}, nil
}

func (c *recordingImportTransferClient) TriggerTask(taskID, tenantID uint) (*client.TriggerTaskResponse, error) {
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
