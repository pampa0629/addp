package api

import (
	"encoding/json"
	"fmt"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	metaauthorization "github.com/addp/meta/internal/authorization"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"gorm.io/gorm"
)

func TestCollectExecutionLineageRouteRequiresDevelopServiceAndIsIdempotent(t *testing.T) {
	db := openLineageRouteTestDB(t)
	source := createLineageRouteItem(t, db, 7, "source")
	target := createLineageRouteItem(t, db, 7, "target")
	insertLineageRouteExecution(t, db, "execution-1", 7, source.ID, target.ID)
	insertLineageRouteExecution(t, db, "tenant-eight-execution", 8, source.ID, target.ID)

	systemServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer develop-token": {
			ClientID: "addp-develop", Permissions: []string{metaauthorization.PermissionMetaLineageCreate},
		},
		"Bearer wrong-client-token": {
			ClientID: "addp-service", Permissions: []string{metaauthorization.PermissionMetaLineageCreate},
		},
		"Bearer no-permission-token": {
			ClientID: "addp-develop", Permissions: []string{metaauthorization.PermissionMetaLineageRead},
		},
	})
	defer systemServer.Close()
	engineService := service.NewEngineService(db, nil)
	scanService := service.NewScanService(db, engineService)
	lineageService := service.NewLineageService(db, engineService)
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	router := SetupRouter(cfg, db, engineService, scanService, nil, nil, nil, nil, modulelifecycle.NewStandalone("meta"), lineageService)

	first := performLineageCollectionRequest(router, "execution-1", "develop-token")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body = %s", first.Code, first.Body.String())
	}
	var firstResult service.LineageCollectionResult
	if err := json.Unmarshal(first.Body.Bytes(), &firstResult); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstResult.Observed != 1 || firstResult.Skipped != 0 {
		t.Fatalf("first result = %#v", firstResult)
	}

	second := performLineageCollectionRequest(router, "execution-1", "develop-token")
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200; body = %s", second.Code, second.Body.String())
	}
	var secondResult service.LineageCollectionResult
	if err := json.Unmarshal(second.Body.Bytes(), &secondResult); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if secondResult.Observed != 0 || secondResult.Skipped != 1 {
		t.Fatalf("second result = %#v", secondResult)
	}

	for _, testCase := range []struct {
		name       string
		execution  string
		token      string
		wantStatus int
	}{
		{name: "wrong service client", execution: "execution-1", token: "wrong-client-token", wantStatus: http.StatusForbidden},
		{name: "missing permission", execution: "execution-1", token: "no-permission-token", wantStatus: http.StatusForbidden},
		{name: "missing token", execution: "execution-1", wantStatus: http.StatusUnauthorized},
		{name: "tenant isolation", execution: "tenant-eight-execution", token: "develop-token", wantStatus: http.StatusNotFound},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := performLineageCollectionRequest(router, testCase.execution, testCase.token)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.wantStatus, response.Body.String())
			}
		})
	}
}

func performLineageCollectionRequest(handler http.Handler, executionID, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/meta/lineage/executions/"+executionID+"/collect", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func openLineageRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	statements := []string{
		`CREATE TABLE meta.lineage_item_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_item_id INTEGER NOT NULL,
			target_item_id INTEGER NOT NULL, relation_kind TEXT NOT NULL, granularity TEXT NOT NULL, source_field_name TEXT NOT NULL DEFAULT '', target_field_name TEXT NOT NULL DEFAULT '', source_schema_hash TEXT NOT NULL DEFAULT '', target_schema_hash TEXT NOT NULL DEFAULT '',
			write_mode TEXT, transformation TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, first_observed_at DATETIME NOT NULL,
			last_observed_at DATETIME NOT NULL, closed_at DATETIME, closed_by_observation_id INTEGER,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE meta.lineage_service_dependencies (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_item_id INTEGER NOT NULL,
			service_id INTEGER NOT NULL, published_revision TEXT NOT NULL, dependency_hash TEXT,
			dependency_kind TEXT NOT NULL, granularity TEXT NOT NULL, dependency_fields JSON,
			status TEXT NOT NULL, first_observed_at DATETIME NOT NULL, last_observed_at DATETIME NOT NULL,
			closed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE meta.lineage_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, relation_kind TEXT NOT NULL,
			granularity TEXT NOT NULL, source_field_name TEXT NOT NULL DEFAULT '', target_field_name TEXT NOT NULL DEFAULT '', source_schema_hash TEXT NOT NULL DEFAULT '', target_schema_hash TEXT NOT NULL DEFAULT '', source_item_id INTEGER, target_item_id INTEGER, service_id INTEGER,
			published_revision TEXT, execution_id TEXT, producer_module TEXT NOT NULL, capture_method TEXT NOT NULL,
			source_snapshot JSON NOT NULL, target_snapshot JSON, evidence JSON NOT NULL,
			observed_at DATETIME NOT NULL, created_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create lineage route fixture: %v", err)
		}
	}
	return db
}

func createLineageRouteItem(t *testing.T, db *gorm.DB, tenantID uint, name string) models.MetaItem {
	t.Helper()
	item := models.MetaItem{
		TenantID: tenantID, EngineID: 9, NodeID: 1, ItemType: "table",
		Name: name, FullName: "public." + name, Fingerprint: "fp-" + name,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func insertLineageRouteExecution(t *testing.T, db *gorm.DB, executionID string, tenantID, sourceItemID, targetItemID uint) {
	t.Helper()
	metadata := commonModels.JSONMap{
		"lineage_facts": commonModels.JSONMap{
			"schema_version": commonExecution.LineageFactsSchemaVersion,
			"inputs":         []map[string]interface{}{{"port": "source", "item_id": sourceItemID}},
			"outputs":        []map[string]interface{}{{"port": "target", "item_id": targetItemID, "write_mode": "replace"}},
			"operations":     []map[string]interface{}{{"kind": "derive", "input_ports": []string{"source"}, "output_ports": []string{"target"}}},
		},
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal lineage execution: %v", err)
	}
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, status, progress, trigger_type, metadata, created_at, updated_at)
		VALUES (?, ?, 'develop', 'workflow', 'develop', 'success', 100, 'manual', ?, ?, ?)`,
		tenantID, executionID, string(payload), time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("insert lineage execution: %v", err)
	}
}

func TestLineageGraphRequestExpansion(t *testing.T) {
	for _, tt := range []struct {
		query string
		valid bool
	}{{"", true}, {"&expand_upstream=1,2&expand_downstream=3", true}, {"&expand_upstream=0", false}, {"&expand_upstream=-1", false}, {"&expand_upstream=abc", false}, {"&expand_upstream=" + strings.Repeat("1,", 100) + "1", false}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/graph?subject_kind=data_item&item_id=3"+tt.query, nil)
		req, err := parseLineageGraphRequest(c)
		if (err == nil) != tt.valid {
			t.Fatalf("%s: %v", tt.query, err)
		}
		if err == nil && req.Depth != 2 {
			t.Fatalf("default depth = %d", req.Depth)
		}
	}
}

func TestLineageGraphFieldRequestKeepsExactNames(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/graph?subject_kind=field_ref&item_id=3&field_name=a.b&schema_snapshot_hash=snapshot&granularity=field", nil)
	request, err := parseLineageGraphRequest(c)
	if err != nil || request.FieldName != "a.b" || request.SchemaSnapshotHash != "snapshot" || request.Granularity != "field" {
		t.Fatalf("field request=%+v %v", request, err)
	}
}

func TestLineageGraphFieldParametersReturnBadRequest(t *testing.T) {
	db := openLineageRouteTestDB(t)
	handler := &Handler{lineageService: service.NewLineageService(db, service.NewEngineService(db, nil))}
	router := gin.New()
	router.GET("/graph", handler.GetLineageGraph)
	for _, query := range []string{"subject_kind=field_ref&item_id=3", "subject_kind=field_ref&item_id=3&field_name=id&expand_upstream=1", "subject_kind=data_item&item_id=3&field_name=id", "subject_kind=data_item&item_id=3&granularity=unknown", "subject_kind=field_ref&item_id=3&field_name=id&granularity=item", "subject_kind=published_service&service_id=1&revision=v1&granularity=field", "subject_kind=data_item&item_id=3&granularity=field&expand_upstream=1", "subject_kind=data_item&item_id=3&limit=501", "subject_kind=data_item&item_id=3&granularity=field&limit=5001", "subject_kind=data_item&item_id=3&granularity=field&limit=0", "subject_kind=data_item&item_id=3&granularity=field&limit=-1"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/graph?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query=%s status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestLineageGraphRequestDefaultCapacityByGranularity(t *testing.T) {
	for _, entry := range []struct {
		query string
		want  int
	}{
		{"subject_kind=data_item&item_id=3", 100},
		{"subject_kind=published_service&service_id=3&revision=v1", 100},
		{"subject_kind=data_item&item_id=3&granularity=field", 5000},
		{"subject_kind=field_ref&item_id=3&field_name=a.b", 5000},
		{"subject_kind=data_item&item_id=3&granularity=field&limit=100", 100},
	} {
		t.Run(entry.query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/graph?"+entry.query, nil)
			request, err := parseLineageGraphRequest(c)
			if err != nil || request.Limit != entry.want {
				t.Fatalf("limit=%d err=%v, want %d", request.Limit, err, entry.want)
			}
		})
	}
}

func TestLineageHTTPUsesCurrentVisibilityAndSafeErrors(t *testing.T) {
	for _, engineType := range []string{"postgresql", "nfs", "ftp", "minio"} {
		t.Run(engineType, func(t *testing.T) { exerciseLineageHTTPVisibility(t, engineType) })
	}
}

func exerciseLineageHTTPVisibility(t *testing.T, engineType string) {
	rootTerm, branchTerm, branchName, itemType, rootName, sep := "server", "schema", "public", "table", "root", "."
	if engineType == "nfs" || engineType == "ftp" {
		rootTerm, branchTerm, branchName, itemType, rootName, sep = "root", "dir", "outputs", "file", "nodes.gpkg", "/"
	}
	if engineType == "minio" {
		rootTerm, branchTerm, branchName, itemType, rootName, sep = "service", "bucket", "outputs", "object", "nodes.gpkg", "/"
	}

	tenant := uint(7)
	allow, unavailable, checks := true, false, 0
	caps := commonModels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"postgresql","engine_family":"tabular","storage":{"store":{}}}`)
	engine := commonModels.Engine{ID: 9, TenantID: &tenant, Name: "PG", EngineType: engineType, LifecycleState: "active", Capabilities: &caps}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{"access_token": "addp_at_meta_runtime", "token_type": "bearer", "expires_in": 300, "scope": "addp.api"})
		case "/api/v1/system/engines":
			json.NewEncoder(w).Encode([]commonModels.Engine{engine})
		case "/api/v1/system/engines/9":
			json.NewEncoder(w).Encode(engine)
		case "/api/v1/system/engine-access/metadata-checks/meta-lineage":
			checks++
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer addp_at_viewer" || r.Header.Get("X-Tenant-ID") != "" {
				t.Error("identity substituted")
			}
			if unavailable {
				w.WriteHeader(503)
				w.Write([]byte(`{"error":"private detail"}`))
				return
			}
			var request authorization.SourceMetadataCheckRequest
			if json.NewDecoder(r.Body).Decode(&request) != nil || !request.Valid() {
				t.Fatal("invalid candidates")
			}
			result := authorization.SourceMetadataCheck{ObservedAt: time.Now()}
			for _, target := range request.Targets {
				result.Targets = append(result.Targets, authorization.SourceMetadataTargetCheck{Target: target, Visible: allow && target.Segments[len(target.Segments)-1].Name == rootName})
			}
			json.NewEncoder(w).Encode(result)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(upstream.URL, "addp-meta", "meta-lineage-test-secret-32-bytes", upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	client := commonClient.NewSystemServiceClient(upstream.URL, tokenSource, upstream.Client())
	db := openLineageRouteTestDB(t)
	serverRoot := createResourceTreeHandlerNode(t, db, models.MetaNode{ID: 90, TenantID: 7, EngineID: 9, NodeType: rootTerm, Name: "Engine", FullName: "", Depth: 1})
	createResourceTreeHandlerNode(t, db, models.MetaNode{ID: 1, TenantID: 7, EngineID: 9, ParentNodeID: &serverRoot.ID, NodeType: branchTerm, Name: branchName, FullName: branchName, Depth: 2})
	makeItem := func(name string) models.MetaItem {
		item := models.MetaItem{TenantID: 7, EngineID: 9, NodeID: 1, ItemType: itemType, Name: name, FullName: branchName + sep + name}
		item.Fingerprint = commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		return item
	}
	root := makeItem(rootName)
	hidden := makeItem("hidden")
	now := time.Now().UTC()
	if err := db.Create(&models.LineageItemRelation{TenantID: 7, SourceItemID: hidden.ID, TargetItemID: root.ID, RelationKind: "derive", Granularity: "item", Status: "active", FirstObservedAt: now, LastObservedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	engines := service.NewEngineService(db, client)
	handler := NewHandler(engines, nil, nil, nil, service.NewMetadataQueryService(db), nil, service.NewLineageService(db, engines))
	handler.lineageVisibility = client
	router := gin.New()
	installResourceTreeTenantContext(t, router, 7)
	router.Use(func(c *gin.Context) {
		current, _ := commonAuth.AuthContextFromGin(c)
		current.Authorization.RoleAssignments = []authorization.RoleAssignment{{AssignmentID: "1", RoleKey: "tenant.lineage_viewer", SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: authorization.AssignmentScope{Type: "tenant", TenantID: current.Context.TenantID}, Permissions: []string{"meta.lineage.read"}}}
		if err := commonAuth.SetAuthContextForGin(c, current); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	router.GET("/graph", commonAuth.MustNewPermissionGuard("meta.lineage.read"), handler.GetLineageGraph)
	query := fmt.Sprintf("subject_kind=data_item&item_id=%d", root.ID)
	get := func(q string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/graph?"+q, nil)
		req.Header.Set("Authorization", "Bearer addp_at_viewer")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	response := get(query)
	var graph models.LineageGraphResponse
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &graph) != nil || len(graph.Nodes) != 1 || len(graph.Edges) != 0 || graph.Subject.HiddenUpstreamCount != 0 || graph.Truncated || !graph.PermissionFiltered || strings.Contains(response.Body.String(), branchName+sep+"hidden") {
		t.Fatalf("visible graph: %d %s", response.Code, response.Body.String())
	}
	allow = false
	response = get(query)
	missing := get("subject_kind=data_item&item_id=9999")
	if response.Code != 404 || missing.Code != 404 || response.Body.String() != missing.Body.String() {
		t.Fatalf("existence leak: %s %s", response.Body.String(), missing.Body.String())
	}
	unavailable = true
	response = get(query)
	if response.Code != 503 || strings.Contains(response.Body.String(), "private detail") {
		t.Fatalf("upstream failure: %d %s", response.Code, response.Body.String())
	}
	previous := checks
	response = get("subject_kind=published_service&service_id=77&revision=v1")
	if response.Code != 403 || checks != previous || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("service query: %d %s", response.Code, response.Body.String())
	}
}
