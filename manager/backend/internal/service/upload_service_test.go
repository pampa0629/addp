package service

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

type fakeUploadSystemClient struct {
	engine *commonModels.Engine
}

func (c fakeUploadSystemClient) GetEngine(engineID uint) (*commonModels.Engine, error) {
	return c.engine, nil
}

func (c fakeUploadSystemClient) GetEngineForTenant(_ context.Context, _ uint, _ uint) (*commonModels.Engine, error) {
	return c.engine, nil
}

type fakeUploadMetaClient struct {
	tenantID *uint
	opts     commonClient.MetaScanOptions
}

func (c *fakeUploadMetaClient) CreateManualScanRunForTenant(tenantID uint, opts commonClient.MetaScanOptions) (*commonExecution.TaskExecution, error) {
	c.tenantID = &tenantID
	c.opts = opts
	return &commonExecution.TaskExecution{ExecutionID: "scan-1"}, nil
}

func TestUploadFilesWritesMultipleObjectsAndSubmitsScan(t *testing.T) {
	engineType := "manager_upload_test_object"
	writerPlugin := &captureUploadPlugin{engineType: engineType, objectCatalog: true, writes: map[string]string{}}
	plugin.Register(writerPlugin)
	t.Cleanup(func() { plugin.Unregister(engineType) })

	metaClient := &fakeUploadMetaClient{}
	svc := NewUploadService(fakeUploadSystemClient{engine: &commonModels.Engine{
		ID:             9,
		EngineType:     engineType,
		ConnectionInfo: map[string]interface{}{},
		TenantID:       uintPtr(7),
		LifecycleState: "active",
	}}, metaClient)

	result, err := svc.UploadFiles(context.Background(), &UploadRequest{
		TargetNodeLocator: "addp://engine/9/path/bucket/raw?type=prefix&node_id=5",
		TenantID:          7,
		Files: []UploadFile{
			{FileName: "roads.shp", Reader: strings.NewReader("shape")},
			{FileName: "roads.dbf", Reader: strings.NewReader("attrs")},
		},
	})
	if err != nil {
		t.Fatalf("UploadFiles() error = %v", err)
	}
	if got := writerPlugin.writes["bucket/raw/roads.shp"]; got != "shape" {
		t.Fatalf("roads.shp write = %q", got)
	}
	if got := writerPlugin.writes["bucket/raw/roads.dbf"]; got != "attrs" {
		t.Fatalf("roads.dbf write = %q", got)
	}
	if result.ScanExecutionID != "scan-1" {
		t.Fatalf("scan execution id = %q, want scan-1", result.ScanExecutionID)
	}
	if len(result.Files) != 2 || result.Files[0].Locator != "addp://engine/9/path/bucket/raw/roads.shp?type=object" {
		t.Fatalf("upload files result = %#v", result.Files)
	}
	if metaClient.tenantID == nil || *metaClient.tenantID != 7 {
		t.Fatalf("tenant id = %#v, want 7", metaClient.tenantID)
	}
	if len(metaClient.opts.CatalogPaths) != 0 {
		t.Fatalf("scan catalog paths = %#v, want empty", metaClient.opts.CatalogPaths)
	}
	wantRefGroups := []commonClient.MetaScanRefGroup{
		{
			Primary: "bucket/raw/roads.shp",
			Refs: []commonClient.MetaScanRef{
				{Path: "bucket/raw/roads.dbf", Role: "sidecar", Required: false, Primary: false},
				{Path: "bucket/raw/roads.shp", Role: "main", Required: true, Primary: true},
			},
		},
	}
	if !reflect.DeepEqual(metaClient.opts.RefGroups, wantRefGroups) {
		t.Fatalf("scan ref groups = %#v, want %#v", metaClient.opts.RefGroups, wantRefGroups)
	}
	if metaClient.opts.EngineID != 9 || metaClient.opts.Source != commonExecution.ModuleManager || !metaClient.opts.Force {
		t.Fatalf("scan opts = %#v", metaClient.opts)
	}
}

func TestUploadFilesSubmitsSeparateRefGroupsByBaseName(t *testing.T) {
	engineType := "manager_upload_test_object_groups"
	writerPlugin := &captureUploadPlugin{engineType: engineType, objectCatalog: true, writes: map[string]string{}}
	plugin.Register(writerPlugin)
	t.Cleanup(func() { plugin.Unregister(engineType) })

	metaClient := &fakeUploadMetaClient{}
	svc := NewUploadService(fakeUploadSystemClient{engine: &commonModels.Engine{
		ID:             9,
		EngineType:     engineType,
		ConnectionInfo: map[string]interface{}{},
		LifecycleState: "active",
	}}, metaClient)

	_, err := svc.UploadFiles(context.Background(), &UploadRequest{
		TargetNodeLocator: "addp://engine/9/path/bucket/raw?type=prefix&node_id=5",
		Files: []UploadFile{
			{FileName: "roads.shp", Reader: strings.NewReader("shape")},
			{FileName: "roads.dbf", Reader: strings.NewReader("attrs")},
			{FileName: "readme.pdf", Reader: strings.NewReader("doc")},
		},
	})
	if err != nil {
		t.Fatalf("UploadFiles() error = %v", err)
	}
	wantRefGroups := []commonClient.MetaScanRefGroup{
		{
			Primary: "bucket/raw/readme.pdf",
			Refs: []commonClient.MetaScanRef{
				{Path: "bucket/raw/readme.pdf", Role: "main", Required: true, Primary: true},
			},
		},
		{
			Primary: "bucket/raw/roads.shp",
			Refs: []commonClient.MetaScanRef{
				{Path: "bucket/raw/roads.dbf", Role: "sidecar", Required: false, Primary: false},
				{Path: "bucket/raw/roads.shp", Role: "main", Required: true, Primary: true},
			},
		},
	}
	if !reflect.DeepEqual(metaClient.opts.RefGroups, wantRefGroups) {
		t.Fatalf("scan ref groups = %#v, want %#v", metaClient.opts.RefGroups, wantRefGroups)
	}
	if len(metaClient.opts.CatalogPaths) != 0 {
		t.Fatalf("scan catalog paths = %#v, want empty", metaClient.opts.CatalogPaths)
	}
}

func uintPtr(value uint) *uint {
	return &value
}

func TestUploadFilesRejectsDatabaseNode(t *testing.T) {
	svc := NewUploadService(fakeUploadSystemClient{}, nil)
	_, err := svc.UploadFiles(context.Background(), &UploadRequest{
		TargetNodeLocator: "addp://engine/9/path/public?type=schema&node_id=5",
		Files:             []UploadFile{{FileName: "roads.shp", Reader: strings.NewReader("shape")}},
	})
	if err != ErrUploadTargetUnsupported {
		t.Fatalf("UploadFiles() error = %v, want ErrUploadTargetUnsupported", err)
	}
}

type captureUploadPlugin struct {
	engineType    string
	objectCatalog bool
	writes        map[string]string
}

func (p *captureUploadPlugin) Type() string         { return p.engineType }
func (p *captureUploadPlugin) DisplayName() string  { return p.engineType }
func (p *captureUploadPlugin) EngineOrigin() string { return "general" }
func (p *captureUploadPlugin) DefaultPort() int     { return 0 }
func (p *captureUploadPlugin) RequiredFields() []string {
	return nil
}
func (p *captureUploadPlugin) SensitiveFields() []string {
	return nil
}
func (p *captureUploadPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *captureUploadPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *captureUploadPlugin) Capabilities() plugin.EngineCapabilities {
	if p.objectCatalog {
		return plugin.NewObjectCapabilities(p.engineType)
	}
	return plugin.NewFileCapabilities(p.engineType)
}
func (p *captureUploadPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *captureUploadPlugin) CatalogModel() plugin.CatalogModelSpec {
	if p.objectCatalog {
		return plugin.ObjectCatalogModel()
	}
	return plugin.FileCatalogModel()
}
func (p *captureUploadPlugin) CreateContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.WriteOptions) (io.WriteCloser, error) {
	return &captureUploadWriter{path: strings.Trim(path.StringPath(), "/"), writes: p.writes}, nil
}

type captureUploadWriter struct {
	path   string
	writes map[string]string
	buf    bytes.Buffer
}

func (w *captureUploadWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *captureUploadWriter) Close() error {
	w.writes[w.path] = w.buf.String()
	return nil
}
