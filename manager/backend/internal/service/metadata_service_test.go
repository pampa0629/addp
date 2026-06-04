package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/minio"
	_ "github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
)

func TestMetadataServiceRefreshItemUsesMetaClient(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotHeader string
	var gotTenant string
	var gotPayload map[string]interface{}
	var decodeErr error
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Internal-API-Key")
		gotTenant = r.Header.Get("X-Tenant-ID")
		decodeErr = json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","message":"ok","catalog_nodes_scanned":2,"items_scanned":1,"fields_scanned":7,"duration_ms":33,"started_at":"2026-05-20T00:00:00Z","extraction":{"documents":1,"extracted":1,"unsupported":0,"failed":0,"indexed":1,"index_failed":0}}`))
	}))
	defer metaServer.Close()

	metaClient := client.NewMetaClientWithInternalKey(metaServer.URL, "internal-key")
	service := &MetadataService{metaClient: metaClient}
	tenantID := uint(11)
	resp, err := service.RefreshItem(t.Context(), &tenantID, 26, &models.MetaManualScanRequest{ItemID: 1831})
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if decodeErr != nil {
		t.Fatalf("decode payload: %v", decodeErr)
	}
	if gotPath != "/api/v1/meta/items/1831/refresh" {
		t.Fatalf("path = %q, want /api/v1/meta/items/1831/refresh", gotPath)
	}
	if gotHeader != "internal-key" || gotTenant != "11" {
		t.Fatalf("auth headers = key:%q tenant:%q", gotHeader, gotTenant)
	}
	if gotPayload["engine_id"] != float64(26) {
		t.Fatalf("payload = %#v", gotPayload)
	}
	if _, ok := gotPayload["item_id"]; ok {
		t.Fatalf("payload should not include item_id in body: %#v", gotPayload)
	}
	if gotPayload["force"] != true {
		t.Fatalf("scan options = %#v", gotPayload)
	}
	if resp.ItemsScanned != 1 || resp.FieldsScanned != 7 || resp.Status != "success" {
		t.Fatalf("resp = %#v", resp)
	}
	if resp.Extraction == nil || resp.Extraction.Documents != 1 || resp.Extraction.Indexed != 1 {
		t.Fatalf("extraction = %#v", resp.Extraction)
	}
}

func TestStreamStorageRefPathSupportsFileAndObjectCatalogs(t *testing.T) {
	t.Parallel()

	filePath, displayPath, err := streamStorageRefPath("nfs", 26, "raw/book.epub")
	if err != nil {
		t.Fatalf("file stream path error = %v", err)
	}
	if got := filePath.StringPath(); got != "raw/book.epub" || displayPath != "raw/book.epub" {
		t.Fatalf("file path/display = %q/%q, want raw/book.epub", got, displayPath)
	}

	objectPath, displayPath, err := streamStorageRefPath("minio", 9, "addp/raw/book.epub")
	if err != nil {
		t.Fatalf("object stream path error = %v", err)
	}
	if got := objectPath.StringPath(); got != "addp/raw/book.epub" || displayPath != "addp/raw/book.epub" {
		t.Fatalf("object path/display = %q/%q, want addp/raw/book.epub", got, displayPath)
	}
}

func TestStreamStorageRefPathRejectsInvalidStorageRefs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		engineType string
		storageRef string
	}{
		{name: "empty", engineType: "nfs", storageRef: "///"},
		{name: "object without key", engineType: "minio", storageRef: "bucket"},
		{name: "object without bucket", engineType: "minio", storageRef: "/file.txt"},
		{name: "unsupported engine", engineType: "postgresql", storageRef: "schema/table"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := streamStorageRefPath(tc.engineType, 1, tc.storageRef); err == nil {
				t.Fatalf("streamStorageRefPath(%q, %q) expected error", tc.engineType, tc.storageRef)
			}
		})
	}
}

func TestParseStorageRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		header      string
		size        int64
		wantOffset  int64
		wantLength  int64
		wantRange   string
		wantNoRange bool
	}{
		{name: "full content", header: "", size: 100, wantLength: 100, wantNoRange: true},
		{name: "closed range", header: "bytes=10-19", size: 100, wantOffset: 10, wantLength: 10, wantRange: "bytes 10-19/100"},
		{name: "open ended range", header: "bytes=95-", size: 100, wantOffset: 95, wantLength: 5, wantRange: "bytes 95-99/100"},
		{name: "suffix range", header: "bytes=-7", size: 100, wantOffset: 93, wantLength: 7, wantRange: "bytes 93-99/100"},
		{name: "range clipped to size", header: "bytes=90-999", size: 100, wantOffset: 90, wantLength: 10, wantRange: "bytes 90-99/100"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, length, contentRange, err := parseStorageRange(tc.header, tc.size)
			if err != nil {
				t.Fatalf("parseStorageRange() error = %v", err)
			}
			if tc.wantNoRange {
				if opts.Offset != 0 || opts.Length != 0 {
					t.Fatalf("full content options = %#v, want zero read options", opts)
				}
				if length != tc.wantLength || contentRange != "" {
					t.Fatalf("full content length/range = %d/%q, want %d/empty", length, contentRange, tc.wantLength)
				}
				return
			}
			if opts.Offset != tc.wantOffset || opts.Length != tc.wantLength {
				t.Fatalf("read options = %#v, want offset=%d length=%d", opts, tc.wantOffset, tc.wantLength)
			}
			if length != tc.wantLength || contentRange != tc.wantRange {
				t.Fatalf("length/range = %d/%q, want %d/%q", length, contentRange, tc.wantLength, tc.wantRange)
			}
		})
	}
}

func TestParseStorageRangeRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()

	for _, header := range []string{
		"items=0-1",
		"bytes=",
		"bytes=10",
		"bytes=10-1",
		"bytes=100-101",
		"bytes=0-1,5-6",
		"bytes=-0",
		"bytes=a-b",
	} {
		header := header
		t.Run(header, func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseStorageRange(header, 100)
			if !errors.Is(err, ErrInvalidRange) {
				t.Fatalf("parseStorageRange(%q) err = %v, want ErrInvalidRange", header, err)
			}
		})
	}
}

func TestResolveStorageDownloadPlanUsesSingleStorageRef(t *testing.T) {
	t.Parallel()

	svc := &MetadataService{
		systemClient: testSystemClient(t, 9, "nfs", nil),
	}
	plan, err := svc.ResolveStorageDownloadPlan(t.Context(), 9, "raw/test.parquet", nil)
	if err != nil {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v", err)
	}
	if plan.Kind != models.DownloadKindStream || plan.FileName != "test.parquet" || len(plan.Refs) != 1 {
		t.Fatalf("plan = %#v, want stream test.parquet with one ref", plan)
	}
	if plan.Refs[0].StorageRef != "raw/test.parquet" {
		t.Fatalf("ref = %#v", plan.Refs[0])
	}
}

func TestResolveStorageDownloadPlanRejectsKnownMultiFormatWithoutMetaItem(t *testing.T) {
	t.Parallel()

	svc := &MetadataService{
		systemClient: testSystemClient(t, 9, "nfs", nil),
	}
	_, err := svc.ResolveStorageDownloadPlan(t.Context(), 9, "shp/farmland.shp", nil)
	if !errors.Is(err, ErrDownloadNotSupported) {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v, want ErrDownloadNotSupported", err)
	}
}

func TestResolveStorageDownloadPlanUsesMetaMultiRefs(t *testing.T) {
	t.Parallel()

	svc := &MetadataService{
		systemClient: testSystemClient(t, 9, "minio", nil),
		metaClient: testMetaItemClient(t, `{
			"id": 1,
			"engine_id": 9,
			"item_type": "object",
			"name": "roads.shp",
			"full_name": "bucket/roads/roads.shp",
			"attributes": {
				"item": {
					"layout": "multi",
					"format": "shapefile",
					"refs": [
						{"path":"bucket/roads/roads.shp","role":"main","required":true,"primary":true},
						{"path":"bucket/roads/roads.shx","role":"index","required":true},
						{"path":"bucket/roads/roads.dbf","role":"attributes","required":true},
						{"path":"bucket/roads/roads.prj","role":"projection"}
					]
				}
			}
		}`),
	}
	tenantID := uint(11)
	plan, err := svc.ResolveStorageDownloadPlan(t.Context(), 9, "bucket/roads/roads.shp", &tenantID)
	if err != nil {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v", err)
	}
	if plan.Kind != models.DownloadKindBundle || plan.FileName != "roads.shapefile.zip" || plan.ContentType != "application/zip" {
		t.Fatalf("plan = %#v, want shapefile bundle", plan)
	}
	if len(plan.Refs) != 4 {
		t.Fatalf("refs = %#v, want 4 refs", plan.Refs)
	}
	if plan.Refs[2].StorageRef != "bucket/roads/roads.dbf" || !plan.Refs[2].Required {
		t.Fatalf("dbf ref = %#v", plan.Refs[2])
	}
}

func TestOpenStorageDownloadPlanBundlesNFSShapefileRefs(t *testing.T) {
	t.Parallel()

	engineType := "download_test_nfs_bundle"
	plugin.Register(newDownloadTestFilePlugin(engineType, map[string]string{
		"shp/farmland.shp": "shape",
		"shp/farmland.shx": "index",
		"shp/farmland.dbf": "attrs",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	svc := &MetadataService{
		systemClient: testSystemClient(t, 26, engineType, nil),
		metaClient: testMetaItemClient(t, `{
			"id": 1,
			"engine_id": 26,
			"item_type": "file",
			"name": "farmland.shp",
			"full_name": "shp/farmland.shp",
			"attributes": {
				"item": {
					"layout": "multi",
					"format": "shapefile",
					"refs": [
						{"path":"shp/farmland.shp","role":"main","required":true,"primary":true},
						{"path":"shp/farmland.shx","role":"index","required":true},
						{"path":"shp/farmland.dbf","role":"attributes","required":true}
					]
				}
			}
		}`),
	}

	plan, err := svc.ResolveStorageDownloadPlan(t.Context(), 26, "shp/farmland.shp", nil)
	if err != nil {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v", err)
	}
	if plan.Kind != models.DownloadKindBundle || plan.FileName != "farmland.shapefile.zip" || plan.ContentType != "application/zip" {
		t.Fatalf("plan = %#v, want NFS shapefile zip bundle", plan)
	}

	reader, err := svc.OpenStorageDownloadPlan(t.Context(), 26, plan, nil)
	if err != nil {
		t.Fatalf("OpenStorageDownloadPlan() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("zip data is empty")
	}

	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	names := make([]string, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	want := []string{"farmland.dbf", "farmland.shp", "farmland.shx"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("zip entries = %#v, want %#v", names, want)
	}
}

func TestOpenStorageDownloadPlanBundlesObjectShapefileRefs(t *testing.T) {
	t.Parallel()

	engineType := "download_test_object_bundle"
	plugin.Register(newDownloadTestObjectPlugin(engineType, map[string]string{
		"gischain/data/farmland.shp": "shape",
		"gischain/data/farmland.shx": "index",
		"gischain/data/farmland.dbf": "attrs",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	svc := &MetadataService{
		systemClient: testSystemClient(t, 9, engineType, nil),
		metaClient: testMetaItemClient(t, `{
			"id": 1,
			"engine_id": 9,
			"item_type": "object",
			"name": "farmland.shp",
			"full_name": "gischain/data/farmland.shp",
			"attributes": {
				"item": {
					"layout": "multi",
					"format": "shapefile",
					"refs": [
						{"path":"data/farmland.shp","role":"main","required":true,"primary":true},
						{"path":"data/farmland.shx","role":"index","required":true},
						{"path":"data/farmland.dbf","role":"attributes","required":true}
					]
				}
			}
		}`),
	}

	plan, err := svc.ResolveStorageDownloadPlan(t.Context(), 9, "gischain/data/farmland.shp", nil)
	if err != nil {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v", err)
	}
	if plan.Kind != models.DownloadKindBundle || plan.FileName != "farmland.shapefile.zip" || plan.ContentType != "application/zip" {
		t.Fatalf("plan = %#v, want object shapefile zip bundle", plan)
	}
	if plan.Refs[0].StorageRef != "gischain/data/farmland.shp" || plan.Refs[1].StorageRef != "gischain/data/farmland.shx" {
		t.Fatalf("normalized refs = %#v, want bucket-prefixed object refs", plan.Refs)
	}

	reader, err := svc.OpenStorageDownloadPlan(t.Context(), 9, plan, nil)
	if err != nil {
		t.Fatalf("OpenStorageDownloadPlan() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("zip data is empty")
	}

	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	names := make([]string, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	want := []string{"farmland.dbf", "farmland.shp", "farmland.shx"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("zip entries = %#v, want %#v", names, want)
	}
}

func TestOpenStorageDownloadPlanFailsBeforeWritingWhenRequiredRefMissing(t *testing.T) {
	t.Parallel()

	engineType := "download_test_nfs_missing"
	plugin.Register(newDownloadTestFilePlugin(engineType, map[string]string{
		"shp/farmland.shp": "shape",
	}))
	t.Cleanup(func() { plugin.Unregister(engineType) })

	svc := &MetadataService{
		systemClient: testSystemClient(t, 26, engineType, nil),
		metaClient: testMetaItemClient(t, `{
			"id": 1,
			"engine_id": 26,
			"item_type": "file",
			"name": "farmland.shp",
			"full_name": "shp/farmland.shp",
			"attributes": {
				"item": {
					"layout": "multi",
					"format": "shapefile",
					"refs": [
						{"path":"shp/farmland.shp","role":"main","required":true,"primary":true},
						{"path":"shp/farmland.dbf","role":"attributes","required":true}
					]
				}
			}
		}`),
	}

	plan, err := svc.ResolveStorageDownloadPlan(t.Context(), 26, "shp/farmland.shp", nil)
	if err != nil {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v", err)
	}
	if _, err := svc.OpenStorageDownloadPlan(t.Context(), 26, plan, nil); err == nil {
		t.Fatal("OpenStorageDownloadPlan() succeeded with missing required ref")
	}
}

func TestResolveStorageDownloadPlanRejectsMultiItemWithoutRefs(t *testing.T) {
	t.Parallel()

	svc := &MetadataService{
		systemClient: testSystemClient(t, 9, "minio", nil),
		metaClient: testMetaItemClient(t, `{
			"id": 1,
			"engine_id": 9,
			"item_type": "object",
			"name": "roads.shp",
			"full_name": "bucket/roads/roads.shp",
			"attributes": {"item": {"layout": "multi", "format": "shapefile"}}
		}`),
	}
	_, err := svc.ResolveStorageDownloadPlan(t.Context(), 9, "bucket/roads/roads.shp", nil)
	if !errors.Is(err, ErrDownloadNotSupported) {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v, want ErrDownloadNotSupported", err)
	}
}

func TestResolveStorageDownloadPlanRejectsMultiRefsWithoutPrimary(t *testing.T) {
	t.Parallel()

	svc := &MetadataService{
		systemClient: testSystemClient(t, 9, "minio", nil),
		metaClient: testMetaItemClient(t, `{
			"id": 1,
			"engine_id": 9,
			"item_type": "object",
			"name": "roads.shp",
			"full_name": "bucket/roads/roads.shp",
			"attributes": {
				"item": {
					"layout": "multi",
					"format": "shapefile",
					"refs": [
						{"path":"bucket/roads/roads.shp","role":"main","required":true},
						{"path":"bucket/roads/roads.dbf","role":"attributes","required":true}
					]
				}
			}
		}`),
	}
	_, err := svc.ResolveStorageDownloadPlan(t.Context(), 9, "bucket/roads/roads.shp", nil)
	if !errors.Is(err, ErrDownloadNotSupported) {
		t.Fatalf("ResolveStorageDownloadPlan() error = %v, want ErrDownloadNotSupported", err)
	}
}

func testSystemClient(t *testing.T, engineID uint, engineType string, connInfo map[string]interface{}) *client.SystemClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/api/v1/system/engines/%d", engineID) {
			http.NotFound(w, r)
			return
		}
		payload := map[string]interface{}{
			"id":              engineID,
			"name":            "engine",
			"engine_type":     engineType,
			"connection_info": connInfo,
			"tenant_id":       11,
			"is_active":       true,
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	return client.NewSystemClient(server.URL, "test-token")
}

func testMetaItemClient(t *testing.T, itemJSON string) *client.MetaClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/by-catalog-path" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(itemJSON))
	}))
	t.Cleanup(server.Close)
	return client.NewMetaClientWithInternalKey(server.URL, "internal-key")
}

type downloadTestFilePlugin struct {
	engineType    string
	files         map[string]string
	objectCatalog bool
}

func newDownloadTestFilePlugin(engineType string, files map[string]string) *downloadTestFilePlugin {
	copied := map[string]string{}
	for path, content := range files {
		copied[strings.Trim(path, "/")] = content
	}
	return &downloadTestFilePlugin{engineType: engineType, files: copied}
}

func newDownloadTestObjectPlugin(engineType string, files map[string]string) *downloadTestFilePlugin {
	p := newDownloadTestFilePlugin(engineType, files)
	p.objectCatalog = true
	return p
}

func (p *downloadTestFilePlugin) Type() string         { return p.engineType }
func (p *downloadTestFilePlugin) DisplayName() string  { return p.engineType }
func (p *downloadTestFilePlugin) EngineOrigin() string { return "general" }
func (p *downloadTestFilePlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *downloadTestFilePlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (p *downloadTestFilePlugin) DefaultPort() int          { return 0 }
func (p *downloadTestFilePlugin) RequiredFields() []string  { return nil }
func (p *downloadTestFilePlugin) SensitiveFields() []string { return nil }
func (p *downloadTestFilePlugin) Capabilities() plugin.EngineCapabilities {
	if p.objectCatalog {
		return plugin.NewObjectCapabilities(p.engineType)
	}
	return plugin.NewFileCapabilities(p.engineType)
}
func (p *downloadTestFilePlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *downloadTestFilePlugin) DescribeCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	filePath := strings.Trim(path.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	now := time.Unix(0, 0)
	sizeBytes := int64(len(content))
	return &plugin.CatalogFacts{
		Path: path,
		Kind: p.itemKind(),
		Storage: &plugin.CatalogStorageFacts{
			Name:      filePath,
			Path:      filePath,
			SizeBytes: &sizeBytes,
		},
		UpdatedAt: &now,
	}, nil
}

func (p *downloadTestFilePlugin) itemKind() string {
	if p.objectCatalog {
		return plugin.CatalogKindObject
	}
	return plugin.CatalogKindFile
}
func (p *downloadTestFilePlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	filePath := strings.Trim(path.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func setupExplorerService(t *testing.T) (*ExplorerService, func()) {
	t.Helper()

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewTabularCapabilities("postgresql", plugin.CatalogTermSchema, plugin.TabularCapabilityOptions{}))
	if err != nil {
		t.Fatalf("failed to marshal capabilities: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines":
			tenantID := r.URL.Query().Get("tenant_id")
			switch tenantID {
			case "":
				fmt.Fprintf(w, `[
					{"id":1,"name":"tenant-one-db","engine_type":"postgresql","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q},
					{"id":2,"name":"tenant-two-db","engine_type":"postgresql","connection_info":{},"tenant_id":2,"is_active":true,"capabilities":%q},
					{"id":3,"name":"global-db","engine_type":"postgresql","connection_info":{},"is_active":true,"capabilities":%q}
				]`, capabilities, capabilities, capabilities)
			case "1":
				fmt.Fprintf(w, `[{"id":1,"name":"tenant-one-db","engine_type":"postgresql","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}]`, capabilities)
			case "2":
				fmt.Fprintf(w, `[{"id":2,"name":"tenant-two-db","engine_type":"postgresql","connection_info":{},"tenant_id":2,"is_active":true,"capabilities":%q}]`, capabilities)
			default:
				fmt.Fprint(w, `[]`)
			}
		case "/api/v1/system/engines/1":
			fmt.Fprintf(w, `{"id":1,"name":"tenant-one-db","engine_type":"postgresql","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}`, capabilities)
		case "/api/v1/system/engines/2":
			fmt.Fprintf(w, `{"id":2,"name":"tenant-two-db","engine_type":"postgresql","connection_info":{},"tenant_id":2,"is_active":true,"capabilities":%q}`, capabilities)
		default:
			http.NotFound(w, r)
		}
	}))

	return NewExplorerService(client.NewSystemClient(server.URL, "test-token"), nil, nil), server.Close
}

func TestExplorerEngineListTenantFiltering(t *testing.T) {
	service, cleanup := setupExplorerService(t)
	defer cleanup()

	// 无租户上下文（超级管理员）应看到所有激活资源（含租户为空）
	resourcesAll, err := service.GetEngineList(nil)
	if err != nil {
		t.Fatalf("GetEngineList(nil) returned error: %v", err)
	}
	if got, want := len(resourcesAll), 3; got != want {
		t.Fatalf("GetEngineList(nil) length = %d, want %d", got, want)
	}

	tenantOne := uint(1)
	resourcesTenantOne, err := service.GetEngineList(&tenantOne)
	if err != nil {
		t.Fatalf("GetEngineList(tenant=1) returned error: %v", err)
	}
	if got, want := len(resourcesTenantOne), 1; got != want {
		t.Fatalf("GetEngineList(tenant=1) length = %d, want %d", got, want)
	}
	if resourcesTenantOne[0].Name != "tenant-one-db" {
		t.Fatalf("GetEngineList(tenant=1)[0] = %s, want tenant-one-db", resourcesTenantOne[0].Name)
	}

	tenantTwo := uint(2)
	resourcesTenantTwo, err := service.GetEngineList(&tenantTwo)
	if err != nil {
		t.Fatalf("GetEngineList(tenant=2) returned error: %v", err)
	}
	if got, want := len(resourcesTenantTwo), 1; got != want {
		t.Fatalf("GetEngineList(tenant=2) length = %d, want %d", got, want)
	}
	if resourcesTenantTwo[0].Name != "tenant-two-db" {
		t.Fatalf("GetEngineList(tenant=2)[0] = %s, want tenant-two-db", resourcesTenantTwo[0].Name)
	}

	tenantThree := uint(3)
	resourcesTenantThree, err := service.GetEngineList(&tenantThree)
	if err != nil {
		t.Fatalf("GetEngineList(tenant=3) returned error: %v", err)
	}
	if got, want := len(resourcesTenantThree), 0; got != want {
		t.Fatalf("GetEngineList(tenant=3) length = %d, want %d", got, want)
	}
}

func TestGetTreeDeniedForOtherTenant(t *testing.T) {
	service, cleanup := setupExplorerService(t)
	defer cleanup()

	tenantOne := uint(1)
	_, err := service.GetTree(t.Context(), &tenantOne, 2, 1)
	if !errors.Is(err, ErrEngineAccessDenied) {
		t.Fatalf("GetTree should deny cross-tenant access, got err=%v", err)
	}
}

func TestGetTreeIncludesObjectStoragePrefixesAtDefaultDepth(t *testing.T) {
	t.Parallel()

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("failed to marshal capabilities: %v", err)
	}

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines/9":
			fmt.Fprintf(w, `{"id":9,"name":"Business MinIO","engine_type":"s3","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}`, capabilities)
		case "/api/v1/meta/engines/9/tree":
			_, _ = w.Write([]byte(`{
				"top_nodes":[{"id":6,"tenant_id":1,"engine_id":9,"node_type":"service","name":"Business MinIO","full_name":"","depth":1,"path":"6","scan_status":"completed","item_count":7}],
				"child_nodes":[
					{"id":23,"tenant_id":1,"engine_id":9,"parent_node_id":6,"node_type":"bucket","name":"addp","full_name":"addp","depth":2,"path":"6/23","scan_status":"completed","item_count":6},
					{"id":24,"tenant_id":1,"engine_id":9,"parent_node_id":23,"node_type":"prefix","name":"gis","full_name":"addp/gis","depth":3,"path":"6/23/24","scan_status":"completed","item_count":1},
					{"id":25,"tenant_id":1,"engine_id":9,"parent_node_id":23,"node_type":"prefix","name":"gischain","full_name":"addp/gischain","depth":3,"path":"6/23/25","scan_status":"completed","item_count":1},
					{"id":31,"tenant_id":1,"engine_id":9,"parent_node_id":6,"node_type":"bucket","name":"manager","full_name":"manager","depth":2,"path":"6/31","scan_status":"completed","item_count":1}
				],
				"items":[
					{"id":168,"tenant_id":1,"engine_id":9,"node_id":23,"item_type":"object","name":"test2.csv","full_name":"addp/test2.csv","attributes":{"item":{"layout":"single"},"storage":{"physical_path":"addp/test2.csv"}}},
					{"id":169,"tenant_id":1,"engine_id":9,"node_id":31,"item_type":"object","name":"lake2","full_name":"manager/lake2","attributes":{"item":{"layout":"single"},"storage":{"physical_path":"manager/lake2"}}}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer metaServer.Close()

	svc := NewExplorerService(
		client.NewSystemClient(metaServer.URL, "test-token"),
		client.NewMetaClientWithInternalKey(metaServer.URL, "internal-key"),
		nil,
	)
	tenantID := uint(1)
	tree, err := svc.GetTree(t.Context(), &tenantID, 9, 2)
	if err != nil {
		t.Fatalf("GetTree() error = %v", err)
	}

	addp := findChildByLabel(tree, "addp")
	if addp == nil {
		t.Fatalf("addp bucket not found in children: %#v", tree.Children)
	}
	labels := make([]string, 0, len(addp.Children))
	for _, child := range addp.Children {
		labels = append(labels, child.Label)
	}
	sort.Strings(labels)
	if strings.Join(labels, ",") != "gis,gischain,test2.csv" {
		t.Fatalf("addp children labels = %#v, want gis, gischain and test2.csv", labels)
	}
}

func TestRefreshNodeSubmitsDeepScanRun(t *testing.T) {
	t.Parallel()

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("failed to marshal capabilities: %v", err)
	}

	var gotScanPayload map[string]interface{}
	var gotScanAuth string
	treeRequests := 0
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines/9":
			fmt.Fprintf(w, `{"id":9,"name":"Business MinIO","engine_type":"s3","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}`, capabilities)
		case "/api/v1/meta/scan/run/manual":
			gotScanAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&gotScanPayload); err != nil {
				t.Fatalf("decode scan payload: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":11,"tenant_id":1,"execution_id":"run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
		case "/api/v1/meta/engines/9/tree":
			treeRequests++
			http.Error(w, "tree should not be loaded during refresh submission", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer metaServer.Close()

	svc := NewExplorerService(
		client.NewSystemClient(metaServer.URL, "test-token"),
		client.NewMetaClientWithInternalKey(metaServer.URL, "internal-key"),
		nil,
	)
	tenantID := uint(1)
	result, err := svc.RefreshNode(t.Context(), &tenantID, "addp://engine/9/path/addp?type=bucket&node_id=8", "user-token")
	if err != nil {
		t.Fatalf("RefreshNode() error = %v", err)
	}
	if result.Run == nil || result.Run.ExecutionID != "run-1" || result.Run.Status != "pending" {
		t.Fatalf("run = %#v", result.Run)
	}
	if treeRequests != 0 {
		t.Fatalf("treeRequests = %d, want 0", treeRequests)
	}
	if gotScanAuth != "Bearer user-token" {
		t.Fatalf("scan auth = %q, want user bearer token", gotScanAuth)
	}
	if gotScanPayload["scan_depth"] != "deep" || gotScanPayload["force"] != true {
		t.Fatalf("scan payload = %#v", gotScanPayload)
	}
	if gotScanPayload["node_id"] != float64(8) {
		t.Fatalf("scan payload node_id = %#v, want 8", gotScanPayload["node_id"])
	}
	if gotScanPayload["trigger_type"] != "manual" || gotScanPayload["source"] != "manager_refresh" {
		t.Fatalf("scan payload trigger/source = %#v/%#v, want manual/manager_refresh", gotScanPayload["trigger_type"], gotScanPayload["source"])
	}
}

func TestRefreshNodeRequiresAuthTokenBeforeSubmittingScan(t *testing.T) {
	t.Parallel()

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("failed to marshal capabilities: %v", err)
	}

	scanRequests := 0
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines/9":
			fmt.Fprintf(w, `{"id":9,"name":"Business MinIO","engine_type":"s3","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}`, capabilities)
		case "/api/v1/meta/scan/run/manual":
			scanRequests++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":11,"execution_id":"run-1","status":"pending"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer metaServer.Close()

	svc := NewExplorerService(
		client.NewSystemClient(metaServer.URL, "test-token"),
		client.NewMetaClientWithInternalKey(metaServer.URL, "internal-key"),
		nil,
	)
	tenantID := uint(1)
	_, err = svc.RefreshNode(t.Context(), &tenantID, "addp://engine/9/path/?type=service", "")
	if err == nil || !strings.Contains(err.Error(), "metadata scan requires authorization token") {
		t.Fatalf("RefreshNode() error = %v, want missing authorization token", err)
	}
	if scanRequests != 0 {
		t.Fatalf("scanRequests = %d, want 0", scanRequests)
	}
}

func findChildByLabel(node *resourcetree.TreeNode, label string) *resourcetree.TreeNode {
	if node == nil {
		return nil
	}
	for _, child := range node.Children {
		if child.Label == label {
			return child
		}
	}
	return nil
}

func TestGetNodeChildrenIncludesObjectStoragePrefixBelowBucket(t *testing.T) {
	t.Parallel()

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("failed to marshal capabilities: %v", err)
	}

	var requestedFullTree atomic.Bool
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines/9":
			fmt.Fprintf(w, `{"id":9,"name":"Business MinIO","engine_type":"s3","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}`, capabilities)
		case "/api/v1/meta/engines/9/tree":
			requestedFullTree.Store(true)
			http.Error(w, "full metadata tree must not be requested for node expansion", http.StatusInternalServerError)
		case "/api/v1/meta/nodes/31/children":
			_, _ = w.Write([]byte(`[
				{"id":32,"tenant_id":1,"engine_id":9,"parent_node_id":31,"node_type":"prefix","name":"regression","full_name":"manager/regression","depth":3,"path":"6/31/32","scan_status":"completed","item_count":2}
			]`))
		case "/api/v1/meta/nodes/31/items":
			_, _ = w.Write([]byte(`[
				{"id":168,"tenant_id":1,"engine_id":9,"node_id":31,"item_type":"object","name":"lake2","full_name":"manager/lake2","attributes":{"item":{"layout":"single"},"storage":{"physical_path":"manager/lake2"}}}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer metaServer.Close()

	svc := NewExplorerService(
		client.NewSystemClient(metaServer.URL, "test-token"),
		client.NewMetaClientWithInternalKey(metaServer.URL, "internal-key"),
		nil,
	)
	tenantID := uint(1)
	node, err := svc.GetNodeChildren(t.Context(), &tenantID, 9, "addp://engine/9/path/manager?type=bucket&node_id=31", 1)
	if err != nil {
		t.Fatalf("GetNodeChildren() error = %v", err)
	}
	if requestedFullTree.Load() {
		t.Fatal("GetNodeChildren() requested full metadata tree, want direct children/items only")
	}

	labels := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		labels = append(labels, child.Label)
	}
	sort.Strings(labels)
	if strings.Join(labels, ",") != "lake2,regression" {
		t.Fatalf("children labels = %#v, want lake2 and regression", labels)
	}

	var regression *resourcetree.TreeNode
	for _, child := range node.Children {
		if child.Label == "regression" {
			regression = child
			break
		}
	}
	if regression == nil {
		t.Fatal("regression prefix child not found")
	}
	if regression.Type != "prefix" || !regression.HasChildren {
		t.Fatalf("regression node = %#v, want prefix with children", regression)
	}
}

func TestGetNodeChildrenRequiresNodeID(t *testing.T) {
	t.Parallel()

	svc := NewExplorerService(nil, nil, nil)
	tenantID := uint(1)
	_, err := svc.GetNodeChildren(t.Context(), &tenantID, 9, "addp://engine/9/path/manager?type=bucket", 1)
	if err == nil || !strings.Contains(err.Error(), "requires locator node_id") {
		t.Fatalf("GetNodeChildren() error = %v, want missing node_id error", err)
	}
}
