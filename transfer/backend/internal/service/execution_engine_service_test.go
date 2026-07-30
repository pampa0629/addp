package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/contentio"
	engineplugin "github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/executor"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func newExecutionTestMetaClient(baseURL string) *commonClient.MetaClient {
	return commonClient.NewMetaClient(baseURL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "test-token", nil
	}))
}

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

func TestTableTargetRefGroupsUsesActualShapefileRefs(t *testing.T) {
	t.Parallel()

	actualRefs := []format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.shp", contentio.RoleMain), true, true),
		format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.shx", "index"), true, false),
		format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.dbf", "attributes"), true, false),
		format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.cpg", "encoding"), false, false),
	}
	got := tableTargetRefGroups(executor.TableTargetPlan{
		Kind:   executor.TableEndpointEncoded,
		Path:   engineplugin.ObjectItemPath(9, "bucket", "exports/roads.shp"),
		Format: format.FormatShapefile,
	}, actualRefs)
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
	wantPaths := []string{
		"bucket/exports/roads.shp",
		"bucket/exports/roads.shx",
		"bucket/exports/roads.dbf",
		"bucket/exports/roads.cpg",
	}
	for _, want := range wantPaths {
		if !containsString(paths, want) {
			t.Fatalf("ref paths = %#v, want actual ref %s", paths, want)
		}
	}
	for _, unexpected := range []string{
		"bucket/exports/roads.prj",
		"bucket/exports/roads.qpj",
		"bucket/exports/roads.sbn",
		"bucket/exports/roads.sbx",
	} {
		if containsString(paths, unexpected) {
			t.Fatalf("ref paths = %#v, must not include non-created ref %s", paths, unexpected)
		}
	}
}

func TestTableTargetRefGroupsDoesNotInventShapefileRefs(t *testing.T) {
	t.Parallel()

	got := tableTargetRefGroups(executor.TableTargetPlan{
		Kind:   executor.TableEndpointEncoded,
		Path:   engineplugin.ObjectItemPath(9, "bucket", "exports/roads.shp"),
		Format: format.FormatShapefile,
	}, nil)
	if got != nil {
		t.Fatalf("ref groups = %#v, want nil without actual multi refs", got)
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
	}, nil)
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
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("Meta request must not send legacy internal authentication headers")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":11,"tenant_id":7,"execution_id":"run-1","module":"meta","task_type":"scan","status":"pending","trigger_type":"manual"}`))
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: newExecutionTestMetaClient(server.URL),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	service.triggerMetadataScan(
		&models.TransferTask{TenantID: 7},
		0,
		planner.TableExportTaskSpec{Target: planner.EndpointSpec{Locator: "addp://engine/9/path/bucket/exports/roads.shp?type=object"}},
		executor.TableTargetPlan{
			Kind:   executor.TableEndpointEncoded,
			Path:   engineplugin.ObjectItemPath(9, "bucket", "exports/roads.shp"),
			Format: format.FormatShapefile,
		},
		[]format.RelatedRef{
			format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.shp", contentio.RoleMain), true, true),
			format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.shx", "index"), true, false),
			format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.dbf", "attributes"), true, false),
			format.NewRelatedRef(contentio.NewRef("bucket/exports/roads.cpg", "encoding"), false, false),
		},
	)

	assertTransferScanPayloadUsesRefGroups(t, gotPayload, "bucket/exports/roads.shp")
	assertTransferScanPayloadRefPaths(t, gotPayload, []string{
		"bucket/exports/roads.shp",
		"bucket/exports/roads.shx",
		"bucket/exports/roads.dbf",
		"bucket/exports/roads.cpg",
	}, []string{
		"bucket/exports/roads.prj",
		"bucket/exports/roads.qpj",
		"bucket/exports/roads.sbn",
		"bucket/exports/roads.sbx",
	})
}

func TestTriggerMetadataScanSkipsEncodedMultiTargetWithoutActualRefs(t *testing.T) {
	t.Parallel()

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	service := &ExecutionEngineService{
		metaClient: newExecutionTestMetaClient(server.URL),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	service.triggerMetadataScan(
		&models.TransferTask{TenantID: 7},
		0,
		planner.TableExportTaskSpec{Target: planner.EndpointSpec{Locator: "addp://engine/9/path/bucket/exports/roads.shp?type=object"}},
		executor.TableTargetPlan{
			Kind:   executor.TableEndpointEncoded,
			Path:   engineplugin.ObjectItemPath(9, "bucket", "exports/roads.shp"),
			Format: format.FormatShapefile,
		},
		nil,
	)
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("metadata scan requests = %d, want 0 without actual multi refs", got)
	}
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
		metaClient: newExecutionTestMetaClient(server.URL),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	service.triggerRawCopyMetadataScan(
		&models.TransferTask{TenantID: 7},
		0,
		planner.RawCopyTaskSpec{Target: planner.EndpointSpec{Locator: "addp://engine/9/path/backup/report.pdf?type=file"}},
		executor.RawCopyEndpointPlan{Path: engineplugin.FileItemPath(9, "backup/report.pdf")},
	)

	assertTransferScanPayloadUsesRefGroups(t, gotPayload, "backup/report.pdf")
}

func TestUpdateMetadataScanExecutionPersistsScanExecutionID(t *testing.T) {
	ctx := context.Background()
	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusSuccess)
	executionService := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	service := &ExecutionEngineService{
		executionService: executionService,
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	service.updateMetadataScanExecution(uint(execution.ID), "meta-run-1", 9, []string{"public"}, 0)

	dto, err := executionService.GetExecutionByExecutionID(ctx, execution.ExecutionID, uint(task.TenantID))
	if err != nil {
		t.Fatalf("GetExecutionByExecutionID() error = %v", err)
	}
	metadataScan, ok := dto.Metadata["metadata_scan"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata_scan = %#v, want object", dto.Metadata["metadata_scan"])
	}
	if metadataScan["execution_id"] != "meta-run-1" || metadataScan["engine_id"] != float64(9) {
		t.Fatalf("metadata_scan = %#v, want execution id and engine id", metadataScan)
	}
	paths, ok := metadataScan["catalog_paths"].([]interface{})
	if !ok || len(paths) != 1 || paths[0] != "public" {
		t.Fatalf("metadata_scan catalog_paths = %#v, want [public]", metadataScan["catalog_paths"])
	}
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

func assertTransferScanPayloadRefPaths(t *testing.T, payload map[string]interface{}, wantPaths []string, unexpectedPaths []string) {
	t.Helper()
	refGroups, ok := payload["ref_groups"].([]interface{})
	if !ok || len(refGroups) != 1 {
		t.Fatalf("ref_groups = %#v, want one group", payload["ref_groups"])
	}
	group, ok := refGroups[0].(map[string]interface{})
	if !ok {
		t.Fatalf("ref group = %#v, want object", refGroups[0])
	}
	refs, ok := group["refs"].([]interface{})
	if !ok {
		t.Fatalf("refs = %#v, want array", group["refs"])
	}
	paths := make([]string, 0, len(refs))
	for _, item := range refs {
		ref, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("ref = %#v, want object", item)
		}
		if path, ok := ref["path"].(string); ok {
			paths = append(paths, path)
		}
	}
	for _, want := range wantPaths {
		if !containsString(paths, want) {
			t.Fatalf("ref paths = %#v, want %s", paths, want)
		}
	}
	for _, unexpected := range unexpectedPaths {
		if containsString(paths, unexpected) {
			t.Fatalf("ref paths = %#v, must not include %s", paths, unexpected)
		}
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
		ParentLocator: "addp://engine/9/path/exports?type=directory",
		Name:          "abc.shp",
	}

	got := nativeTargetCatalogPaths(endpoint)
	if got != nil {
		t.Fatalf("nativeTargetCatalogPaths() = %#v, want nil", got)
	}
}

func TestNativeTargetCatalogPathsUsesNativeTableNamespace(t *testing.T) {
	endpoint := planner.EndpointSpec{
		ParentLocator: "addp://engine/9/path/public?type=schema",
		Name:          "roads",
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
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("Meta request must not send legacy internal authentication headers")
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
		metaClient: newExecutionTestMetaClient(server.URL),
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
		metaClient: newExecutionTestMetaClient(server.URL),
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
