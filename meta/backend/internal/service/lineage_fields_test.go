package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFieldLineageEvidenceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	// As in the Meta migration gate, schema changes are rolled back in addp_test.
	if err := tx.Exec("DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.LineageItemRelation{}, &models.LineageObservation{}); err != nil {
		t.Fatal(err)
	}
	endpoints := models.LineageFieldEndpoints{SourceFieldName: `name.with."quote`, TargetFieldName: "total", SourceSchemaHash: "source", TargetSchemaHash: "target"}
	relation := models.LineageItemRelation{TenantID: 7, SourceItemID: 1, TargetItemID: 2, RelationKind: "derive", Granularity: "field", LineageFieldEndpoints: endpoints}
	if err := tx.Create(&relation).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	for _, entry := range []struct {
		tenant uint
		at     time.Time
		id     string
	}{{7, at, "old"}, {7, at.Add(time.Minute), "new"}, {7, at.Add(time.Minute), "tie"}, {8, at.Add(2 * time.Minute), "foreign"}, {7, at.Add(-time.Minute), "late-old"}} {
		observation := models.LineageObservation{TenantID: entry.tenant, SourceItemID: &relation.SourceItemID, TargetItemID: &relation.TargetItemID, RelationKind: "derive", Granularity: "field", LineageFieldEndpoints: endpoints, SourceSnapshot: models.JSONMap{}, Evidence: models.JSONMap{"execution_id": entry.id}, ObservedAt: entry.at}
		if err := tx.Create(&observation).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewLineageService(tx, lineageTestEngineCatalog{})
	history := at.Add(30 * time.Second)
	for _, entry := range []struct {
		asOf *time.Time
		want string
	}{{nil, "tie"}, {&history, "old"}} {
		evidence, err := svc.fieldLineageEvidence(t.Context(), 7, []models.LineageItemRelation{relation}, models.LineageGraphRequest{AsOf: entry.asOf})
		if err != nil || len(evidence) != 1 || evidence[relation.ID].Evidence["execution_id"] != entry.want {
			t.Fatalf("evidence = %+v, err = %v, want %s", evidence, err, entry.want)
		}
	}
}

func TestFieldLineageBatchesEvidenceAndPreservesHistory(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "name.with.dot", `quoted"field`}
	sourceSchema := fieldTestSchema(t, names...)
	targetSchema := fieldTestSchema(t, "total")
	source := fieldTestItem(t, db, 7, "batch-source", sourceSchema)
	target := fieldTestItem(t, db, 7, "batch-target", targetSchema)
	at := time.Now().UTC().Add(-time.Hour)
	mappings := make([]commonExecution.LineageFieldMapping, 0, len(names))
	for _, name := range names {
		mappings = append(mappings, commonExecution.LineageFieldMapping{SourceField: name, TargetField: "total", Transformation: "derived"})
	}
	fieldTestExecution(t, db, "batch-old", source, target, sourceSchema, targetSchema, "replace", at, mappings...)
	if _, err := svc.CollectExecution(t.Context(), 7, "batch-old"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ id, transformation string }{{"batch-new", "direct"}, {"batch-tie", "derived"}} {
		fieldTestExecution(t, db, entry.id, source, target, sourceSchema, targetSchema, "append", at.Add(time.Minute),
			commonExecution.LineageFieldMapping{SourceField: "a", TargetField: "total", Transformation: entry.transformation})
		if _, err := svc.CollectExecution(t.Context(), 7, entry.id); err != nil {
			t.Fatal(err)
		}
	}
	var foreign models.LineageObservation
	if err := db.Where("execution_id = ? AND granularity = 'field'", "batch-tie").First(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	foreign.ID, foreign.TenantID, foreign.ObservedAt = 0, 8, at.Add(2*time.Minute)
	foreign.Evidence = models.JSONMap{"execution_id": "foreign", "transformation": "direct"}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	queries := 0
	const callback = "count-field-lineage-evidence"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		if !db.DryRun && strings.Contains(db.Statement.SQL.String(), "lineage_observations") {
			queries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	historical := at.Add(30 * time.Second)
	for _, asOf := range []*time.Time{nil, &historical} {
		queries = 0
		graph, err := svc.GetGraph(t.Context(), 7, models.LineageGraphRequest{
			SubjectKind: "field_ref", ItemID: &target.ID, FieldName: "total", Direction: "both", Depth: 2, Limit: 30, AsOf: asOf,
		})
		if err != nil || graph.Truncated || len(graph.Edges) != len(names) || len(graph.Nodes) != len(names)+1 {
			t.Fatalf("graph: %+v, %v", graph, err)
		}
		for _, edge := range graph.Edges {
			want := "batch-old"
			if asOf == nil && edge.Source.FieldName == "a" {
				want = "batch-tie"
			}
			if edge.Transformation != "derived" || edge.Evidence["execution_id"] != want {
				t.Fatalf("evidence for %q: %+v, want %s", edge.Source.FieldName, edge, want)
			}
		}
		if queries > 2 {
			t.Errorf("evidence queries = %d for %d edges, want at most one root proof and one batch", queries, len(names))
		}
	}
}

func fieldTestSchema(t *testing.T, names ...string) *commonExecution.LineageSchemaSnapshot {
	t.Helper()
	fields := make([]datatype.FieldInfo, 0, len(names))
	for _, name := range names {
		fields = append(fields, datatype.FieldInfo{Name: name, Type: datatype.FieldTypeString})
	}
	snapshot, err := commonExecution.NewLineageSchemaSnapshot(fields)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func fieldTestItem(t *testing.T, db *gorm.DB, tenant uint, name string, schema *commonExecution.LineageSchemaSnapshot) models.MetaItem {
	t.Helper()
	item := createLineageItem(t, db, tenant, name, "fp-"+name)
	item.Attributes = models.JSONMap{"type_info": map[string]interface{}{"table": map[string]interface{}{"fields": schema.Fields}}}
	if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func fieldTestExecution(t *testing.T, db *gorm.DB, id string, source, target models.MetaItem, sourceSchema, targetSchema *commonExecution.LineageSchemaSnapshot, mode string, at time.Time, mappings ...commonExecution.LineageFieldMapping) {
	t.Helper()
	insertLineageExecution(t, db, id, source.TenantID, source.ID, target.ID, mode)
	for i := range mappings {
		mappings[i].OutputPort = "target"
		if mappings[i].SourceField != "" {
			mappings[i].InputPort = "source"
		}
	}
	facts := commonExecution.LineageFacts{SchemaVersion: commonExecution.LineageFactsSchemaVersion,
		Inputs:     []commonExecution.LineageResourceRef{{Port: "source", ItemID: &source.ID, SchemaSnapshot: sourceSchema}},
		Outputs:    []commonExecution.LineageResourceRef{{Port: "target", ItemID: &target.ID, WriteMode: mode, SchemaSnapshot: targetSchema}},
		Operations: []commonExecution.LineageOperation{{Kind: "derive", InputPorts: []string{"source"}, OutputPorts: []string{"target"}, FieldLineageStatus: "complete", FieldMappings: mappings}}}
	metadata, err := json.Marshal(map[string]interface{}{"lineage_facts": facts})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", id).Updates(map[string]interface{}{"metadata": string(metadata), "completed_at": at}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestFieldLineageReplacementReplayAndGeneratedFields(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	sourceSchema := fieldTestSchema(t, "x", "y")
	targetSchema := fieldTestSchema(t, "a", "b", "constant")
	source := fieldTestItem(t, db, 7, "source-fields", sourceSchema)
	target := fieldTestItem(t, db, 7, "target-fields", targetSchema)
	at := time.Now().UTC().Add(-time.Hour)
	fieldTestExecution(t, db, "field-old", source, target, sourceSchema, targetSchema, "replace", at,
		commonExecution.LineageFieldMapping{SourceField: "x", TargetField: "a", Transformation: "direct"},
		commonExecution.LineageFieldMapping{SourceField: "y", TargetField: "b", Transformation: "derived"},
		commonExecution.LineageFieldMapping{TargetField: "constant", Transformation: "generated"})
	if result, err := svc.CollectExecution(context.Background(), 7, "field-old"); err != nil || result.Observed != 3 {
		t.Fatalf("collect: %+v %v", result, err)
	}
	request := models.LineageGraphRequest{SubjectKind: "field_ref", ItemID: &target.ID, FieldName: "a", Direction: "both", Depth: 2, Limit: 20}
	graph, err := svc.GetGraph(context.Background(), 7, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].Source.FieldName != "x" || graph.FieldLineageStatus != "complete" {
		t.Fatalf("graph: %+v", graph)
	}
	request.FieldName = "constant"
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 0 || graph.FieldLineageStatus != "complete" {
		t.Fatalf("generated: %+v %v", graph, err)
	}
	fieldTestExecution(t, db, "field-new", source, target, sourceSchema, targetSchema, "replace", at.Add(time.Minute), commonExecution.LineageFieldMapping{SourceField: "y", TargetField: "a", Transformation: "direct"})
	if _, err := svc.CollectExecution(context.Background(), 7, "field-new"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CollectExecution(context.Background(), 7, "field-old"); err != nil {
		t.Fatal(err)
	}
	request.FieldName = "a"
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].Source.FieldName != "y" {
		t.Fatalf("replay restored old mapping: %+v", graph)
	}
	request.AsOf = &at
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].Source.FieldName != "x" {
		t.Fatalf("history: %+v %v", graph, err)
	}
	itemGraph, err := svc.GetGraph(context.Background(), 7, models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &target.ID, Direction: "both", Depth: 2, Limit: 20})
	if err != nil || len(itemGraph.Edges) != 1 {
		t.Fatalf("item graph contains field edges: %+v %v", itemGraph, err)
	}
	if _, err := svc.GetGraph(context.Background(), 8, request); err == nil {
		t.Fatal("foreign tenant saw fields")
	}
}

func TestFieldLineageAppendLateCollectionAndValidation(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	schema := fieldTestSchema(t, "id", "name")
	source := fieldTestItem(t, db, 7, "input", schema)
	target := fieldTestItem(t, db, 7, "output", schema)
	at := time.Now().UTC().Add(-time.Hour)
	for index, mode := range []string{"replace", "append"} {
		name := []string{"id", "name"}[index]
		id := []string{"first", "second"}[index]
		fieldTestExecution(t, db, id, source, target, schema, schema, mode, at.Add(time.Duration(index)*time.Minute), commonExecution.LineageFieldMapping{SourceField: name, TargetField: "id", Transformation: "direct"})
		if _, err := svc.CollectExecution(context.Background(), 7, id); err != nil {
			t.Fatal(err)
		}
	}
	request := models.LineageGraphRequest{SubjectKind: "field_ref", ItemID: &target.ID, FieldName: "id", Direction: "upstream", Depth: 2, Limit: 20}
	graph, err := svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 2 {
		t.Fatalf("append: %+v %v", graph, err)
	}
	fieldTestExecution(t, db, "late", source, target, schema, schema, "replace", at.Add(-time.Minute), commonExecution.LineageFieldMapping{SourceField: "name", TargetField: "name", Transformation: "direct"})
	if _, err := svc.CollectExecution(context.Background(), 7, "late"); err != nil {
		t.Fatal(err)
	}
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 2 {
		t.Fatalf("late replace corrupted current projection: %+v %v", graph, err)
	}
	fieldTestExecution(t, db, "invalid", source, target, schema, schema, "replace", at.Add(2*time.Minute), commonExecution.LineageFieldMapping{SourceField: "absent", TargetField: "id", Transformation: "direct"})
	if _, err := svc.CollectExecution(context.Background(), 7, "invalid"); err == nil {
		t.Fatal("invalid field accepted")
	}
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 2 {
		t.Fatalf("invalid facts changed projection: %+v %v", graph, err)
	}
	request.Limit = 1
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || !graph.Truncated || len(graph.Nodes) != 1 || len(graph.Edges) != 0 {
		t.Fatalf("limits: %+v %v", graph, err)
	}
}

func TestFieldLineageKeepsPathsAndSnapshotsSeparate(t *testing.T) {
	db := openLineageTestDB(t)
	svc := NewLineageService(db, lineageTestEngineCatalog{})
	schema := fieldTestSchema(t, "id", "other")
	a := fieldTestItem(t, db, 7, "path-a", schema)
	b := fieldTestItem(t, db, 7, "path-b", schema)
	c := fieldTestItem(t, db, 7, "path-c", schema)
	at := time.Now().UTC().Add(-time.Hour)
	fieldTestExecution(t, db, "a-b", a, b, schema, schema, "replace", at, commonExecution.LineageFieldMapping{SourceField: "id", TargetField: "id", Transformation: "direct"})
	fieldTestExecution(t, db, "b-c", b, c, schema, schema, "replace", at.Add(time.Minute), commonExecution.LineageFieldMapping{SourceField: "other", TargetField: "id", Transformation: "direct"})
	for _, id := range []string{"a-b", "b-c"} {
		if _, err := svc.CollectExecution(context.Background(), 7, id); err != nil {
			t.Fatal(err)
		}
	}
	request := models.LineageGraphRequest{SubjectKind: "field_ref", ItemID: &c.ID, FieldName: "id", Direction: "upstream", Depth: 3, Limit: 20}
	graph, err := svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].Source.FieldName != "other" {
		t.Fatalf("crossed another field: %+v %v", graph, err)
	}
	request.ItemID = &b.ID
	request.FieldName = "id"
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 1 {
		t.Fatalf("matching path missing: %+v %v", graph, err)
	}
	changed := fieldTestSchema(t, "id", "other", "new")
	b.Attributes = models.JSONMap{"type_info": map[string]interface{}{"table": map[string]interface{}{"fields": changed.Fields}}}
	if err := db.Model(&b).Update("attributes", b.Attributes).Error; err != nil {
		t.Fatal(err)
	}
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 0 || graph.FieldLineageStatus != "unavailable" {
		t.Fatalf("current structure used old facts: %+v %v", graph, err)
	}
	request.SchemaSnapshotHash = schema.Hash
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 1 {
		t.Fatalf("immutable snapshot unavailable: %+v %v", graph, err)
	}
	// Current staleness does not delete historical evidence at a frozen identity.
	if err := db.Model(&models.LineageItemRelation{}).Where("target_item_id = ? AND granularity = 'field'", b.ID).Updates(map[string]interface{}{"status": "stale", "updated_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	past := at.Add(30 * time.Second)
	request.AsOf = &past
	graph, err = svc.GetGraph(context.Background(), 7, request)
	if err != nil || len(graph.Edges) != 1 || graph.Edges[0].Status != "stale" {
		t.Fatalf("schema change deleted historical evidence: %+v %v", graph, err)
	}
	request.AsOf = nil
	request.SchemaSnapshotHash = "unproven"
	if _, err = svc.GetGraph(context.Background(), 7, request); err == nil {
		t.Fatal("invented snapshot accepted")
	}
	// Collecting a new execution against an old schema cannot reactivate that schema.
	fieldTestExecution(t, db, "old-schema", a, b, schema, schema, "append", at.Add(2*time.Minute), commonExecution.LineageFieldMapping{SourceField: "other", TargetField: "id", Transformation: "direct"})
	if _, err := svc.CollectExecution(context.Background(), 7, "old-schema"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.LineageItemRelation{}).Where("target_item_id = ? AND granularity = 'field' AND source_field_name = 'other' AND status = 'active'", b.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("old schema activated: %d %v", count, err)
	}
}
