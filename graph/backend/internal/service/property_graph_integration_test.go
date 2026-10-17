package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/falkordb"
	_ "github.com/addp/common/engine/plugins/neo4j"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
)

type graphServiceToken struct{}

func (graphServiceToken) Token(context.Context, uint) (string, error) { return "addp_at_graph", nil }

func (graphServiceToken) PlatformToken(context.Context) (string, error) { return "addp_at_graph", nil }

func TestIntegrationFalkorDBGraphDomain(t *testing.T) {
	if os.Getenv("ADDP_FALKORDB_INTEGRATION") != "1" {
		t.Skip("owned FalkorDB T2 gate required")
	}
	testPropertyGraphDomain(t, "falkordb", os.Getenv("FALKORDB_T2_ENDPOINT"), os.Getenv("INFRA_FALKORDB_PASSWORD"))
}
func TestIntegrationNeo4jGraphDomain(t *testing.T) {
	if os.Getenv("ADDP_NEO4J_INTEGRATION") != "1" {
		t.Skip("owned Neo4j T2 gate required")
	}
	testPropertyGraphDomain(t, "neo4j", os.Getenv("NEO4J_T2_ENDPOINT"), os.Getenv("NEO4J_T2_PASSWORD"))
}
func testPropertyGraphDomain(t *testing.T, kind, endpoint, password string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := plugin.Get(kind)
	if err != nil {
		t.Fatal(err)
	}
	provider := registered.(plugin.PropertyGraphProvider)
	path := plugin.EngineCatalogRootPath(registered.(plugin.EngineCatalogModelProvider).EngineCatalogModel(), 13)
	conn := commonmodels.ConnectionInfo{"host": host, "port": port, "password": password}
	if kind == "falkordb" {
		name := "graph.domain/中文"
		path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "graph", Kind: "graph", Name: name})
		client := redis.NewClient(&redis.Options{Addr: endpoint, Password: password, Protocol: 2})
		defer client.Close()
		if err = client.Do(ctx, "GRAPH.QUERY", name, "CREATE (:ADDPDomainSeed)").Err(); err != nil {
			t.Fatal(err)
		}
		// Adjacent graph must never be affected by domain queries/index management.
		if err = client.Do(ctx, "GRAPH.QUERY", "graph.adjacent", "CREATE (:ADDPDomainPerson {key:'outside',name:'Ada'})").Err(); err != nil {
			t.Fatal(err)
		}
	} else {
		conn["user"] = "neo4j"
		path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "database", Kind: "namespace", Name: "neo4j"}, plugin.EngineCatalogSegment{Term: "graph", Kind: "graph", Name: "graph"})
	}
	engine := &commonmodels.Engine{ID: 13, Name: "domain", EngineType: kind, ConnectionInfo: conn, LifecycleState: commonmodels.EngineLifecycleActive, ConnectionStatus: commonmodels.EngineConnectionOnline}
	capsJSON, err := plugin.MarshalEngineCapabilities(registered.Capabilities())
	if err != nil {
		t.Fatal(err)
	}
	capsValue := commonmodels.JSONString(capsJSON)
	engine.Capabilities = &capsValue

	// Use the real tenant service client and narrow Engine API contract.
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/engines/13" || r.Header.Get("Authorization") != "Bearer addp_at_graph" {
			t.Errorf("unexpected System request %s", r.URL.Path)
			http.Error(w, "denied", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(engine)
	}))
	defer system.Close()
	db := setupGraphCleanupTestDB(t)
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	ontology := models.Ontology{TenantID: 7, Name: "domain", Status: "active"}
	if err = db.Create(&ontology).Error; err != nil {
		t.Fatal(err)
	}
	props := []models.PropertyDefinition{{Name: "key", DataType: "string", Unique: true}, {Name: "name", DataType: "string", Searchable: true}, {Name: "summary", DataType: "string", Searchable: true}, {Name: "remarks", DataType: "string"}, {Name: "extra", DataType: "string"}}
	raw, _ := json.Marshal(props)
	entity := models.EntityType{TenantID: 7, OntologyID: ontology.ID, Name: "Person", NodeLabels: datatypes.JSON(`["ADDPDomainPerson"]`), DisplayProperty: "name", Properties: raw}
	if err = db.Create(&entity).Error; err != nil {
		t.Fatal(err)
	}
	staffProps, _ := json.Marshal([]models.PropertyDefinition{{Name: "remarks", DataType: "string", Searchable: true}})
	staff := models.EntityType{TenantID: 7, OntologyID: ontology.ID, Name: "Staff", NodeLabels: datatypes.JSON(`["ADDPDomainPerson","Staff"]`), Properties: staffProps}
	if err = db.Create(&staff).Error; err != nil {
		t.Fatal(err)
	}

	graphRepo, ontologyRepo := repository.NewKnowledgeGraphRepository(db), repository.NewOntologyRepository(db)
	if err := db.Exec("ATTACH DATABASE 'file:graph-protection?mode=memory&cache=private' AS graph").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "graph", "graph", nil)
	if err != nil {
		t.Fatal(err)
	}
	syncGraphProtection(t, store, 7, "")
	gate := NewGraphProtectionGate(store)
	data := NewGraphDataService(graphRepo, ontologyRepo, commonClient.NewSystemServiceClient(system.URL, graphServiceToken{}, system.Client()), gate)
	graphs := NewKnowledgeGraphService(graphRepo, data)
	req := &models.CreateKnowledgeGraphRequest{OntologyID: ontology.ID, EngineID: 13, CatalogPath: path, IndexManagement: models.GraphIndexesManaged, Name: "domain"}
	kg, err := graphs.Create(ctx, 7, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = graphs.Create(ctx, 7, req); err == nil {
		t.Fatal("duplicate graph target accepted")
	}
	_, held, err := data.getGraphAndEngine(ctx, kg.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err = gate.ReadyToAcknowledge(ctx, 7, "next"); err == nil {
		t.Fatal("target lifecycle ended before its operation")
	}
	held.close()
	if err = gate.ReadyToAcknowledge(ctx, 7, "next"); err != nil {
		t.Fatal(err)
	}
	first, err := data.MergeEntity(ctx, kg.ID, 7, []string{"ADDPDomainPerson"}, "key", "a", map[string]interface{}{"key": "a", "name": "Ada Lovelace", "summary": "Mathematics", "remarks": "SecretOnly", "big": int64(9007199254740993)})
	if err != nil || first == "" {
		t.Fatalf("merge=%q %v", first, err)
	}
	second, err := data.MergeEntity(ctx, kg.ID, 7, []string{"ADDPDomainPerson"}, "key", "b", map[string]interface{}{"key": "b", "name": "Charles Babbage"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := data.MergeEntity(ctx, kg.ID, 7, []string{"ADDPDomainPerson"}, "key", "a", map[string]interface{}{"name": ""})
	if err != nil || again != first {
		t.Fatalf("merge idempotency=%q %v", again, err)
	}
	rel, err := data.MergeRelation(ctx, kg.ID, 7, "ADDP_COLLABORATES", []string{"ADDPDomainPerson"}, "key", "a", []string{"ADDPDomainPerson"}, "key", "b", map[string]interface{}{"note": "中文 ' \\"})
	if err != nil || rel == "" {
		t.Fatalf("relation=%q %v", rel, err)
	}
	if err = data.SyncConstraints(ctx, kg.ID, 7, "Person", []string{"ADDPDomainPerson"}, props); err != nil {
		t.Fatal(err)
	}
	if err = data.SyncConstraints(ctx, kg.ID, 7, "Person", []string{"ADDPDomainPerson"}, props); err != nil {
		t.Fatalf("idempotent constraint: %v", err)
	}
	if _, err = provider.ExecuteGraphQueryAtPath(ctx, plugin.ConnectionInfo(conn), path, "CREATE (:ADDPDomainPerson {key:'a'})", plugin.QueryOptions{}); err == nil {
		t.Fatal("duplicate bypassed native unique constraint")
	}
	snapshot, err := data.GetBrowseSnapshot(ctx, kg.ID, 7)
	if err != nil || snapshot.Stats.ByLabel["ADDPDomainPerson"] != 2 {
		t.Fatalf("snapshot=%+v %v", snapshot, err)
	}
	expanded, err := data.Expand(ctx, kg.ID, 7, &models.ExpandRequest{Target: models.ExpandTarget{Kind: "entity", ID: first}, Depth: 1, NodeLimit: 10, RelationshipLimit: 10})
	if err != nil || len(expanded.Nodes) != 2 || len(expanded.Edges) != 1 {
		t.Fatalf("expand=%+v %v", expanded, err)
	}
	_, err = data.Expand(ctx, kg.ID, 7, &models.ExpandRequest{Target: models.ExpandTarget{Kind: "aggregate", Labels: []string{"ADDPDomainPerson"}}, Depth: 1, NodeLimit: 10, RelationshipLimit: 10})
	if err != nil {
		t.Fatalf("aggregate expand: %v", err)
	}
	route, err := data.FindPath(ctx, kg.ID, 7, first, second)
	if err != nil || len(route.Edges) != 1 {
		t.Fatalf("path=%+v %v", route, err)
	}
	reverse, err := data.FindPath(ctx, kg.ID, 7, second, first)
	if err != nil || len(reverse.Edges) != 1 {
		t.Fatalf("reverse undirected path=%+v %v", reverse, err)
	}

	inference := NewSchemaInferenceService(graphRepo, ontologyRepo, data, nil)
	inferred, err := inference.InferSchemaFromEngine(ctx, 13, 7, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	inferredPerson := false
	for _, node := range inferred.EntityTypes {
		if node.Name == "ADDPDomainPerson" && node.Count == 2 {
			for _, property := range node.Properties {
				if property.Name == "name" {
					inferredPerson = true
				}
			}
		}
	}
	if !inferredPerson {
		t.Fatalf("native graph schema inference failed: %+v", inferred)
	}

	found, err := data.SearchNodes(ctx, kg.ID, 7, "Ada", 10)
	if err != nil || len(found.Nodes) != 1 {
		t.Fatalf("search=%+v %v", found, err)
	}
	notSearchable, err := data.SearchNodes(ctx, kg.ID, 7, "SecretOnly", 10)
	if err != nil || len(notSearchable.Nodes) != 0 {
		t.Fatalf("unsearchable property leaked=%+v %v", notSearchable, err)
	}
	knowledge := NewKnowledgeService(data, ontologyRepo, graphRepo)
	listed, total, err := knowledge.ListEntitiesByType(ctx, kg.ID, 7, "Person", 1, 10)
	if err != nil || total != 2 || len(listed) != 2 {
		t.Fatalf("list=%+v total=%d %v", listed, total, err)
	}
	detail, err := knowledge.GetEntityDetail(ctx, kg.ID, 7, first)
	if err != nil || detail.DisplayName != "Ada Lovelace" {
		t.Fatalf("detail=%+v %v", detail, err)
	}
	neighbors, err := knowledge.GetEntityNeighbors(ctx, kg.ID, 7, first, 10)
	if err != nil || len(neighbors.Neighbors) != 1 {
		t.Fatalf("neighbors=%+v %v", neighbors, err)
	}
	searched, total, err := knowledge.SearchEntities(ctx, kg.ID, 7, "Ada", "Person", 1, 10)
	if err != nil || total != 1 || len(searched) != 1 {
		t.Fatalf("knowledge search=%+v total=%d %v", searched, total, err)
	}
	// Actual review service commits the graph element ID only after native success.
	buildRepo := repository.NewBuildRepository(db)
	reviewContent, _ := json.Marshal(map[string]interface{}{"type": "Person", "node_labels": []string{"ADDPDomainPerson"}, "unique_key_field": "key", "unique_key_value": "review", "properties": map[string]interface{}{"key": "review", "name": "Reviewed"}})
	review := models.ReviewItem{TenantID: 7, GraphID: kg.ID, ItemType: "entity", Content: reviewContent, Status: models.ReviewStatusPending}
	if err = db.Create(&review).Error; err != nil {
		t.Fatal(err)
	}
	build := &BuildService{buildRepo: buildRepo, graphDataSvc: data}
	if err = build.ApproveReviewItem(ctx, review.ID, 7, 9); err != nil {
		t.Fatal(err)
	}
	approved, err := buildRepo.GetReviewItem(review.ID, 7)
	if err != nil || approved.ElementID == "" || approved.Status != models.ReviewStatusApproved {
		t.Fatalf("review=%+v %v", approved, err)
	}
	if _, err = data.GetBrowseSnapshot(ctx, kg.ID, 8); err == nil {
		t.Fatal("cross-tenant graph accepted")
	}
	analysis := NewAnalysisService(graphRepo, ontologyRepo, data)
	caps, err := analysis.CheckCapabilities(ctx, kg.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if kind == "falkordb" && (caps.GDSAvailable || caps.SpatialAvailable || len(caps.CypherAlgos) != 2) {
		t.Fatalf("unsupported capabilities advertised: %+v", caps)
	}
	degree, err := analysis.RunAlgorithm(ctx, kg.ID, 7, &models.AlgorithmRunRequest{Algorithm: "degree_centrality", Limit: 10})
	if err != nil || degree == nil {
		t.Fatalf("degree=%+v %v", degree, err)
	}
	// External targets may reuse existing matching indexes, but never rebuild them.
	kg.IndexManagement = models.GraphIndexesExternal
	if err = graphRepo.Update(kg); err != nil {
		t.Fatal(err)
	}
	if _, err = data.SearchNodes(ctx, kg.ID, 7, "Ada", 10); err != nil {
		t.Fatalf("external reuse: %v", err)
	}
	props[1].Searchable = false
	props[2].Searchable = false
	props[4].Searchable = true
	entity.DisplayProperty = ""
	raw, _ = json.Marshal(props)
	entity.Properties = raw
	if err = db.Save(&entity).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = data.SearchNodes(ctx, kg.ID, 7, "SecretOnly", 10); err == nil {
		t.Fatal("external index conflict silently changed native index")
	}
	// Restoring the ontology proves the original index still serves its original fields.
	props[1].Searchable = true
	props[2].Searchable = true
	props[3].Searchable = false
	props[4].Searchable = false
	entity.DisplayProperty = "name"
	raw, _ = json.Marshal(props)
	entity.Properties = raw
	if err = db.Save(&entity).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = data.SearchNodes(ctx, kg.ID, 7, "Ada", 10); err != nil {
		t.Fatalf("external index was modified: %v", err)
	}
	kg.IndexManagement = models.GraphIndexesManaged
	if err = graphRepo.Update(kg); err != nil {
		t.Fatal(err)
	}
	props[2].Searchable = false
	props[3].Searchable = true
	raw, _ = json.Marshal(props)
	entity.Properties = raw
	if err = db.Save(&entity).Error; err != nil {
		t.Fatal(err)
	}
	rebuilt, err := data.SearchNodes(ctx, kg.ID, 7, "SecretOnly", 10)
	if err != nil || len(rebuilt.Nodes) != 1 {
		t.Fatalf("managed rebuild=%+v %v", rebuilt, err)
	}
	middle, err := data.MergeEntity(ctx, kg.ID, 7, []string{"ADDPDomainIntermediate"}, "key", "middle", map[string]interface{}{"key": "middle"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = data.MergeRelation(ctx, kg.ID, 7, "ADDP_NEXT", []string{"ADDPDomainPerson"}, "key", "b", []string{"ADDPDomainIntermediate"}, "key", "middle", nil); err != nil {
		t.Fatal(err)
	}
	multipleHops, err := data.FindPath(ctx, kg.ID, 7, middle, first)
	if err != nil || len(multipleHops.Nodes) != 3 || len(multipleHops.Edges) != 2 {
		t.Fatalf("reverse multi-hop path=%+v %v", multipleHops, err)
	}
	invalid := *req
	invalid.CatalogPath = path
	invalid.CatalogPath.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
	if kind == "falkordb" {
		invalid.CatalogPath.Segments[1].Name = "missing.graph"
	} else {
		invalid.CatalogPath.Segments[1].Name = "missing_database"
	}
	if _, err = graphs.Create(ctx, 7, &invalid); err == nil {
		t.Fatal("missing native graph target was created implicitly")
	}

	if kind == "falkordb" {
		client := redis.NewClient(&redis.Options{Addr: endpoint, Password: password, Protocol: 2})
		defer client.Close()
		response, err := client.Do(ctx, "GRAPH.RO_QUERY", "graph.adjacent", "MATCH(n) RETURN count(n) AS count").Result()
		if err != nil || response == nil {
			t.Fatalf("adjacent graph changed: %v", err)
		}
	}
	if kind == "falkordb" {
		reserved, err := data.MergeEntity(ctx, kg.ID, 7, []string{"SpatialLayer"}, "key", "business-spatial", map[string]interface{}{"key": "business-spatial", "name": "Business layer"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = data.MergeRelation(ctx, kg.ID, 7, "RTREE_ROOT", []string{"ADDPDomainPerson"}, "key", "a", []string{"SpatialLayer"}, "key", "business-spatial", nil); err != nil {
			t.Fatal(err)
		}
		snapshot, err := data.GetBrowseSnapshot(ctx, kg.ID, 7)
		if err != nil || snapshot.Stats.ByLabel["SpatialLayer"] != 1 {
			t.Fatalf("FalkorDB business label incorrectly excluded: %+v %v", snapshot, err)
		}
		path, err := data.FindPath(ctx, kg.ID, 7, first, reserved)
		if err != nil || len(path.Edges) != 1 {
			t.Fatalf("FalkorDB business relationship incorrectly excluded: %+v %v", path, err)
		}
	}

	protected := graphProtectionProjection(t, registered.(plugin.PropertyGraphProvider), path, "managed-graph")
	syncGraphProtection(t, store, 7, "managed", dataprotection.ProjectionChange{ChangeID: "managed", Operation: dataprotection.ChangeOperationUpsert, Projection: &protected})
	for operation, run := range map[string]func() error{
		"expand": func() error {
			_, err := data.Expand(ctx, kg.ID, 7, &models.ExpandRequest{Target: models.ExpandTarget{Kind: "entity", ID: first}, Depth: 1, NodeLimit: 10, RelationshipLimit: 10})
			return err
		},
		"knowledge_search": func() error {
			_, _, err := knowledge.SearchEntities(ctx, kg.ID, 7, "name", "Person", 1, 10)
			return err
		},
		"knowledge_stats":  func() error { _, err := knowledge.GetStats(ctx, kg.ID, 7); return err },
		"knowledge_schema": func() error { _, err := knowledge.GetOntologyDescription(ctx, kg.ID, 7); return err },
		"schema":           func() error { _, err := inference.InferSchema(ctx, kg.ID, 7, nil); return err },
		"sync_constraints": func() error {
			return data.SyncConstraints(ctx, kg.ID, 7, "Person", []string{"ADDPDomainPerson"}, props)
		},
		"spatial_sync": func() error { _, err := analysis.SyncAllSpatialLayers(ctx, kg.ID, 7); return err },
		"browse":       func() error { _, err := data.GetBrowseSnapshot(ctx, kg.ID, 7); return err },
		"search":       func() error { _, err := data.SearchNodes(ctx, kg.ID, 7, "name", 10); return err },
		"path":         func() error { _, err := data.FindPath(ctx, kg.ID, 7, first, middle); return err },
		"constraints":  func() error { _, err := data.GetConstraints(ctx, kg.ID, 7); return err },
		"inference":    func() error { _, err := inference.InferSchemaFromEngine(ctx, 13, 7, path, nil); return err },
		"entities":     func() error { _, _, err := knowledge.ListEntitiesByType(ctx, kg.ID, 7, "Person", 1, 10); return err },
		"entity":       func() error { _, err := knowledge.GetEntityDetail(ctx, kg.ID, 7, first); return err },
		"neighbors":    func() error { _, err := knowledge.GetEntityNeighbors(ctx, kg.ID, 7, first, 10); return err },
		"capabilities": func() error { _, err := analysis.CheckCapabilities(ctx, kg.ID, 7); return err },
		"analysis": func() error {
			_, err := analysis.RunAlgorithm(ctx, kg.ID, 7, &models.AlgorithmRunRequest{Algorithm: "degree_centrality", Limit: 10})
			return err
		},
		"merge": func() error {
			_, err := data.MergeEntity(ctx, kg.ID, 7, []string{"ADDPDomainPerson"}, "key", "protected-write", map[string]interface{}{"name": "forbidden"})
			return err
		},
	} {
		t.Run("protected_"+operation, func(t *testing.T) {
			if err := run(); !errors.Is(err, ErrGraphProtectionRequired) {
				t.Fatalf("protected exit: %v", err)
			}
		})
	}
	if err := gate.ReadyToAcknowledge(ctx, 7, "managed"); err != nil {
		t.Fatal(err)
	}
	t.Log(fmt.Sprintf("%s domain target, writes, indexes, review, browse and knowledge service passed", kind))
}
