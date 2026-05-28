package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
