package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	supermapworkflow "github.com/addp/common/engine/plugins/supermap_workflow"
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

type executionTestTokenSource struct{}

func (executionTestTokenSource) Token(context.Context, uint) (string, error) {
	return "test-token", nil
}

func (executionTestTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-test-token", nil
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

func TestSuperMapSDXPostgreSQLTableProviderUsesExactBoundRuntime(t *testing.T) {
	const runtimeEngineType = "tenant_supermap_runtime"
	var operatorCalls atomic.Int32
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operatorCalls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/operators" {
			t.Fatalf("runtime request = %s %s, want GET /api/operators", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		operators := make([]map[string]interface{}, 0, len(supermapworkflow.RequiredTableOperators()))
		for _, name := range supermapworkflow.RequiredTableOperators() {
			operators = append(operators, executionTestWorkflowOperator(runtimeEngineType, name))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"operators": operators})
	}))
	defer runtimeServer.Close()

	runtimeEndpoint := executionTestRuntimeEndpoint(t, runtimeServer.URL)
	runtimeCapabilities := executionTestWorkflowCapabilities(t, runtimeEngineType)
	var descriptorCalls atomic.Int32
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		descriptorCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("system request method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/system/runtime/engine-descriptors/22" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.EngineRuntimeDescriptor{
			ID:               22,
			Name:             "Tenant SuperMap Runtime",
			EngineType:       runtimeEngineType,
			EngineOrigin:     "extension",
			LifecycleState:   commonModels.EngineLifecycleActive,
			ConnectionStatus: commonModels.EngineConnectionOnline,
			Capabilities:     runtimeCapabilities,
			RuntimeEndpoint:  runtimeEndpoint,
		})
	}))
	defer systemServer.Close()

	service := &ExecutionEngineService{
		systemRuntime: commonClient.NewSystemServiceClient(systemServer.URL, executionTestTokenSource{}, systemServer.Client()),
	}
	plainBinding := planner.EngineBinding{Type: "postgresql"}
	if provider, err := service.superMapSDXPostgreSQLTableProvider(context.Background(), 7, plainBinding); err != nil || provider != nil {
		t.Fatalf("plain PostgreSQL provider = %#v, error = %v, want nil", provider, err)
	}
	postGISBinding := superMapWorkspaceBinding(engineplugin.SpatialWorkspaceSuperMapSDXPostGIS, 22)
	if provider, err := service.superMapSDXPostgreSQLTableProvider(context.Background(), 7, postGISBinding); err != nil || provider != nil {
		t.Fatalf("SuperMap SDX+ for PostGIS provider = %#v, error = %v, want nil", provider, err)
	}
	if got := descriptorCalls.Load(); got != 0 {
		t.Fatalf("runtime descriptor calls = %d before SuperMap SDX+ for PostgreSQL binding, want 0", got)
	}

	provider, err := service.superMapSDXPostgreSQLTableProvider(
		context.Background(),
		7,
		superMapWorkspaceBinding(engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL, 22),
	)
	if err != nil || provider == nil {
		t.Fatalf("bound provider = %#v, error = %v", provider, err)
	}
	if got := descriptorCalls.Load(); got != 1 {
		t.Fatalf("runtime descriptor calls = %d, want 1", got)
	}
	if got := operatorCalls.Load(); got != 1 {
		t.Fatalf("runtime operator calls = %d, want 1", got)
	}

	_, err = service.superMapSDXPostgreSQLTableProvider(
		context.Background(),
		7,
		superMapWorkspaceBinding(engineplugin.SpatialWorkspaceSuperMapSDXPostgreSQL, 33),
	)
	if err == nil {
		t.Fatal("missing exact bound runtime returned no error")
	}
}

func executionTestWorkflowOperator(engineType, name string) map[string]interface{} {
	return map[string]interface{}{
		"id":              name,
		"name":            name,
		"display_name":    name,
		"engine_type":     engineType,
		"type":            "table",
		"category":        "SuperMap Table",
		"category_path":   []string{"SuperMap Table"},
		"description":     "SuperMap table operator",
		"parameters":      []map[string]interface{}{},
		"output_ports":    []map[string]interface{}{},
		"execution_modes": []string{"direct"},
		"effects":         []string{"read", "write"},
	}
}

func executionTestWorkflowCapabilities(t *testing.T, engineType string) *commonModels.JSONString {
	t.Helper()
	payload, err := engineplugin.MarshalEngineCapabilities(engineplugin.NewWorkflowCapabilities(engineType, engineplugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatalf("marshal workflow capabilities: %v", err)
	}
	value := commonModels.JSONString(payload)
	return &value
}

func executionTestRuntimeEndpoint(t *testing.T, rawURL string) *commonModels.EngineRuntimeEndpoint {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse runtime URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split runtime host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse runtime port: %v", err)
	}
	return &commonModels.EngineRuntimeEndpoint{Protocol: parsed.Scheme, Host: host, Port: port}
}

func superMapWorkspaceBinding(kind string, boundRuntimeID uint) planner.EngineBinding {
	capabilities := &engineplugin.EngineCapabilities{
		SchemaVersion: engineplugin.CapabilitiesSchemaVersion,
		EngineType:    "postgresql",
		EngineFamily:  "tabular",
	}
	engineplugin.SetSpatialWorkspacesExtension(capabilities, []engineplugin.SpatialWorkspaceFact{{
		Ecosystem:            "supermap",
		Kind:                 kind,
		State:                engineplugin.SpatialWorkspaceStateDetected,
		BoundRuntimeEngineID: &boundRuntimeID,
	}})
	return planner.EngineBinding{Type: "postgresql", Capabilities: capabilities}
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

func TestAttachEncodedRecordSourceMetaAttributesLoadsProtectionSchema(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/items/51657" {
			t.Fatalf("path = %q, want /api/v1/meta/items/51657", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonModels.MetaItem{
			ID:       51657,
			EngineID: 11,
			Attributes: map[string]interface{}{
				"type_info": map[string]interface{}{
					"table": datatype.TableInfoPayload(&datatype.TableInfo{
						Name: "Persons",
						Fields: []datatype.FieldInfo{
							{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
							{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
						},
					}),
				},
			},
		})
	}))
	defer server.Close()

	service := &ExecutionEngineService{metaClient: newExecutionTestMetaClient(server.URL)}
	spec := &planner.EncodedRecordExportTaskSpec{Source: planner.EndpointSpec{
		Locator: "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657",
	}}
	task := &models.TransferTask{TenantID: 7}

	if err := service.attachEncodedRecordSourceMetaAttributes(task, spec); err != nil {
		t.Fatalf("attachEncodedRecordSourceMetaAttributes() error = %v", err)
	}
	fields := planner.EncodedRecordSourceFields(*spec)
	if len(fields) != 2 || fields[1].Name != "userInfo.phone" {
		t.Fatalf("source protection fields = %#v", fields)
	}
}

func TestEncodedRecordManagerInfraExportCommitsStableOutputAndFinishesSuccess(t *testing.T) {
	const sourceType = "manager-infra-export-source-test"
	sourcePlugin := &managerInfraExportSourcePlugin{}
	targetPlugin := &managerInfraExportTargetPlugin{}
	previousMinIO, previousMinIOErr := engineplugin.Get("minio")
	engineplugin.Register(sourcePlugin)
	engineplugin.Register(targetPlugin)
	t.Cleanup(func() {
		engineplugin.Unregister(sourceType)
		if previousMinIOErr == nil {
			engineplugin.Register(previousMinIO)
		} else {
			engineplugin.Unregister("minio")
		}
	})

	config := map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator": "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657", "data_type": "unknown", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp-infra://minio/manager/tenant_7/export/20260902/session-id?type=prefix",
			"name":           "Persons.ejsonl", "data_type": "unknown", "representation": "encoded", "format": "mongodb_extended_jsonl",
			"policy": map[string]interface{}{"apply_mode": "replace"},
		},
	}
	spec, err := planner.ParseEncodedRecordExportTaskSpec(config, 1000)
	if err != nil {
		t.Fatal(err)
	}

	db := newExecutionServiceTestDB(t)
	task := createExecutionServiceTestTask(t, db)
	task.Config = config
	task.BatchSize = 1000
	if err := db.Save(&task).Error; err != nil {
		t.Fatal(err)
	}
	execution := createExecutionServiceTestExecution(t, db, task, commonExecution.ExecutionStatusRunning)
	runningStatus := commonExecution.ExecutionStatusRunning
	task.Status = models.TaskStatusRunning
	task.LastExecutionID = &execution.ExecutionID
	task.LastExecutionStatus = &runningStatus
	if err := db.Save(&task).Error; err != nil {
		t.Fatal(err)
	}
	executionService := NewExecutionService(db, commonExecution.NewTaskExecutionRepository(db))
	bindExecutionServiceTestLease(t, db, executionService, &execution)
	engineService := &ExecutionEngineService{
		taskRepo:         repositoryForExecutionServiceTest(db),
		executionService: executionService,
		protectionGate:   allowTransferSourceProtectionGate{},
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	sourceCaps := engineplugin.NewDynamicSchemaCapabilities(sourceType)
	sourceCaps.Storage.Store.EncodedRecordReadSession = &engineplugin.EncodedRecordReadSessionCapability{Formats: []string{"mongodb_extended_jsonl"}}
	resolver := planner.NewHybridEngineResolver(
		planner.StaticEngineResolver{11: {Type: sourceType, EngineID: 11, Capabilities: &sourceCaps}},
		planner.NewInfraEngineResolver(planner.InfraEngineConfig{MinioEndpoint: "test-minio:9000", MinioAccessKey: "test", MinioSecretKey: "test"}),
	)

	if err := engineService.executeCommonEncodedRecordExportTask(t.Context(), &task, uint(execution.ID), spec, resolver); err != nil {
		t.Fatalf("executeCommonEncodedRecordExportTask() error = %v", err)
	}
	if !targetPlugin.deletedBeforeWrite || string(targetPlugin.content) != "{\"userInfo\":{\"phone\":\"136****4499\"}}\n" {
		t.Fatalf("target committed=%t content=%q", targetPlugin.deletedBeforeWrite, targetPlugin.content)
	}

	var stored commonExecution.TaskExecution
	if err := db.First(&stored, execution.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution status = %q, error_details = %#v", stored.Status, stored.ErrorDetails)
	}
	outputs, ok := stored.Metadata["outputs"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata.outputs = %#v", stored.Metadata["outputs"])
	}
	if outputs["target_locator"] != "addp-infra://minio/manager/tenant_7/export/20260902/session-id/Persons.ejsonl?type=object" {
		t.Fatalf("outputs.target_locator = %#v", outputs["target_locator"])
	}
	if outputs["row_count"] != float64(1) {
		t.Fatalf("outputs.row_count = %#v", outputs["row_count"])
	}
	lineagePayload, err := json.Marshal(stored.Metadata["lineage_facts"])
	if err != nil {
		t.Fatal(err)
	}
	var lineage commonExecution.LineageFacts
	if err := json.Unmarshal(lineagePayload, &lineage); err != nil {
		t.Fatal(err)
	}
	if len(lineage.Outputs) != 1 || lineage.Outputs[0].Locator != "addp-infra://minio/manager/tenant_7/export/20260902/session-id/Persons.ejsonl?type=object" {
		t.Fatalf("lineage outputs = %#v", lineage.Outputs)
	}
}

type managerInfraExportSourcePlugin struct{}

func (*managerInfraExportSourcePlugin) Type() string { return "manager-infra-export-source-test" }
func (*managerInfraExportSourcePlugin) DisplayName() string {
	return "Manager infra export source test"
}
func (*managerInfraExportSourcePlugin) EngineOrigin() string { return "general" }
func (*managerInfraExportSourcePlugin) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}
func (*managerInfraExportSourcePlugin) ValidateConnectionInfo(engineplugin.ConnectionInfo) error {
	return nil
}
func (*managerInfraExportSourcePlugin) DefaultPort() int          { return 0 }
func (*managerInfraExportSourcePlugin) RequiredFields() []string  { return nil }
func (*managerInfraExportSourcePlugin) SensitiveFields() []string { return nil }
func (*managerInfraExportSourcePlugin) Capabilities() engineplugin.EngineCapabilities {
	caps := engineplugin.NewDynamicSchemaCapabilities("manager-infra-export-source-test")
	caps.Storage.Store.EncodedRecordReadSession = &engineplugin.EncodedRecordReadSessionCapability{Formats: []string{"mongodb_extended_jsonl"}}
	return caps
}
func (*managerInfraExportSourcePlugin) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}
func (*managerInfraExportSourcePlugin) OpenEncodedRecordReadSession(context.Context, engineplugin.ConnectionInfo, engineplugin.EngineCatalogPath, engineplugin.EncodedRecordReadSessionOptions) (engineplugin.EncodedRecordReadSession, error) {
	return &managerInfraExportRecordSession{}, nil
}

type managerInfraExportRecordSession struct{ delivered bool }

func (s *managerInfraExportRecordSession) ReadBatch(context.Context, int) (*engineplugin.EncodedRecordBatchData, error) {
	if s.delivered {
		return &engineplugin.EncodedRecordBatchData{}, nil
	}
	s.delivered = true
	content := []byte("{\"userInfo\":{\"phone\":\"136****4499\"}}\n")
	return &engineplugin.EncodedRecordBatchData{Content: content, Records: 1}, nil
}
func (*managerInfraExportRecordSession) Close(context.Context) error { return nil }

type managerInfraExportTargetPlugin struct {
	deletedBeforeWrite bool
	content            []byte
}

func (*managerInfraExportTargetPlugin) Type() string { return "minio" }
func (*managerInfraExportTargetPlugin) DisplayName() string {
	return "Manager infra export target test"
}
func (*managerInfraExportTargetPlugin) EngineOrigin() string { return "general" }
func (*managerInfraExportTargetPlugin) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}
func (*managerInfraExportTargetPlugin) ValidateConnectionInfo(engineplugin.ConnectionInfo) error {
	return nil
}
func (*managerInfraExportTargetPlugin) DefaultPort() int          { return 0 }
func (*managerInfraExportTargetPlugin) RequiredFields() []string  { return nil }
func (*managerInfraExportTargetPlugin) SensitiveFields() []string { return nil }
func (*managerInfraExportTargetPlugin) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.NewObjectCapabilities("minio")
}
func (*managerInfraExportTargetPlugin) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}
func (p *managerInfraExportTargetPlugin) DeleteResource(context.Context, engineplugin.ConnectionInfo, engineplugin.EngineCatalogPath) error {
	p.deletedBeforeWrite = true
	p.content = nil
	return nil
}
func (p *managerInfraExportTargetPlugin) CreateContent(context.Context, engineplugin.ConnectionInfo, engineplugin.EngineCatalogPath, engineplugin.WriteOptions) (io.WriteCloser, error) {
	return &managerInfraExportWriter{target: p}, nil
}

type managerInfraExportWriter struct {
	bytes.Buffer
	target *managerInfraExportTargetPlugin
}

func (w *managerInfraExportWriter) Close() error {
	w.target.content = append([]byte(nil), w.Bytes()...)
	return nil
}

func TestTargetLineageLocatorBuildsCreatedTableIdentity(t *testing.T) {
	got := targetLineageLocator("addp://engine/3/path/public?type=schema", "orders")
	want := "addp://engine/3/path/public/orders?type=table"
	if got != want {
		t.Fatalf("targetLineageLocator() = %q, want %q", got, want)
	}
}

func TestTargetLineageLocatorBuildsInfraObjectIdentity(t *testing.T) {
	got := targetLineageLocator(
		"addp-infra://minio/manager/tenant_1/export/20260902/session-id?type=prefix",
		"Persons.ejsonl",
	)
	want := "addp-infra://minio/manager/tenant_1/export/20260902/session-id/Persons.ejsonl?type=object"
	if got != want {
		t.Fatalf("targetLineageLocator() = %q, want %q", got, want)
	}
}

func TestTransferLineageInputsUsesAllResolvedQueryRelations(t *testing.T) {
	inputs := transferLineageInputs(planner.EndpointSpec{
		Locator: "addp://engine/11/path/public/activities?type=table",
		Query: &planner.QuerySourceSpec{Inputs: []planner.QueryInputSpec{
			{Name: "activities", Locator: "addp://engine/11/path/public/activities?type=table&item_id=22"},
			{Name: "persons", Locator: "addp://engine/11/path/public/persons?type=table&item_id=23"},
		}},
	})

	if len(inputs) != 2 || inputs[0].Port != "activities" || inputs[0].ItemID == nil || *inputs[0].ItemID != 22 ||
		inputs[1].Port != "persons" || inputs[1].ItemID == nil || *inputs[1].ItemID != 23 {
		t.Fatalf("transferLineageInputs() = %#v", inputs)
	}
	if got := lineageInputPorts(inputs); !reflect.DeepEqual(got, []string{"activities", "persons"}) {
		t.Fatalf("lineageInputPorts() = %#v", got)
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
