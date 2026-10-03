package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

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
