package service

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	falkor "github.com/addp/common/engine/plugins/falkordb"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/protection"
	"github.com/google/uuid"
	rdb "github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIntegrationFalkorDBDevelopQueryExecution(t *testing.T) {
	if os.Getenv("ADDP_FALKORDB_INTEGRATION") != "1" {
		t.Skip("owned FalkorDB T2 gate required")
	}
	host, portText, err := net.SplitHostPort(os.Getenv("FALKORDB_T2_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	const name = "develop.graph/中文"
	client := rdb.NewClient(&rdb.Options{Addr: os.Getenv("FALKORDB_T2_ENDPOINT"), Password: os.Getenv("INFRA_FALKORDB_PASSWORD"), Protocol: 2, DisableIdentity: true})
	defer client.Close()
	if err = client.Do(t.Context(), "GRAPH.QUERY", name, "CREATE (a:Person {name:'甲'})-[:KNOWS]->(b:Person {name:'乙'})").Err(); err != nil {
		t.Fatal(err)
	}
	p := &falkor.FalkorDBPlugin{}
	caps, err := plugin.MarshalEngineCapabilities(p.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	capabilities := commonModels.JSONString(caps)
	engine := &commonModels.Engine{ID: 13, EngineType: p.Type(), Capabilities: &capabilities, ConnectionInfo: commonModels.ConnectionInfo{"host": host, "port": port, "password": os.Getenv("INFRA_FALKORDB_PASSWORD")}}
	calls := 0
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/execution-authorizations/1/engine-accesses" || r.Header.Get("Authorization") != "Bearer addp_at_develop" {
			t.Errorf("unexpected System execution access")
			w.WriteHeader(403)
			return
		}
		var req commonClient.ExecutionEngineAccessRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{AuthorizationID: "1", ExecutionID: req.ExecutionID, EngineID: "13", Effects: []string{"read"}, ExpiresAt: time.Now().UTC().Add(time.Minute), Engine: engine})
	}))
	defer system.Close()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: "different-engine", Revision: "00000000000000000001", ConsumerOwner: "develop", State: dataprotection.ProjectionStateEnrolling, Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "other"}, TargetEngineID: 11, Rules: []dataprotection.Rule{}, ValidFrom: now.Add(-time.Minute)}
	if err = projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err = store.ApplyBatch(t.Context(), 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "other", Changes: []dataprotection.ProjectionChange{{ChangeID: "other", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}}, now); err != nil {
		t.Fatal(err)
	}
	sqlEngine := NewSQLEngineService(&config.Config{}, commonClient.NewSystemServiceClient(system.URL, staticServiceTokenSource("addp_at_develop"), system.Client()), nil)
	sqlEngine.SetProtectionGate(protection.NewGate(store))
	executor := &DevExecutor{sqlEngine: sqlEngine}
	locator := (&resourcetree.ResourceLocator{EngineID: 13, Type: resourcetree.TypeGraph, Path: []string{name}}).ToURI()
	task := &models.DevTask{DevType: "query", Content: models.DevTaskContent{"query_type": "cypher", "query": "MATCH (a)-[r]->(b) RETURN a,r,b", "target_locator": locator}, ExecutionConfig: models.DevTaskContent{"engine_id": float64(13)}, Timeout: 10, QueryResultLimit: 5}
	authorization := &IssuedSQLExecutionAuthorization{AuthorizationID: 1, EngineIDs: []uint{13}, Effects: []SQLExecutionEffect{SQLExecutionEffectRead}, ExpiresAt: time.Now().UTC().Add(time.Minute)}
	result, _, err := executor.executeQuery(t.Context(), task, uuid.NewString(), 7, authorization)
	if err != nil {
		t.Fatal(err)
	}
	graph, ok := result["graph_data"].(*plugin.GraphData)
	if !ok || len(graph.Nodes) != 2 || len(graph.Relationships) != 1 || result["result_kind"] != "graph" {
		t.Fatalf("workbench graph result=%+v", result)
	}
	task.Content["query"] = "RETURN $text AS text"
	task.RuntimeParameters = map[string]interface{}{"text": "中文\" RETURN 1 //"}
	result, _, err = executor.executeQuery(t.Context(), task, uuid.NewString(), 7, authorization)
	if err != nil || result["result_kind"] != "table" {
		t.Fatalf("table=%+v err=%v", result, err)
	}
	rows := result["summary"].(map[string]interface{})["preview_rows"].([]map[string]interface{})
	if len(rows) != 1 || rows[0]["text"] != task.RuntimeParameters["text"] {
		t.Fatal("workbench lost typed parameter")
	}
	task.Content["query"] = "CREATE (:Forbidden)"
	task.RuntimeParameters = nil
	if _, _, err = executor.executeQuery(t.Context(), task, uuid.NewString(), 7, authorization); plugin.QueryErrorCodeOf(err) != plugin.QueryErrorCodeLocalReadUnsupported {
		t.Fatalf("write query not refused: %v", err)
	}
	targetPath, err := resourcetree.EngineCatalogPathFromLocator(p.EngineCatalogModel(), &resourcetree.ResourceLocator{EngineID: 13, Type: resourcetree.TypeGraph, Path: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(p.EngineCatalogModel(), targetPath)
	if err != nil {
		t.Fatal(err)
	}
	projection.ProjectionID = "target"
	projection.TargetEngineID = 13
	projection.Target = target
	if err = projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err = store.ApplyBatch(t.Context(), 7, "other", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "target", Changes: []dataprotection.ProjectionChange{{ChangeID: "target", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}}, now); err != nil {
		t.Fatal(err)
	}
	task.Content["query"] = "MATCH (n) RETURN n"
	result, _, err = executor.executeQuery(t.Context(), task, uuid.NewString(), 7, authorization)
	if err == nil || result != nil {
		t.Fatalf("protected workbench returned values: %v", err)
	}
	if calls != 4 {
		t.Fatalf("execution authorization checks=%d", calls)
	}
}
