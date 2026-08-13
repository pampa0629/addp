package service

import (
	"context"
	"encoding/json"
	"testing"

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
		ServiceID: 19, PublishedRevision: "revision-1", DependencyHash: "revision-1",
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
			service_id INTEGER NOT NULL, published_revision TEXT NOT NULL, dependency_hash TEXT,
			dependency_kind TEXT NOT NULL, granularity TEXT NOT NULL, dependency_fields JSON,
			status TEXT NOT NULL, first_observed_at DATETIME NOT NULL, last_observed_at DATETIME NOT NULL,
			closed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE meta.lineage_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, relation_kind TEXT NOT NULL,
			granularity TEXT NOT NULL, source_item_id INTEGER, target_item_id INTEGER, service_id INTEGER,
			published_revision TEXT, execution_id TEXT, producer_module TEXT NOT NULL, capture_method TEXT NOT NULL,
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
