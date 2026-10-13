package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	return onlineEngineFixture(c.engine), nil
}

func (c fakeUploadSystemClient) GetEngineForTenant(_ context.Context, _ uint, _ uint) (*commonModels.Engine, error) {
	return onlineEngineFixture(c.engine), nil
}

type fakeUploadMetaClient struct {
	err    error
	bearer string
	ctx    context.Context
	opts   commonClient.MetaScanOptions
}

func (c *fakeUploadMetaClient) CreateManualScanRun(ctx context.Context, bearer string, opts commonClient.MetaScanOptions) (*commonClient.MetaScanRun, error) {
	c.bearer = bearer
	c.ctx = ctx
	c.opts = opts
	return &commonClient.MetaScanRun{ExecutionID: "scan-1", Status: "pending"}, c.err
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
	}, "current-user")
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
	if metaClient.bearer != "current-user" || metaClient.ctx == nil {
		t.Fatalf("scan did not use the current synchronous User request")
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
	}, "current-user")
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
	}, "current-user")
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
func (p *captureUploadPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	if p.objectCatalog {
		return plugin.ObjectCatalogModel()
	}
	return plugin.FileCatalogModel()
}
func (p *captureUploadPlugin) CreateContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.WriteOptions) (io.WriteCloser, error) {
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

func TestUploadFailsBeforeWritingWithoutSynchronousUserClient(t *testing.T) {
	for _, missingClient := range []bool{false, true} {
		t.Run(fmt.Sprint(missingClient), func(t *testing.T) {
			engineType := "upload_missing_identity"
			writer := &captureUploadPlugin{engineType: engineType, objectCatalog: true, writes: map[string]string{}}
			plugin.Register(writer)
			t.Cleanup(func() { plugin.Unregister(engineType) })
			var meta UploadMetaClient = &fakeUploadMetaClient{}
			token := ""
			if missingClient {
				meta = nil
				token = "current-user"
			}
			svc := NewUploadService(fakeUploadSystemClient{engine: &commonModels.Engine{ID: 9, EngineType: engineType, TenantID: uintPtr(7), LifecycleState: "active"}}, meta)
			_, err := svc.UploadFiles(context.Background(), &UploadRequest{TenantID: 7, TargetNodeLocator: "addp://engine/9/path/bucket/raw?type=prefix", Files: []UploadFile{{FileName: "a.txt", Reader: strings.NewReader("file")}}}, token)
			if err == nil || len(writer.writes) != 0 {
				t.Fatal("upload wrote files without a synchronous User identity/client")
			}
		})
	}
}

func TestUploadScanFailureDoesNotRetryOrReturnInternalExecution(t *testing.T) {
	engineType := "upload_scan_failure"
	writer := &captureUploadPlugin{engineType: engineType, objectCatalog: true, writes: map[string]string{}}
	plugin.Register(writer)
	t.Cleanup(func() { plugin.Unregister(engineType) })
	meta := &fakeUploadMetaClient{err: &commonClient.MetaScanSubmissionError{StatusCode: 403}}
	svc := NewUploadService(fakeUploadSystemClient{engine: &commonModels.Engine{ID: 9, EngineType: engineType, TenantID: uintPtr(7), LifecycleState: "active"}}, meta)
	result, err := svc.UploadFiles(context.Background(), &UploadRequest{TenantID: 7, TargetNodeLocator: "addp://engine/9/path/bucket/raw?type=prefix", Files: []UploadFile{{FileName: "a.txt", Reader: strings.NewReader("file")}}}, "current-user")
	if result != nil || !errors.Is(err, ErrUploadScanSubmission) || writer.writes["bucket/raw/a.txt"] != "file" || meta.bearer != "current-user" {
		t.Fatalf("invalid partial-write failure: %v", err)
	}
	encoded, _ := json.Marshal(&UploadResult{ScanExecutionID: "scan-1"})
	if strings.Contains(string(encoded), "scan_run") || strings.Contains(string(encoded), "execution_config") {
		t.Fatal("upload result contains an internal execution")
	}
}
