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
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/minio"
	_ "github.com/addp/common/engine/plugins/nfs"
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
func (p *downloadTestFilePlugin) DescribeItem(_ context.Context, _ plugin.ConnectionInfo, path plugin.CatalogPath, _ plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	filePath := strings.Trim(path.StringPath(), "/")
	content, ok := p.files[filePath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	now := time.Unix(0, 0)
	return &plugin.ItemMetadata{
		Path: path,
		Kind: p.itemKind(),
		Stats: map[string]interface{}{
			"size_bytes": int64(len(content)),
		},
		Attributes: map[string]interface{}{
			"name": filePath,
			"path": filePath,
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

func TestRefreshNodeReturnsDeepScanStats(t *testing.T) {
	t.Parallel()

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("failed to marshal capabilities: %v", err)
	}

	var gotScanPayload map[string]interface{}
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/engines/9":
			fmt.Fprintf(w, `{"id":9,"name":"Business MinIO","engine_type":"s3","connection_info":{},"tenant_id":1,"is_active":true,"capabilities":%q}`, capabilities)
		case "/api/v1/meta/scan/engine":
			if err := json.NewDecoder(r.Body).Decode(&gotScanPayload); err != nil {
				t.Fatalf("decode scan payload: %v", err)
			}
			_, _ = w.Write([]byte(`{"status":"success","message":"ok","catalog_nodes_scanned":1,"items_scanned":8,"fields_scanned":0,"duration_ms":42,"started_at":"2026-05-27T00:00:00Z","extraction":{"documents":2,"extracted":1,"unsupported":1,"failed":0,"indexed":1,"index_failed":0}}`))
		case "/api/v1/meta/engines/9/tree":
			_, _ = w.Write([]byte(`{
				"top_nodes":[{"id":8,"tenant_id":1,"engine_id":9,"node_type":"bucket","name":"addp","full_name":"addp","depth":1,"path":"addp","scan_status":"completed","item_count":8}],
				"child_nodes":[],
				"items":[]
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
	result, err := svc.RefreshNode(t.Context(), &tenantID, "addp://engine/9/path/addp?type=bucket&meta_id=8")
	if err != nil {
		t.Fatalf("RefreshNode() error = %v", err)
	}
	if result.Node == nil || len(result.Node.Children) != 1 || result.Node.Children[0].Label != "addp" {
		t.Fatalf("node = %#v, want engine tree with addp bucket child", result.Node)
	}
	if result.Scan == nil || result.Scan.ItemsScanned != 8 || result.Scan.CatalogNodesScanned != 1 {
		t.Fatalf("scan = %#v", result.Scan)
	}
	if result.Scan.Extraction == nil || result.Scan.Extraction.Documents != 2 || result.Scan.Extraction.Unsupported != 1 {
		t.Fatalf("scan extraction = %#v", result.Scan.Extraction)
	}
	if gotScanPayload["scan_depth"] != "deep" || gotScanPayload["force"] != true {
		t.Fatalf("scan payload = %#v", gotScanPayload)
	}
	if gotScanPayload["node_id"] != float64(8) {
		t.Fatalf("scan payload node_id = %#v, want 8", gotScanPayload["node_id"])
	}
}
