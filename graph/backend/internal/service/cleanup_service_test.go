package service

import (
	"context"
	"testing"

	"github.com/addp/common/events"
	"github.com/addp/graph/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupGraphCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Ontology{},
		&models.EntityType{},
		&models.RelationType{},
		&models.OntologyVersion{},
		&models.KnowledgeGraph{},
		&models.BuildTask{},
		&models.BuildMaterial{},
		&models.ReviewItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGraphCleanupScanWithoutLifecycleContextReturnsNoCandidates(t *testing.T) {
	db := setupGraphCleanupTestDB(t)
	seedGraphCleanupEngineCandidate(t, db, 1, 7)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if graphCandidateRecordCount(stats) != 0 {
		t.Fatalf("expected no candidates without lifecycle context, got %+v", stats)
	}
}

func TestGraphCleanupEngineDeletedOnlyTargetsBoundKnowledgeGraphs(t *testing.T) {
	db := setupGraphCleanupTestDB(t)
	matchedGraphID, matchedTaskID := seedGraphCleanupEngineCandidate(t, db, 1, 7)
	otherGraphID, _ := seedGraphCleanupEngineCandidate(t, db, 1, 8)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, map[string]interface{}{"engine_id": 7})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if stats.KnowledgeGraphs != 1 || stats.BuildTasks != 1 || stats.BuildMaterials != 1 || stats.ReviewItems != 1 {
		t.Fatalf("unexpected engine scan stats: %+v", stats)
	}
	if stats.Ontologies != 0 || stats.EntityTypes != 0 || stats.RelationTypes != 0 || stats.OntologyVersions != 0 {
		t.Fatalf("engine cleanup must not include ontology definitions: %+v", stats)
	}
	if err := db.Model(&models.BuildTask{}).Where("id = ?", matchedTaskID).Update("status", models.BuildStatusPending).Error; err != nil {
		t.Fatalf("mark build task pending: %v", err)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, map[string]interface{}{"engine_id": 7})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.ArchivedGraphs != 1 || stats.CancelledBuildTasks != 1 {
		t.Fatalf("unexpected logical cleanup stats: %+v", stats)
	}

	var matchedGraph models.KnowledgeGraph
	if err := db.First(&matchedGraph, matchedGraphID).Error; err != nil {
		t.Fatalf("load matched graph: %v", err)
	}
	if matchedGraph.Status != "archived" {
		t.Fatalf("expected matched graph archived, got %s", matchedGraph.Status)
	}
	var matchedTask models.BuildTask
	if err := db.First(&matchedTask, matchedTaskID).Error; err != nil {
		t.Fatalf("load matched task: %v", err)
	}
	if matchedTask.Status != models.BuildStatusCancelled {
		t.Fatalf("expected matched task cancelled, got %s", matchedTask.Status)
	}
	var otherGraph models.KnowledgeGraph
	if err := db.First(&otherGraph, otherGraphID).Error; err != nil {
		t.Fatalf("load other graph: %v", err)
	}
	if otherGraph.Status != "active" {
		t.Fatalf("expected other graph active, got %s", otherGraph.Status)
	}
}

func TestGraphCleanupTenantDeletedPhysicalDeletesOwnedState(t *testing.T) {
	db := setupGraphCleanupTestDB(t)
	seedGraphCleanupTenantState(t, db, 1)
	seedGraphCleanupTenantState(t, db, 2)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if stats.Ontologies != 1 || stats.EntityTypes != 1 || stats.RelationTypes != 1 || stats.OntologyVersions != 1 ||
		stats.KnowledgeGraphs != 1 || stats.BuildTasks != 1 || stats.BuildMaterials != 1 || stats.ReviewItems != 1 {
		t.Fatalf("unexpected tenant scan stats: %+v", stats)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.DeletedRecords != 8 {
		t.Fatalf("expected 8 deleted records, got %+v", stats)
	}
	assertGraphCleanupCount(t, db, 1, 0)
	assertGraphCleanupCount(t, db, 2, 1)
}

func TestGraphCleanupTenantDeletedLogicalArchivesDefinitionsAndGraphs(t *testing.T) {
	db := setupGraphCleanupTestDB(t)
	seedGraphCleanupTenantState(t, db, 1)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModeLogical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.ArchivedOntologies != 1 || stats.ArchivedGraphs != 1 {
		t.Fatalf("expected ontology and graph archived, got %+v", stats)
	}
	if stats.CancelledBuildTasks != 0 {
		t.Fatalf("completed build task should not be cancelled, got %+v", stats)
	}

	var ontology models.Ontology
	if err := db.First(&ontology).Error; err != nil {
		t.Fatalf("load ontology: %v", err)
	}
	if ontology.Status != "archived" {
		t.Fatalf("expected ontology archived, got %s", ontology.Status)
	}
	var graph models.KnowledgeGraph
	if err := db.First(&graph).Error; err != nil {
		t.Fatalf("load graph: %v", err)
	}
	if graph.Status != "archived" {
		t.Fatalf("expected graph archived, got %s", graph.Status)
	}
	var task models.BuildTask
	if err := db.First(&task).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task.Status != models.BuildStatusSuccess {
		t.Fatalf("expected completed task unchanged, got %s", task.Status)
	}
}

func seedGraphCleanupEngineCandidate(t *testing.T, db *gorm.DB, tenantID uint, engineID uint) (uint, uint) {
	t.Helper()
	ontology := models.Ontology{TenantID: tenantID, Name: "ontology", Status: "active"}
	if err := db.Create(&ontology).Error; err != nil {
		t.Fatalf("create ontology: %v", err)
	}
	graph := models.KnowledgeGraph{
		TenantID:   tenantID,
		OntologyID: ontology.ID,
		EngineID:   engineID,
		Database:   "neo4j",
		Name:       "graph",
		Status:     "active",
	}
	if err := db.Create(&graph).Error; err != nil {
		t.Fatalf("create graph: %v", err)
	}
	task := models.BuildTask{TenantID: tenantID, GraphID: graph.ID, Name: "build", Status: models.BuildStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create build task: %v", err)
	}
	material := models.BuildMaterial{TenantID: tenantID, GraphID: graph.ID, TaskID: task.ID, Type: "document", Status: models.BuildMaterialStatusProcessing}
	if err := db.Create(&material).Error; err != nil {
		t.Fatalf("create material: %v", err)
	}
	review := models.ReviewItem{TenantID: tenantID, GraphID: graph.ID, TaskID: task.ID, MaterialID: material.ID, ItemType: models.ReviewItemEntity, Content: []byte(`{}`), Confidence: 0.5, Status: models.ReviewStatusPending}
	if err := db.Create(&review).Error; err != nil {
		t.Fatalf("create review: %v", err)
	}
	return graph.ID, task.ID
}

func seedGraphCleanupTenantState(t *testing.T, db *gorm.DB, tenantID uint) {
	t.Helper()
	ontology := models.Ontology{TenantID: tenantID, Name: "ontology", Status: "active"}
	if err := db.Create(&ontology).Error; err != nil {
		t.Fatalf("create ontology: %v", err)
	}
	entityType := models.EntityType{TenantID: tenantID, OntologyID: ontology.ID, Name: "Person"}
	if err := db.Create(&entityType).Error; err != nil {
		t.Fatalf("create entity type: %v", err)
	}
	relationType := models.RelationType{TenantID: tenantID, OntologyID: ontology.ID, Name: "KNOWS"}
	if err := db.Create(&relationType).Error; err != nil {
		t.Fatalf("create relation type: %v", err)
	}
	version := models.OntologyVersion{TenantID: tenantID, OntologyID: ontology.ID, Version: "1.0.0"}
	if err := db.Create(&version).Error; err != nil {
		t.Fatalf("create version: %v", err)
	}
	graph := models.KnowledgeGraph{TenantID: tenantID, OntologyID: ontology.ID, EngineID: 7, Database: "neo4j", Name: "graph", Status: "active"}
	if err := db.Create(&graph).Error; err != nil {
		t.Fatalf("create graph: %v", err)
	}
	task := models.BuildTask{TenantID: tenantID, GraphID: graph.ID, Name: "build", Status: models.BuildStatusSuccess}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create build task: %v", err)
	}
	material := models.BuildMaterial{TenantID: tenantID, GraphID: graph.ID, TaskID: task.ID, Type: "document", Status: models.BuildMaterialStatusCompleted}
	if err := db.Create(&material).Error; err != nil {
		t.Fatalf("create material: %v", err)
	}
	review := models.ReviewItem{TenantID: tenantID, GraphID: graph.ID, TaskID: task.ID, MaterialID: material.ID, ItemType: models.ReviewItemEntity, Content: []byte(`{}`), Confidence: 0.5, Status: models.ReviewStatusApproved}
	if err := db.Create(&review).Error; err != nil {
		t.Fatalf("create review: %v", err)
	}
}

func assertGraphCleanupCount(t *testing.T, db *gorm.DB, tenantID uint, expected int64) {
	t.Helper()
	for _, item := range []struct {
		name  string
		model interface{}
	}{
		{name: "ontologies", model: &models.Ontology{}},
		{name: "entity_types", model: &models.EntityType{}},
		{name: "relation_types", model: &models.RelationType{}},
		{name: "ontology_versions", model: &models.OntologyVersion{}},
		{name: "knowledge_graphs", model: &models.KnowledgeGraph{}},
		{name: "build_tasks", model: &models.BuildTask{}},
		{name: "build_materials", model: &models.BuildMaterial{}},
		{name: "review_items", model: &models.ReviewItem{}},
	} {
		var count int64
		if err := db.Model(item.model).Where("tenant_id = ?", tenantID).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", item.name, err)
		}
		if count != expected {
			t.Fatalf("expected tenant %d %s count %d, got %d", tenantID, item.name, expected, count)
		}
	}
}
