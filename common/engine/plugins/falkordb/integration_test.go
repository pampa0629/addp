package falkordb

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIntegrationFalkorDBCatalogRepeatedPropertyObservations(t *testing.T) {
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
	p := &FalkorDBPlugin{}
	conn := plugin.ConnectionInfo{"host": host, "port": port, "password": os.Getenv("INFRA_FALKORDB_PASSWORD")}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	client, close, err := p.open(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	const graph = "catalog-property-observations"
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()
		if err := client.Do(cleanupCtx, "GRAPH.DELETE", graph).Err(); err != nil {
			t.Errorf("delete owned catalog property graph: %v", err)
		}
	}()
	// Six fields repeated over 100 records are six schema facts, not 600 fields.
	query := "CREATE (target:Target) WITH target UNWIND range(1,100) AS i " +
		"CREATE (:Record {key:i,name:'Fixture',active:true,score:1.5,note:'same',mixed:CASE WHEN i % 2 = 0 THEN i ELSE 'text' END})" +
		"-[:Link {key:i,name:'Fixture',active:true,score:1.5,note:'same',mixed:CASE WHEN i % 2 = 0 THEN i ELSE 'text' END}]->(target)"
	if _, err = runQuery(ctx, client, graph, query, nil, 1, false); err != nil {
		t.Fatal(err)
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, conn, p.graphPath(13, graph), plugin.EngineCatalogFactsOptions{})
	if err != nil {
		t.Fatalf("repeated property observations must not exhaust the schema budget: %v", err)
	}
	checkFields := func(fields []datatype.FieldInfo) {
		t.Helper()
		if len(fields) != 6 {
			t.Fatalf("wanted six distinct properties, got %+v", fields)
		}
		expected := map[string]datatype.FieldType{"key": datatype.FieldTypeBigInt, "name": datatype.FieldTypeString, "active": datatype.FieldTypeBool, "score": datatype.FieldTypeDouble, "note": datatype.FieldTypeString, "mixed": datatype.FieldTypeUnknown}
		for _, field := range fields {
			want, ok := expected[field.Name]
			if !ok || field.Type != want || (field.Name == "mixed" && field.NativeType != "mixed") {
				t.Fatalf("incorrect observed property fact: %+v", field)
			}
		}
	}
	if facts.Graph == nil || *facts.Graph.NodeCount != 101 || *facts.Graph.RelationshipCount != 100 {
		t.Fatalf("incorrect graph facts: %+v", facts.Graph)
	}
	if len(facts.Graph.NodeShapes) != 2 || facts.Graph.NodeShapes[0].Name != "Record" || len(facts.Graph.RelationshipShapes) != 1 {
		t.Fatalf("incorrect graph shapes: %+v", facts.Graph)
	}
	checkFields(facts.Graph.NodeShapes[0].Properties)
	checkFields(facts.Graph.RelationshipShapes[0].Properties)
	for name, query := range map[string]string{
		"node":         "CREATE (n:Wide) SET n = $props",
		"relationship": "CREATE (:Wide)-[r:WideLink]->(:Wide) SET r = $props",
	} {
		t.Run(name+" schema budget", func(t *testing.T) {
			props := map[string]interface{}{}
			for i := 0; i < 500; i++ {
				props[fmt.Sprintf("f%d", i)] = int64(i)
			}
			if _, err := runQuery(ctx, client, graph, query, map[string]interface{}{"props": props}, 1, false); err != nil {
				t.Fatal(err)
			}
			if _, err := p.DescribeEngineCatalogFacts(ctx, conn, p.graphPath(13, graph), plugin.EngineCatalogFactsOptions{}); err != nil {
				t.Fatalf("500 distinct facts must fit the schema budget: %v", err)
			}
			update := "MATCH (n:Wide) SET n.f500 = 500"
			if name == "relationship" {
				update = "MATCH ()-[r:WideLink]->() SET r.f500 = 500"
			}
			if _, err := runQuery(ctx, client, graph, update, nil, 1, false); err != nil {
				t.Fatal(err)
			}
			if _, err := p.DescribeEngineCatalogFacts(ctx, conn, p.graphPath(13, graph), plugin.EngineCatalogFactsOptions{}); err == nil || !strings.Contains(err.Error(), "graph properties exceed budget") {
				t.Fatalf("501 distinct facts must retain the schema budget failure: %v", err)
			}
			if _, err := runQuery(ctx, client, graph, "MATCH (n:Wide) DETACH DELETE n", nil, 1, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIntegrationFalkorDBSearchIndexReadyBeforeFirstSearch(t *testing.T) {
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
	p := &FalkorDBPlugin{}
	conn := plugin.ConnectionInfo{"host": host, "port": port, "password": os.Getenv("INFRA_FALKORDB_PASSWORD")}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	client, close, err := p.open(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	const graph = "search-index-readiness"
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()
		if err := client.Do(cleanupCtx, "GRAPH.DELETE", graph).Err(); err != nil {
			t.Errorf("delete owned search readiness graph: %v", err)
		}
	}()
	if _, err = runQuery(ctx, client, graph, "UNWIND range(1,30000) AS i CREATE (:Person {name:'Fixture searchable'})", nil, 1, false); err != nil {
		t.Fatal(err)
	}
	path := p.graphPath(13, graph)
	schema := plugin.GraphSearchSchema{OwnerPrefix: "fixture_", Managed: true, Indexes: []plugin.GraphSearchIndex{{Name: "fixture_search", Labels: []string{"Person"}, Properties: []string{"name"}}}}
	if _, err = p.SyncGraphSearchIndexes(ctx, conn, path, schema); err != nil {
		t.Fatal(err)
	}
	state, err := runQuery(ctx, client, graph, "CALL db.indexes() YIELD status RETURN status", nil, 10, true)
	if err != nil || len(state.Rows) != 1 || state.Rows[0]["status"] != "OPERATIONAL" {
		t.Fatalf("index sync returned before population completed: %+v %v", state, err)
	}
	result, err := runQuery(ctx, client, graph, "CALL db.idx.fulltext.queryNodes('Person','Fixture') YIELD node RETURN count(node) AS total", nil, 1, true)
	if err != nil || len(result.Rows) != 1 || result.Rows[0]["total"] != int64(30000) {
		t.Fatalf("first full-text search lost existing nodes: %+v %v", result, err)
	}
	// External indexes use the same readiness boundary without mutating their definition.
	schema.Managed = false
	if _, err = p.SyncGraphSearchIndexes(ctx, conn, path, schema); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationFalkorDBBusinessGraph(t *testing.T) {
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
	conn := plugin.ConnectionInfo{"host": host, "port": port, "password": os.Getenv("INFRA_FALKORDB_PASSWORD")}
	p := &FalkorDBPlugin{}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	probe, err := p.ProbeConnection(ctx, conn)
	if err != nil || probe.ServerVersion != "4.20.6" {
		t.Fatalf("probe=%+v err=%v", probe, err)
	}
	client, close, err := p.open(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	for _, name := range []string{"social.中文/a", "other"} {
		if _, err = runQuery(ctx, client, name, "CREATE (a:Person:Employee {name:'甲',age:33})-[:KNOWS {weight:2}]->(b:Person {name:'乙'}),(:`孤立` {name:'丙'})", nil, 0, false); err != nil {
			t.Fatal(err)
		}
	}
	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), 13)
	entries, err := p.ListChildren(ctx, conn, root, plugin.ListOptions{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("catalog=%+v err=%v", entries, err)
	}
	path := p.graphPath(13, "social.中文/a")
	facts, err := p.DescribeEngineCatalogFacts(ctx, conn, path, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *facts.Graph.NodeCount != 3 || *facts.Graph.RelationshipCount != 1 || len(facts.Graph.NodeShapes) != 3 || len(facts.Graph.RelationshipShapes) != 1 {
		t.Fatalf("facts=%+v", facts.Graph)
	}
	req := plugin.QueryRequest{EngineID: 13, Language: "cypher", TargetPath: &path, Query: "MATCH(a)-[r]->(b) RETURN a,r,b LIMIT $row_limit", Options: plugin.QueryOptions{ReadOnly: true, Limit: 1, Parameters: map[string]interface{}{"row_limit": int64(100)}}}
	plan, err := p.PrepareGraphQuery(ctx, conn, req)
	if err != nil {
		t.Fatal(err)
	}
	req.Options.Parameters["row_limit"] = int64(0)
	scope, err := plan.EngineReadScope(ctx)
	if err != nil || len(scope.EngineIDs) != 1 || scope.EngineIDs[0] != 13 {
		t.Fatalf("scope=%v %v", scope, err)
	}
	set, err := plan.ReadSet(ctx)
	if err != nil || len(set.Paths) != 1 || set.Paths[0].Segments[1].Name != "social.中文/a" {
		t.Fatalf("readset=%v %v", set, err)
	}
	scope.EngineIDs[0] = 99
	set.Paths[0].Segments[1].Name = "changed"
	set, err = plan.ReadSet(ctx)
	if err != nil || set.Paths[0].Segments[1].Name != "social.中文/a" {
		t.Fatal("read facts mutable")
	}
	result, err := plan.ExecuteGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.GraphData == nil || len(result.GraphData.Nodes) != 2 || len(result.GraphData.Relationships) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if _, err = plan.ExecuteGraph(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
		t.Fatalf("re-execution=%v", err)
	}
	tableReq := plugin.QueryRequest{EngineID: 13, Language: "cypher", TargetPath: &path, Query: "RETURN $text AS text,$big AS big", Options: plugin.QueryOptions{Limit: 1, Parameters: map[string]interface{}{"text": "\" RETURN 1 //中文\\\n", "big": int64(9007199254740993)}}}
	table, err := p.PrepareQuery(ctx, conn, tableReq)
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := table.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if scalar.Rows[0]["text"] != tableReq.Options.Parameters["text"] || scalar.Rows[0]["big"] != int64(9007199254740993) {
		t.Fatal("parameters lost type or bytes")
	}
	emptyReq := tableReq
	emptyReq.Query = "MATCH (n) WHERE false RETURN n.name AS name"
	emptyReq.Options.Parameters = nil
	empty, err := p.PrepareQuery(ctx, conn, emptyReq)
	if err != nil {
		t.Fatal(err)
	}
	emptyResult, err := empty.Execute(ctx)
	if err != nil || len(emptyResult.Rows) != 0 || len(emptyResult.Columns) != 1 || emptyResult.Columns[0] != "name" {
		t.Fatalf("empty=%+v %v", emptyResult, err)
	}
	// Engine timeouts/cancellation do not run a second query or create a graph.
	cancelled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	cancelledPlan, err := p.PrepareGraphQuery(ctx, conn, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cancelledPlan.ExecuteGraph(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query=%v", err)
	}
	pathRequest := req
	pathRequest.Options.Parameters = nil
	pathRequest.Query = "MATCH p=(a)-[r]->(b) RETURN p"
	pathPlan, err := p.PrepareGraphQuery(ctx, conn, pathRequest)
	if err != nil {
		t.Fatal(err)
	}
	pathResult, err := pathPlan.ExecuteGraph(ctx)
	if err != nil || pathResult.GraphData == nil || len(pathResult.GraphData.Nodes) != 2 || len(pathResult.GraphData.Relationships) != 1 {
		t.Fatalf("path result=%+v %v", pathResult, err)
	}
	missing := p.graphPath(13, "missing")
	if _, err = p.ResolvePath(ctx, conn, missing); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("missing graph=%v", err)
	}
	if exists := client.Exists(ctx, "missing").Val(); exists != 0 {
		t.Fatal("read created graph")
	}
	if _, err = runQuery(ctx, client, "social.中文/a", "CREATE (:Forbidden)", nil, 1, true); err == nil {
		t.Fatal("server read command allowed writes")
	}
	sample, err := p.SampleGraph(ctx, conn, path, plugin.GraphSampleOptions{Limit: 1, Filter: plugin.GraphSampleFilter{Kind: plugin.GraphSampleKindNodeShape, Labels: []string{"孤立"}}})
	if err != nil || len(sample.Nodes) != 1 {
		t.Fatalf("sample=%+v %v", sample, err)
	}
	// A native reader without GRAPH.QUERY can still decode nodes, edges and
	// property keys. The SDK must not reintroduce a write-capable command.
	readerPassword := rand.Text()
	if err = client.Do(ctx, "ACL", "SETUSER", "addp_t2_reader", "on", ">"+readerPassword, "~*", "+hello", "+info", "+module|list", "+graph.config|get", "+graph.list", "+type", "+graph.ro_query").Err(); err != nil {
		t.Fatal(err)
	}
	readerConn := plugin.ConnectionInfo{}
	for k, v := range conn {
		readerConn[k] = v
	}
	readerConn["user"] = "addp_t2_reader"
	readerConn["password"] = readerPassword
	reader, readerClose, err := p.open(ctx, readerConn)
	if err != nil {
		t.Fatal(err)
	}
	if err = reader.Do(ctx, "GRAPH.QUERY", "social.中文/a", "RETURN 1").Err(); err == nil {
		readerClose()
		t.Fatal("reader fixture unexpectedly has GRAPH.QUERY")
	}
	readerClose()
	req.Options.Parameters["row_limit"] = int64(10)
	readerPlan, err := p.PrepareGraphQuery(ctx, readerConn, req)
	if err != nil {
		t.Fatal(err)
	}
	readerResult, err := readerPlan.ExecuteGraph(ctx)
	if err != nil || readerResult.GraphData == nil || len(readerResult.GraphData.Nodes) != 2 || len(readerResult.GraphData.Relationships) != 1 {
		t.Fatalf("native reader graph=%+v %v", readerResult, err)
	}
	// The same projection contract used by Develop: disjoint engine, exact
	// neighboring graph, protected graph and uninitialized tenant are distinct.
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
	projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV4, ProjectionID: "other", Revision: "00000000000000000001", ConsumerOwner: "develop", State: dataprotection.ProjectionStateEnrolling, Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "other-engine"}, TargetEngineID: 11, Rules: []dataprotection.Rule{}, ValidFrom: now.Add(-time.Minute)}
	apply := func(cursor, previous string) {
		t.Helper()
		if err := projection.Seal(); err != nil {
			t.Fatal(err)
		}
		batch := &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: cursor, Changes: []dataprotection.ProjectionChange{{ChangeID: cursor, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}}
		if err = store.ApplyBatch(ctx, 7, previous, batch, now); err != nil {
			t.Fatal(err)
		}
	}
	check := func() error {
		t.Helper()
		req.Options.Parameters["row_limit"] = int64(10)
		prepared, err := p.PrepareGraphQuery(ctx, conn, req)
		if err != nil {
			return err
		}
		return store.RequireReadPlanUnmanaged(ctx, 7, p.EngineCatalogModel(), prepared, now)
	}
	apply("other-engine", "")
	if err = check(); err != nil {
		t.Fatalf("unrelated protection blocked query: %v", err)
	}
	neighbor, err := dataprotection.DataItemTargetFromCatalogPath(p.EngineCatalogModel(), p.graphPath(13, "other"))
	if err != nil {
		t.Fatal(err)
	}
	projection.ProjectionID = "neighbor"
	projection.TargetEngineID = 13
	projection.Target = neighbor
	apply("neighbor", "other-engine")
	if err = check(); err != nil {
		t.Fatalf("neighbor graph protection blocked exact read: %v", err)
	}
	target, err := dataprotection.DataItemTargetFromCatalogPath(p.EngineCatalogModel(), path)
	if err != nil {
		t.Fatal(err)
	}
	projection.ProjectionID = "target"
	projection.Target = target
	apply("target", "neighbor")
	if err = check(); err == nil {
		t.Fatal("protected target admitted")
	}
}
