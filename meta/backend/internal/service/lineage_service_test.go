package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type lineageTestEngineCatalog struct{}

func (lineageTestEngineCatalog) GetEnginesByTenant(tenantID uint) ([]*commonModels.Engine, error) {
	return []*commonModels.Engine{
		{ID: 9, Name: "Source PostgreSQL"},
		{ID: 10, Name: "Target PostgreSQL"},
	}, nil
}

func TestLineageCollectorIsIdempotentAndBuildsGraph(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	source := createLineageItemOnEngine(t, db, 7, 9, "source", "fp-source")
	target := createLineageItemOnEngine(t, db, 7, 10, "target", "fp-target")
	insertLineageExecution(t, db, "exec-1", 7, source.ID, target.ID, "replace")

	first, err := svc.CollectExecution(context.Background(), 7, "exec-1")
	if err != nil {
		t.Fatalf("CollectExecution() error = %v", err)
	}
	if first.Observed != 1 || first.Skipped != 0 {
		t.Fatalf("first result = %#v", first)
	}
	second, err := svc.CollectExecution(context.Background(), 7, "exec-1")
	if err != nil {
		t.Fatalf("CollectExecution() second error = %v", err)
	}
	if second.Observed != 0 || second.Skipped != 1 {
		t.Fatalf("second result = %#v", second)
	}

	graph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{
		SubjectKind: "data_item", ItemID: &target.ID, Direction: "upstream", Depth: 2, Limit: 20,
	})
	if err != nil {
		t.Fatalf("GetGraph() error = %v", err)
	}
	if graph.Subject.ItemID == nil || *graph.Subject.ItemID != target.ID || len(graph.Edges) != 1 {
		t.Fatalf("graph = %#v", graph)
	}
	if graph.Subject.EngineID == nil || *graph.Subject.EngineID != 10 || graph.Subject.EngineName != "Target PostgreSQL" {
		t.Fatalf("subject engine = %#v", graph.Subject)
	}
	if graph.Edges[0].RelationKind != "derive" || graph.Edges[0].Source.ItemID == nil || *graph.Edges[0].Source.ItemID != source.ID {
		t.Fatalf("edge = %#v", graph.Edges[0])
	}
	if graph.Edges[0].Source.EngineID == nil || *graph.Edges[0].Source.EngineID != 9 || graph.Edges[0].Source.EngineName != "Source PostgreSQL" {
		t.Fatalf("source engine = %#v", graph.Edges[0].Source)
	}
}

func TestLineageGraphExcludesRelationsWhoseEndpointIsSoftDeleted(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	source := createLineageItem(t, db, 7, "source", "fp-source")
	staleTarget := createLineageItem(t, db, 7, "stale-target", "fp-stale-target")
	currentTarget := createLineageItem(t, db, 7, "current-target", "fp-current-target")
	now := time.Now().UTC()

	for _, relation := range []models.LineageItemRelation{
		{
			TenantID: 7, SourceItemID: source.ID, TargetItemID: staleTarget.ID,
			RelationKind: "derive", Granularity: "item", Status: "active",
			FirstObservedAt: now, LastObservedAt: now,
		},
		{
			TenantID: 7, SourceItemID: source.ID, TargetItemID: currentTarget.ID,
			RelationKind: "derive", Granularity: "item", Status: "active",
			FirstObservedAt: now, LastObservedAt: now,
		},
	} {
		if err := db.Create(&relation).Error; err != nil {
			t.Fatalf("create lineage relation: %v", err)
		}
	}
	if err := db.Delete(&staleTarget).Error; err != nil {
		t.Fatalf("soft delete stale target: %v", err)
	}

	graph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{
		SubjectKind: "data_item", ItemID: &currentTarget.ID, Direction: "both", Depth: 3, Limit: 20,
	})
	if err != nil {
		t.Fatalf("GetGraph() error = %v", err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("graph nodes = %#v, want source and current target", graph.Nodes)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("graph edges = %#v, want only the relation with active endpoints", graph.Edges)
	}

	nodeIDs := make(map[uint]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.ItemID != nil {
			nodeIDs[*node.ItemID] = struct{}{}
		}
	}
	for _, edge := range graph.Edges {
		if edge.Source.ItemID == nil || edge.Target.ItemID == nil {
			t.Fatalf("edge has an empty endpoint: %#v", edge)
		}
		if _, ok := nodeIDs[*edge.Source.ItemID]; !ok {
			t.Fatalf("edge source %d is absent from nodes", *edge.Source.ItemID)
		}
		if _, ok := nodeIDs[*edge.Target.ItemID]; !ok {
			t.Fatalf("edge target %d is absent from nodes", *edge.Target.ItemID)
		}
	}
}

func TestLineageCollectorReplaceClosesPreviousInput(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	oldSource := createLineageItem(t, db, 7, "old-source", "fp-old")
	newSource := createLineageItem(t, db, 7, "new-source", "fp-new")
	target := createLineageItem(t, db, 7, "target", "fp-target")
	insertLineageExecution(t, db, "exec-old", 7, oldSource.ID, target.ID, "replace")
	insertLineageExecution(t, db, "exec-new", 7, newSource.ID, target.ID, "replace")

	if _, err := svc.CollectExecution(context.Background(), 7, "exec-old"); err != nil {
		t.Fatalf("collect old: %v", err)
	}
	if _, err := svc.CollectExecution(context.Background(), 7, "exec-new"); err != nil {
		t.Fatalf("collect new: %v", err)
	}

	var oldRelation models.LineageItemRelation
	if err := db.Where("source_item_id = ? AND target_item_id = ?", oldSource.ID, target.ID).First(&oldRelation).Error; err != nil {
		t.Fatalf("load old relation: %v", err)
	}
	if oldRelation.Status != "closed" || oldRelation.ClosedAt == nil {
		t.Fatalf("old relation = %#v", oldRelation)
	}
	var newRelation models.LineageItemRelation
	if err := db.Where("source_item_id = ? AND target_item_id = ?", newSource.ID, target.ID).First(&newRelation).Error; err != nil {
		t.Fatalf("load new relation: %v", err)
	}
	if newRelation.Status != "active" {
		t.Fatalf("new relation = %#v", newRelation)
	}
}

func TestRecordServicePublicationIsIdempotentAndReturnsEvidence(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	source := createLineageItem(t, db, 7, "source", "fp-source")
	request := models.RecordServicePublicationRequest{
		ServiceID: 19, ServiceName: "人员指标服务", ServiceUpdatedAt: time.Now().UTC(), PublishedRevision: "revision-1", DependencyHash: "revision-1",
		Dependencies: []models.LineageServiceDependencyInput{{SourceItemID: source.ID, DependencyKind: "table"}},
	}
	if err := svc.RecordServicePublication(context.Background(), 7, request); err != nil {
		t.Fatalf("RecordServicePublication() error = %v", err)
	}
	if err := svc.RecordServicePublication(context.Background(), 7, request); err != nil {
		t.Fatalf("RecordServicePublication() second error = %v", err)
	}
	var observationCount int64
	if err := db.Model(&models.LineageObservation{}).Where("relation_kind = 'serve'").Count(&observationCount).Error; err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if observationCount != 1 {
		t.Fatalf("observation count = %d, want 1", observationCount)
	}
	var observation models.LineageObservation
	if err := db.Where("relation_kind = 'serve'").First(&observation).Error; err != nil {
		t.Fatalf("load service publication observation: %v", err)
	}
	if observation.CaptureMethod != "declared" {
		t.Fatalf("capture method = %q, want declared", observation.CaptureMethod)
	}
	graph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{
		SubjectKind: "published_service", ServiceID: &request.ServiceID, Revision: request.PublishedRevision,
		Direction: "upstream", Depth: 1, Limit: 20,
	})
	if err != nil {
		t.Fatalf("GetGraph() error = %v", err)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].RelationKind != "serve" {
		t.Fatalf("graph edges = %#v", graph.Edges)
	}
}

func openLineageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := metatest.OpenMetadataDB(t)
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}
	statements := []string{
		`CREATE TABLE meta.lineage_item_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_item_id INTEGER NOT NULL,
			target_item_id INTEGER NOT NULL, relation_kind TEXT NOT NULL, granularity TEXT NOT NULL,
			write_mode TEXT, status TEXT NOT NULL, first_observed_at DATETIME NOT NULL,
			last_observed_at DATETIME NOT NULL, closed_at DATETIME, closed_by_observation_id INTEGER,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE meta.lineage_service_dependencies (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_item_id INTEGER NOT NULL,
			service_id INTEGER NOT NULL, service_name TEXT NOT NULL, service_updated_at DATETIME, published_revision TEXT NOT NULL, dependency_hash TEXT,
			dependency_kind TEXT NOT NULL, granularity TEXT NOT NULL, dependency_fields JSON,
			status TEXT NOT NULL, first_observed_at DATETIME NOT NULL, last_observed_at DATETIME NOT NULL,
			closed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE meta.lineage_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, relation_kind TEXT NOT NULL,
			granularity TEXT NOT NULL, source_item_id INTEGER, target_item_id INTEGER, service_id INTEGER,
			published_revision TEXT, execution_id TEXT, producer_module TEXT NOT NULL,
			capture_method TEXT NOT NULL CHECK (capture_method IN ('declared', 'runtime', 'parsed')),
			source_snapshot JSON NOT NULL, target_snapshot JSON, evidence JSON NOT NULL,
			observed_at DATETIME NOT NULL, created_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create lineage table: %v", err)
		}
	}
	return db
}

func createLineageItem(t *testing.T, db *gorm.DB, tenantID uint, name, fingerprint string) models.MetaItem {
	return createLineageItemOnEngine(t, db, tenantID, 9, name, fingerprint)
}

func createLineageItemOnEngine(t *testing.T, db *gorm.DB, tenantID, engineID uint, name, fingerprint string) models.MetaItem {
	t.Helper()
	item := models.MetaItem{TenantID: tenantID, EngineID: engineID, NodeID: 1, ItemType: "table", Name: name, FullName: "public." + name, Fingerprint: fingerprint}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func insertLineageExecution(t *testing.T, db *gorm.DB, executionID string, tenantID, sourceItemID, targetItemID uint, writeMode string) {
	t.Helper()
	metadata := map[string]interface{}{
		"lineage_facts": map[string]interface{}{
			"schema_version": "addp.lineage-facts/v1",
			"inputs":         []map[string]interface{}{{"port": "source", "item_id": sourceItemID}},
			"outputs":        []map[string]interface{}{{"port": "target", "item_id": targetItemID, "write_mode": writeMode}},
			"operations":     []map[string]interface{}{{"kind": "derive", "input_ports": []string{"source"}, "output_ports": []string{"target"}}},
		},
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := db.Exec(`INSERT INTO common.task_executions
		(tenant_id, execution_id, module, task_type, source, status, progress, trigger_type, metadata, created_at, updated_at)
		VALUES (?, ?, 'transfer', 'sync', 'transfer', 'success', 100, 'manual', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		tenantID, executionID, string(payload)).Error; err != nil {
		t.Fatalf("insert execution: %v", err)
	}
}

func TestLineageGraphFollowsOnlyDirectedPaths(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	items := map[string]models.MetaItem{}
	for _, name := range []string{"ancestor", "input", "root", "output", "descendant", "sibling", "coinput", "deleted", "hidden"} {
		items[name] = createLineageItem(t, db, 7, name, "fp-"+name)
	}
	now := time.Now().UTC()
	for _, pair := range [][2]string{{"ancestor", "input"}, {"input", "root"}, {"root", "output"}, {"output", "descendant"}, {"input", "sibling"}, {"coinput", "output"}, {"hidden", "deleted"}, {"deleted", "root"}} {
		r := models.LineageItemRelation{TenantID: 7, SourceItemID: items[pair[0]].ID, TargetItemID: items[pair[1]].ID, RelationKind: "derive", Granularity: "item", Status: "active", FirstObservedAt: now, LastObservedAt: now}
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	deleted := items["deleted"]
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	// Even malformed cross-tenant relation rows must not bridge tenant boundaries.
	foreign := models.LineageItemRelation{TenantID: 8, SourceItemID: items["hidden"].ID, TargetItemID: items["root"].ID, RelationKind: "derive", Granularity: "item", Status: "active"}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"input", "root", "output"} {
		if err := svc.RecordServicePublication(context.Background(), 7, models.RecordServicePublicationRequest{ServiceID: items[name].ID, ServiceName: "service-" + name, ServiceUpdatedAt: now, PublishedRevision: "r1", Dependencies: []models.LineageServiceDependencyInput{{SourceItemID: items[name].ID, DependencyKind: "table"}}}); err != nil {
			t.Fatal(err)
		}
	}
	root := items["root"]
	for _, tt := range []struct {
		direction string
		depth     int
		names     []string
		edgeCount int
	}{
		{"both", 0, []string{"root"}, 0},
		{"both", 1, []string{"input", "output", "root", "service-root"}, 3},
		{"both", 2, []string{"ancestor", "descendant", "input", "output", "root", "service-output", "service-root"}, 6},
		{"upstream", 2, []string{"ancestor", "input", "root"}, 2},
		{"downstream", 2, []string{"descendant", "output", "root", "service-output", "service-root"}, 4},
	} {
		t.Run(fmt.Sprintf("%s-%d", tt.direction, tt.depth), func(t *testing.T) {
			graph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &root.ID, Direction: tt.direction, Depth: tt.depth, Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, node := range graph.Nodes {
				if node.ItemID != nil {
					names = append(names, node.Name)
				} else {
					for name, item := range items {
						if item.ID == *node.ServiceID {
							names = append(names, "service-"+name)
						}
					}
				}
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, tt.names) || len(graph.Edges) != tt.edgeCount {
				t.Fatalf("nodes=%v edges=%d; want %v / %d", names, len(graph.Edges), tt.names, tt.edgeCount)
			}
		})
	}
	for _, depth := range []int{0, 1, 2} {
		graph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{SubjectKind: "published_service", ServiceID: &root.ID, Revision: "r1", Direction: "both", Depth: depth, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(graph.Nodes) != depth+1 || len(graph.Edges) != depth {
			t.Fatalf("service depth %d: %#v", depth, graph)
		}
	}
	// A cycle terminates; limits keep the root and a connected, closed graph.
	cycle := models.LineageItemRelation{TenantID: 7, SourceItemID: root.ID, TargetItemID: items["input"].ID, RelationKind: "derive", Granularity: "item", Status: "active", FirstObservedAt: now, LastObservedAt: now}
	if err := db.Create(&cycle).Error; err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 2, 3, 50} {
		graph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &root.ID, Direction: "both", Depth: 20, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		if len(graph.Nodes) > limit || len(graph.Edges) > limit || *graph.Subject.ItemID != root.ID {
			t.Fatalf("limit %d: %#v", limit, graph)
		}
		if limit < 4 && !graph.Truncated {
			t.Fatal("expected truncation")
		}
		connected := map[string]bool{lineageGraphNodeKey(graph.Subject): true}
		for _, edge := range graph.Edges {
			source, target := lineageGraphNodeKey(edge.Source), lineageGraphNodeKey(edge.Target)
			if !connected[source] && !connected[target] {
				t.Fatal("disconnected edge")
			}
			connected[source], connected[target] = true, true
		}
		if len(connected) != len(graph.Nodes) {
			t.Fatal("orphan nodes")
		}
	}
}

func TestLineageExpansionKeepsRootDirectionAndCountsHiddenNeighbours(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	items := make([]models.MetaItem, 8)
	for i := range items {
		items[i] = createLineageItem(t, db, 7, fmt.Sprintf("n%d", i), fmt.Sprintf("fp%d", i))
	}
	now := time.Now().UTC()
	// 0 -> 1 -> 2(root) -> 3 -> 4; 1 -> 5 is a sibling; 6 -> 3 a coinput; 7 -> 0.
	for _, pair := range [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {1, 5}, {6, 3}, {7, 0}} {
		if err := db.Create(&models.LineageItemRelation{TenantID: 7, SourceItemID: items[pair[0]].ID, TargetItemID: items[pair[1]].ID, RelationKind: "derive", Granularity: "item", Status: "active", FirstObservedAt: now, LastObservedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	req := models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &items[2].ID, Direction: "both", Depth: 1, Limit: 50}
	graph, err := svc.GetGraph(context.Background(), 7, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range graph.Nodes {
		switch *node.ItemID {
		case items[1].ID:
			if node.HiddenUpstreamCount != 1 || node.HiddenDownstreamCount != 0 {
				t.Fatalf("input counts = %+v", node)
			}
		case items[3].ID:
			if node.HiddenUpstreamCount != 0 || node.HiddenDownstreamCount != 1 {
				t.Fatalf("output counts = %+v", node)
			}
		}
	}
	req.ExpandUpstream = []uint{items[1].ID, items[3].ID} // output cannot become an upstream root
	req.ExpandDownstream = []uint{items[1].ID}            // input's sibling must stay hidden
	graph, err = svc.GetGraph(context.Background(), 7, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 4 || *graph.Subject.ItemID != items[2].ID {
		t.Fatalf("expanded graph = %+v", graph)
	}
	for _, node := range graph.Nodes {
		if *node.ItemID == items[0].ID && node.HiddenUpstreamCount != 1 {
			t.Fatalf("next layer count = %+v", node)
		}
		if *node.ItemID == items[4].ID || *node.ItemID == items[5].ID || *node.ItemID == items[6].ID || *node.ItemID == items[7].ID {
			t.Fatalf("unexpected branch = %+v", node)
		}
	}
	req.ExpandUpstream = append(req.ExpandUpstream, items[0].ID)
	graph, err = svc.GetGraph(context.Background(), 7, req)
	if err != nil || len(graph.Nodes) != 5 {
		t.Fatalf("second expansion = %+v, %v", graph, err)
	}
}

func TestServicePublicationDisplaysCurrentNameAndRejectsStaleReplay(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	source := createLineageItem(t, db, 7, "source", "fp-source")
	first := models.RecordServicePublicationRequest{ServiceID: 24, ServiceName: "旧名称", ServiceUpdatedAt: time.Now().UTC().Add(-time.Hour), PublishedRevision: "v1", Dependencies: []models.LineageServiceDependencyInput{{SourceItemID: source.ID, DependencyKind: "table"}}}
	if err := svc.RecordServicePublication(context.Background(), 7, first); err != nil {
		t.Fatal(err)
	}
	latest := first
	latest.ServiceName = "人员指标查询"
	latest.ServiceUpdatedAt = first.ServiceUpdatedAt.Add(time.Minute)
	latest.PublishedRevision = "v2"
	for _, req := range []models.RecordServicePublicationRequest{latest, first, latest} {
		if err := svc.RecordServicePublication(context.Background(), 7, req); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &source.ID, Direction: "downstream", Depth: 1, Limit: 20})
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].Target.Name != "人员指标查询" || graph.Edges[0].Target.PublishedRevision != "v2" {
		t.Fatalf("current service = %+v %v", graph, err)
	}
	var observations int64
	db.Model(&models.LineageObservation{}).Count(&observations)
	if observations != 2 {
		t.Fatalf("history count = %d", observations)
	}
	latest.Dependencies = nil
	latest.ServiceUpdatedAt = latest.ServiceUpdatedAt.Add(time.Minute)
	if err := svc.RecordServicePublication(context.Background(), 7, latest); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordServicePublication(context.Background(), 7, first); err != nil {
		t.Fatal(err)
	}
	var active int64
	db.Model(&models.LineageServiceDependency{}).Where("status = 'active'").Count(&active)
	if active != 0 {
		t.Fatalf("inactive replay restored %d dependencies", active)
	}
}
