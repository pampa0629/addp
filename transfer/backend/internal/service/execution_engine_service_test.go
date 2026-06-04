package service

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func TestNativeTargetCatalogPathsIgnoresEncodedObjectTarget(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Format:  "csv",
		Locator: "addp://engine/9/path/addp/gis/abc.csv?type=object",
	}

	got := nativeTargetCatalogPaths(endpoint)
	if got != nil {
		t.Fatalf("nativeTargetCatalogPaths() = %#v, want nil", got)
	}
}

func TestTableTargetRefGroupsUsesShapefileRelatedRefs(t *testing.T) {
	t.Parallel()

	got := tableTargetRefGroups(executor.TableTargetPlan{
		Kind:   executor.TableEndpointEncoded,
		Path:   engineplugin.ObjectItemPath(9, "bucket", "exports/roads.shp"),
		Format: format.FormatShapefile,
	})
	if len(got) != 1 {
		t.Fatalf("ref groups = %#v", got)
	}
	group := got[0]
	if group.Primary != "bucket/exports/roads.shp" {
		t.Fatalf("primary = %q", group.Primary)
	}
	paths := make([]string, 0, len(group.Refs))
	for _, ref := range group.Refs {
		paths = append(paths, ref.Path)
	}
	required := []string{
		"bucket/exports/roads.shp",
		"bucket/exports/roads.shx",
		"bucket/exports/roads.dbf",
	}
	for _, want := range required {
		if !containsString(paths, want) {
			t.Fatalf("ref paths = %#v, want required ref %s", paths, want)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestTableTargetRefGroupsUsesSingleEncodedTargetPath(t *testing.T) {
	t.Parallel()

	got := tableTargetRefGroups(executor.TableTargetPlan{
		Kind:   executor.TableEndpointEncoded,
		Path:   engineplugin.FileItemPath(9, "exports/table.csv"),
		Format: format.FormatCSV,
	})
	if len(got) != 1 || got[0].Primary != "exports/table.csv" || len(got[0].Refs) != 1 {
		t.Fatalf("ref groups = %#v", got)
	}
}

func TestTriggerMetadataScanSubmitsEncodedTargetRefGroups(t *testing.T) {
	t.Parallel()

	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/scan/run/manual" {
			t.Fatalf("path = %q, want /api/v1/meta/scan/run/manual", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "7" {
			t.Fatalf("X-Tenant-ID = %q, want 7", got)
		}
		if got := r.Header.Get("X-Internal-API-Key"); got != "internal-key" {
			t.Fatalf("X-Internal-API-Key = %q, want internal-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":11,"tenant_id":7,"execution_id":"run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: commonClient.NewMetaClientWithInternalKey(server.URL, "internal-key"),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	service.triggerMetadataScan(
		&models.TransferTask{TenantID: 7},
		planner.TableExportTaskSpec{Target: planner.EndpointSpec{Locator: "addp://engine/9/path/bucket/exports/roads.shp?type=object"}},
		executor.TableTargetPlan{
			Kind:   executor.TableEndpointEncoded,
			Path:   engineplugin.ObjectItemPath(9, "bucket", "exports/roads.shp"),
			Format: format.FormatShapefile,
		},
	)

	assertTransferScanPayloadUsesRefGroups(t, gotPayload, "bucket/exports/roads.shp")
}

func TestTriggerRawCopyMetadataScanSubmitsSingleRefGroup(t *testing.T) {
	t.Parallel()

	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/scan/run/manual" {
			t.Fatalf("path = %q, want /api/v1/meta/scan/run/manual", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":11,"tenant_id":7,"execution_id":"run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: commonClient.NewMetaClientWithInternalKey(server.URL, "internal-key"),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	service.triggerRawCopyMetadataScan(
		&models.TransferTask{TenantID: 7},
		planner.RawCopyTaskSpec{Target: planner.EndpointSpec{Locator: "addp://engine/9/path/backup/report.pdf?type=file"}},
		executor.RawCopyEndpointPlan{Path: engineplugin.FileItemPath(9, "backup/report.pdf")},
	)

	assertTransferScanPayloadUsesRefGroups(t, gotPayload, "backup/report.pdf")
}

func assertTransferScanPayloadUsesRefGroups(t *testing.T, payload map[string]interface{}, primary string) {
	t.Helper()
	if payload == nil {
		t.Fatal("metadata scan payload was not submitted")
	}
	if payload["trigger_type"] != "manual" || payload["source"] != "transfer" {
		t.Fatalf("payload trigger/source = %#v/%#v, want manual/transfer", payload["trigger_type"], payload["source"])
	}
	if _, ok := payload["catalog_paths"]; ok {
		t.Fatalf("catalog_paths must not be submitted for encoded content target: %#v", payload["catalog_paths"])
	}
	refGroups, ok := payload["ref_groups"].([]interface{})
	if !ok || len(refGroups) != 1 {
		t.Fatalf("ref_groups = %#v, want one group", payload["ref_groups"])
	}
	group, ok := refGroups[0].(map[string]interface{})
	if !ok || group["primary"] != primary {
		t.Fatalf("ref group = %#v, want primary %s", refGroups[0], primary)
	}
	if payload["scan_depth"] != "deep" || payload["force"] != true {
		t.Fatalf("payload scan_depth/force = %#v/%#v, want deep/true", payload["scan_depth"], payload["force"])
	}
}

func TestNativeTargetCatalogPathsIgnoresEncodedMultiObjectTarget(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Format:  "shapefile",
		Locator: "addp://engine/9/path/addp/gis/abc.shp?type=object",
	}

	got := nativeTargetCatalogPaths(endpoint)
	if got != nil {
		t.Fatalf("nativeTargetCatalogPaths() = %#v, want nil", got)
	}
}

func TestNativeTargetCatalogPathsIgnoresEncodedFileTarget(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Locator: "addp://engine/9/path/exports/abc.shp?type=file",
	}

	got := nativeTargetCatalogPaths(endpoint)
	if got != nil {
		t.Fatalf("nativeTargetCatalogPaths() = %#v, want nil", got)
	}
}

func TestNativeTargetCatalogPathsUsesNativeTableNamespace(t *testing.T) {
	endpoint := planner.EndpointSpec{
		Locator: "addp://engine/9/path/public/roads?type=table",
	}

	got := nativeTargetCatalogPaths(endpoint)
	want := []string{"public"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nativeTargetCatalogPaths() = %#v, want %#v", got, want)
	}
}

func TestRunningProgressCapsAtNinetyNine(t *testing.T) {
	tests := []struct {
		batchIndex int64
		want       float64
	}{
		{batchIndex: 0, want: 0},
		{batchIndex: 1, want: 1},
		{batchIndex: 50, want: 50},
		{batchIndex: 100, want: 99},
	}

	for _, tt := range tests {
		if got := runningProgress(tt.batchIndex); got != tt.want {
			t.Fatalf("runningProgress(%d) = %v, want %v", tt.batchIndex, got, tt.want)
		}
	}
}

func TestAttachSourceMetaAttributesLoadsMetaItem(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/12" {
			t.Fatalf("path = %q, want /api/v1/meta/items/12", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-ID"); got != "7" {
			t.Fatalf("X-Tenant-ID = %q, want 7", got)
		}
		if got := r.Header.Get("X-Internal-API-Key"); got != "internal-key" {
			t.Fatalf("X-Internal-API-Key = %q, want internal-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.MetaItem{
			ID:       12,
			EngineID: 3,
			Attributes: map[string]interface{}{
				"item": map[string]interface{}{
					"format": "parquet",
				},
			},
		})
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: commonClient.NewMetaClientWithInternalKey(server.URL, "internal-key"),
	}
	spec := &planner.TableExportTaskSpec{
		Source: planner.EndpointSpec{
			Locator: "addp://engine/3/path/datasets/roads.parquet?type=object&item_id=12",
		},
	}
	task := &models.TransferTask{
		TenantID: 7,
	}

	if err := service.attachSourceMetaAttributes(task, spec); err != nil {
		t.Fatalf("attachSourceMetaAttributes() error = %v", err)
	}
	itemAttrs, ok := spec.Source.Attributes["item"].(map[string]interface{})
	if !ok || itemAttrs["format"] != "parquet" {
		t.Fatalf("source attributes = %#v, want Meta item attributes", spec.Source.Attributes)
	}
}

func TestAttachSourceMetaAttributesRejectsEngineMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.MetaItem{
			ID:         12,
			EngineID:   4,
			Attributes: map[string]interface{}{"item": map[string]interface{}{"format": "parquet"}},
		})
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: commonClient.NewMetaClientWithInternalKey(server.URL, "internal-key"),
	}
	spec := &planner.TableExportTaskSpec{
		Source: planner.EndpointSpec{
			Locator: "addp://engine/3/path/datasets/roads.parquet?type=object&item_id=12",
		},
	}
	task := &models.TransferTask{
		TenantID: 7,
	}

	if err := service.attachSourceMetaAttributes(task, spec); err == nil {
		t.Fatal("attachSourceMetaAttributes() succeeded, want engine mismatch error")
	}
}
