package service

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
)

type fakeImportSystemClient struct {
	engines []commonModels.Engine
}

func (c fakeImportSystemClient) GetEngine(engineID uint) (*commonModels.Engine, error) {
	for _, engine := range c.engines {
		if engine.ID == engineID {
			return &engine, nil
		}
	}
	return nil, nil
}

func (c fakeImportSystemClient) ListObjectStorages(uint) ([]commonModels.Engine, error) {
	return c.engines, nil
}

func TestBuildShapefileImportTaskConfigUsesEndpointSpec(t *testing.T) {
	service := &ImportService{minioBucket: "manager"}
	req := &ImportShapefileRequest{
		TargetEngineID: 7,
		Encoding:       "GBK",
	}

	config := service.buildShapefileImportTaskConfig(3, "temp/upload/roads.shp", req, "public", "roads")
	if _, ok := config["source_config"]; ok {
		t.Fatalf("config = %#v, must not contain legacy source_config", config)
	}
	if _, ok := config["target_config"]; ok {
		t.Fatalf("config = %#v, must not contain legacy target_config", config)
	}

	source := config["source"].(map[string]interface{})
	sourceEngine := source["engine"].(map[string]interface{})
	if sourceEngine["id"] != uint(3) {
		t.Fatalf("source engine = %#v, want id 3", sourceEngine)
	}
	if _, ok := sourceEngine["type"]; ok {
		t.Fatalf("source engine = %#v, must not declare engine type", sourceEngine)
	}
	if source["representation"] != "encoded" || source["format"] != "shapefile" {
		t.Fatalf("source = %#v, want encoded shapefile", source)
	}
	sourceResource := source["resource"].(map[string]interface{})
	sourcePath := sourceResource["path"].(map[string]interface{})
	if sourceResource["kind"] != "object" || sourcePath["bucket"] != "manager" || sourcePath["path"] != "temp/upload/roads.shp" {
		t.Fatalf("source resource = %#v, want manager/temp/upload/roads.shp object", sourceResource)
	}
	sourceOptions := source["options"].(map[string]interface{})
	if sourceOptions["encoding"] != "GBK" {
		t.Fatalf("source options = %#v, want GBK encoding", sourceOptions)
	}

	target := config["target"].(map[string]interface{})
	targetEngine := target["engine"].(map[string]interface{})
	if targetEngine["id"] != uint(7) {
		t.Fatalf("target engine = %#v, want id 7", targetEngine)
	}
	targetResource := target["resource"].(map[string]interface{})
	targetPath := targetResource["path"].(map[string]interface{})
	if targetResource["kind"] != "native_table" || targetPath["schema"] != "public" || targetPath["table"] != "roads" {
		t.Fatalf("target resource = %#v, want public.roads native table", targetResource)
	}
}

func TestResolveImportSourceEngineMatchesConfiguredObjectStore(t *testing.T) {
	service := &ImportService{
		minioEndpoint:  "http://business-minio:9000",
		minioBucket:    "manager",
		minioAccessKey: "minioadmin",
		systemClient: fakeImportSystemClient{engines: []commonModels.Engine{
			{
				ID:         9,
				EngineType: "s3",
				IsActive:   true,
				ConnectionInfo: commonModels.ConnectionInfo{
					"endpoint":   "business-minio:9000",
					"access_key": "minioadmin",
					"bucket":     "manager",
				},
			},
		}},
	}

	id, err := service.resolveImportSourceEngine(1)
	if err != nil {
		t.Fatalf("resolveImportSourceEngine() error = %v", err)
	}
	if id != 9 {
		t.Fatalf("resolved engine = %d, want 9", id)
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
	if !strings.Contains(err.Error(), "same basename") {
		t.Fatalf("extractShapefileZip() error = %v, want same basename message", err)
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
	if !strings.Contains(err.Error(), "same basename") {
		t.Fatalf("extractShapefileZip() error = %v, want same basename message", err)
	}
}

func TestExplicitImportSourceEngineRequiresActiveEngine(t *testing.T) {
	service := &ImportService{
		sourceEngineID:       4,
		sourceEngineExplicit: true,
		systemClient: fakeImportSystemClient{engines: []commonModels.Engine{
			{ID: 4, EngineType: "s3", IsActive: false},
		}},
	}

	_, err := service.resolveImportSourceEngine(1)
	if err == nil {
		t.Fatal("resolveImportSourceEngine() error = nil, want inactive engine error")
	}
}

func TestResolveExplicitImportSourceEngineWithoutSystemClient(t *testing.T) {
	service := &ImportService{
		sourceEngineID:       4,
		sourceEngineExplicit: true,
	}

	id, err := service.resolveImportSourceEngine(1)
	if err != nil {
		t.Fatalf("resolveImportSourceEngine() error = %v", err)
	}
	if id != 4 {
		t.Fatalf("resolved engine = %d, want 4", id)
	}
}

func TestResolveImportSourceEngineRequiresSystemClientWithoutExplicitEngine(t *testing.T) {
	service := &ImportService{}

	_, err := service.resolveImportSourceEngine(1)
	if err == nil {
		t.Fatal("resolveImportSourceEngine() error = nil, want missing System integration error")
	}
}

func TestConnectionStringHandlesContextStringer(t *testing.T) {
	got := connectionString(map[string]interface{}{"value": context.Canceled}, "value")
	if got != context.Canceled.Error() {
		t.Fatalf("connectionString() = %q, want %q", got, context.Canceled.Error())
	}
}
