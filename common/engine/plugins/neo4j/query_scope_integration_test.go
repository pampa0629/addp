package neo4j

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIntegrationNeo4jPreparedReadScope(t *testing.T) {
	if os.Getenv("ADDP_NEO4J_INTEGRATION") != "1" {
		t.Skip("owned Neo4j T2 gate required")
	}
	host, portText, err := net.SplitHostPort(os.Getenv("NEO4J_T2_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	conn := plugin.ConnectionInfo{"host": host, "port": port, "user": "neo4j", "password": os.Getenv("NEO4J_T2_PASSWORD"), "database": "neo4j"}
	provider := &Neo4jPlugin{}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	if _, err := provider.executeGraphQuery(ctx, conn, "CREATE (a:ADDPReadScope {name:'A'})-[:LINK]->(b:ADDPReadScope {name:'B'})", plugin.QueryOptions{}); err != nil {
		t.Fatal(err)
	}
	request := plugin.QueryRequest{EngineID: 13, Language: "cypher", Query: "MATCH (a:ADDPReadScope)-[r]->(b) RETURN a,r,b LIMIT $row_limit", Options: plugin.QueryOptions{ReadOnly: true, Limit: 1, Parameters: map[string]interface{}{"row_limit": int64(10)}}}
	prepared, err := provider.PrepareGraphQuery(ctx, conn, request)
	if err != nil {
		t.Fatal(err)
	}
	// Provider freezes the request; caller mutations cannot invalidate the proof.
	request.Options.Parameters["row_limit"] = int64(0)
	scope, err := prepared.EngineReadScope(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.EngineIDs) != 1 || scope.EngineIDs[0] != 13 {
		t.Fatal(scope)
	}
	scope.EngineIDs[0] = 99
	scope, err = prepared.EngineReadScope(ctx)
	if err != nil || scope.EngineIDs[0] != 13 {
		t.Fatalf("mutated scope: %v %v", scope, err)
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV4, ProjectionID: "mongodb", Revision: "00000000000000000001", ConsumerOwner: "develop", State: dataprotection.ProjectionStateEnrolling, Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "mongo-persons"}, TargetEngineID: 11, Rules: []dataprotection.Rule{}, ValidFrom: now.Add(-time.Minute)}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "other-engine", Changes: []dataprotection.ProjectionChange{{ChangeID: "other-engine", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}}
	if err := store.ApplyBatch(ctx, 7, "", batch, now); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReadPlanUnmanaged(ctx, 7, provider.EngineCatalogModel(), prepared, now); err != nil {
		t.Fatalf("unrelated engine blocked graph: %v", err)
	}
	graph, err := prepared.ExecuteGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Rows) != 1 || graph.GraphData == nil || len(graph.GraphData.Nodes) != 2 || len(graph.GraphData.Relationships) != 1 {
		t.Fatalf("graph result: %+v", graph)
	}
	if _, err := prepared.ExecuteGraph(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
		t.Fatalf("reexecuted graph: %v", err)
	}
	tableRequest := plugin.QueryRequest{EngineID: 13, Language: "cypher", Query: "MATCH (n:ADDPReadScope) RETURN n.name AS name", Options: plugin.QueryOptions{ReadOnly: true, Limit: 1}}
	table, err := provider.PrepareQuery(ctx, conn, tableRequest)
	if err != nil {
		t.Fatal(err)
	}
	protector, err := store.PrepareQueryProtection(ctx, 7, provider.EngineCatalogModel(), table, "query", dataprotection.SubjectReference{}, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := table.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := protector.Apply(result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("unbounded table result: %d", len(result.Rows))
	}
	path := plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 13, Segments: []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "neo4j"},
		{Term: plugin.EngineCatalogTermGraph, Kind: plugin.EngineCatalogKindGraph, Name: plugin.EngineCatalogKindGraph},
	}}
	sample, language := provider.GenerateSampleQuery(ctx, conn, plugin.SampleQueryOptions{Path: path})
	template, err := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 13, Language: language, Query: sample, TargetPath: &path, Options: plugin.QueryOptions{ReadOnly: true, Limit: 10}})
	if err != nil {
		t.Fatalf("generated sample preparation: %v", err)
	}
	templateProtection, err := store.PrepareQueryProtection(ctx, 7, provider.EngineCatalogModel(), template, "query", dataprotection.SubjectReference{}, now)
	if err != nil {
		t.Fatalf("generated sample protection: %v", err)
	}
	templateResult, err := template.Execute(ctx)
	if err != nil {
		t.Fatalf("generated sample execution: %v", err)
	}
	if err := templateProtection.Apply(templateResult); err != nil {
		t.Fatal(err)
	}
	if len(templateResult.Rows) != 2 {
		t.Fatalf("generated sample returned %d rows", len(templateResult.Rows))
	}
	projection.ProjectionID = "neo4j"
	projection.TargetEngineID = 13
	projection.Target.ResourceIdentity = "local-graph"
	_ = projection.Seal()
	batch.NextCursor = "protected-graph"
	batch.Changes[0].ChangeID = "protected-graph"
	if err := store.ApplyBatch(ctx, 7, "other-engine", batch, now); err != nil {
		t.Fatal(err)
	}
	graphPlan, err := provider.PrepareGraphQuery(ctx, conn, tableRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReadPlanUnmanaged(ctx, 7, provider.EngineCatalogModel(), graphPlan, now); !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatalf("protected graph admitted: %v", err)
	}
	var outbound atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { outbound.Add(1); w.Write([]byte("name\nsecret\n")) }))
	defer server.Close()
	for _, query := range []string{"CREATE (:ADDPReadScope)", "CALL db.labels()", "LOAD CSV FROM '" + server.URL + "' AS row RETURN row", "USE remote MATCH (n) RETURN n", "RETURN 1; RETURN 2"} {
		unsafe := tableRequest
		unsafe.Query = query
		if _, err := provider.PrepareGraphQuery(ctx, conn, unsafe); err == nil {
			t.Fatalf("unsupported query accepted: %s", query)
		}
	}
	unsafe := tableRequest
	unsafe.Query = "RETURN custom.fetch() AS value"
	unknown, err := provider.PrepareQuery(ctx, conn, unsafe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unknown.Execute(ctx); !errors.Is(err, errLocalReadCypher) {
		t.Fatalf("custom function admitted: %v", err)
	}
	if outbound.Load() != 0 {
		t.Fatal("external URL was accessed")
	}
	empty, err := provider.executeGraphQuery(ctx, conn, "MATCH (n:ADDPReadScope) WHERE false RETURN n.name AS name", plugin.QueryOptions{ReadOnly: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Rows) != 0 || len(empty.Columns) != 1 || empty.Columns[0] != "name" {
		t.Fatalf("empty read became write counters: %+v", empty)
	}
}
