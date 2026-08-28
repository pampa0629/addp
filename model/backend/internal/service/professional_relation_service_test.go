package service

import (
	"strconv"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProfessionalRelationsExposeOwnerFactsWithoutCatalogCopies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:model-professional-relations?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS model`,
		`CREATE TABLE model.entities (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, code TEXT, status TEXT)`,
		`CREATE TABLE model.entity_relations (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, source_entity INTEGER NOT NULL, target_entity INTEGER NOT NULL, relation_type TEXT, name TEXT, description TEXT, created_at DATETIME)`,
		`CREATE TABLE model.logical_tables (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, entity_id INTEGER, name TEXT, code TEXT, status TEXT, table_type TEXT)`,
		`CREATE TABLE model.logical_fields (id INTEGER PRIMARY KEY, table_id INTEGER NOT NULL, element_revision_id INTEGER, name TEXT)`,
		`CREATE TABLE model.table_relations (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, source_table INTEGER NOT NULL, source_field INTEGER NOT NULL, target_table INTEGER NOT NULL, target_field INTEGER NOT NULL, relation_type TEXT)`,
		`CREATE TABLE model.fact_metric_mappings (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, fact_table_id INTEGER NOT NULL, metric_id INTEGER NOT NULL, field_id INTEGER, note TEXT, created_at DATETIME)`,
		`INSERT INTO model.entities VALUES (1, 7, 'Order', 'order', 'approved'), (2, 7, 'Customer', 'customer', 'approved'), (3, 8, 'Other', 'other', 'approved')`,
		`INSERT INTO model.entity_relations VALUES (11, 7, 1, 2, 'one_to_many', 'contains', 'Customer has orders', CURRENT_TIMESTAMP)`,
		`INSERT INTO model.logical_tables VALUES (21, 7, 1, 'Fact order', 'fact_order', 'approved', 'fact'), (22, 7, NULL, 'Dim customer', 'dim_customer', 'approved', 'dimension')`,
		`INSERT INTO model.logical_fields VALUES (31, 21, NULL, 'customer_id'), (32, 22, NULL, 'id')`,
		`INSERT INTO model.table_relations VALUES (41, 7, 21, 31, 22, 32, 'fk')`,
		`INSERT INTO model.fact_metric_mappings VALUES (51, 7, 21, 61, 31, 'amount metric', CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("execute %q: %v", statement, err)
		}
	}

	entityRepo := repository.NewEntityRepository(db)
	entityRelationService := NewEntityRelationService(repository.NewEntityRelationRepository(db), entityRepo)
	entityGraph, err := entityRelationService.GetProfessionalRelations(7, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if entityGraph.SchemaVersion != "addp.professional_relations/v1" || len(entityGraph.Nodes) != 2 || len(entityGraph.Edges) != 1 {
		t.Fatalf("entity graph = %#v", entityGraph)
	}
	if entityGraph.Edges[0].RelationKind != "model.entity.one_to_many" {
		t.Fatalf("entity relation kind = %q", entityGraph.Edges[0].RelationKind)
	}

	tableService := NewTableRelationService(repository.NewTableRelationRepository(db), repository.NewLogicalTableRepository(db))
	tableService.SetProfessionalRelationSources(entityRepo, repository.NewFactMetricRepository(db))
	tableGraph, err := tableService.GetProfessionalRelations(7, 21, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tableGraph.Edges) != 3 {
		t.Fatalf("logical table edges = %#v", tableGraph.Edges)
	}
	wantKinds := []string{"model.logical_table.entity", "model.logical_table.fk", "model.logical_table.supports_metric"}
	for index, want := range wantKinds {
		if tableGraph.Edges[index].RelationKind != want {
			t.Fatalf("edge %d kind = %q, want %q", index, tableGraph.Edges[index].RelationKind, want)
		}
	}
	if tableGraph.Edges[2].Target.OwnerModule != "standard" || tableGraph.Edges[2].Target.ResourceID != "61" {
		t.Fatalf("metric target = %#v", tableGraph.Edges[2].Target)
	}

	truncated, err := tableService.GetProfessionalRelations(7, 21, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated.Truncated || len(truncated.Edges) != 1 {
		t.Fatalf("truncated graph = %#v", truncated)
	}
}

func TestPostgresProfessionalRelationsUseOwnerSchemaAndTenantBoundary(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	entityRepo := repository.NewEntityRepository(tx)
	firstEntity := &models.Entity{TenantID: tenantID, Name: "PG Order", Code: "pg_order", Status: "approved", Version: 1, CreatedBy: 1}
	secondEntity := &models.Entity{TenantID: tenantID, Name: "PG Customer", Code: "pg_customer", Status: "approved", Version: 1, CreatedBy: 1}
	for _, entity := range []*models.Entity{firstEntity, secondEntity} {
		if err := entityRepo.Create(entity); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Create(&models.EntityRelation{
		TenantID: tenantID, SourceEntity: firstEntity.ID, TargetEntity: secondEntity.ID,
		RelationType: "one_to_many", Name: "contains", Version: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	entityGraph, err := NewEntityRelationService(repository.NewEntityRelationRepository(tx), entityRepo).
		GetProfessionalRelations(tenantID, firstEntity.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entityGraph.Edges) != 1 || entityGraph.Edges[0].Target.ResourceID != strconv.FormatInt(secondEntity.ID, 10) {
		t.Fatalf("entity graph = %#v", entityGraph)
	}

	layer := &models.DWLayer{TenantID: tenantID, LayerCode: "pg_rel", LayerName: "PG relation", Version: 1}
	if err := repository.NewDWLayerRepository(tx).Create(layer); err != nil {
		t.Fatal(err)
	}
	fact := &models.LogicalTable{
		TenantID: tenantID, EntityID: &firstEntity.ID, Name: "PG fact", Code: "pg_fact",
		TableType: "fact", Layer: layer.LayerCode, Status: "approved", Materialization: models.JSONB{}, Version: 1, CreatedBy: 1,
	}
	dimension := &models.LogicalTable{
		TenantID: tenantID, Name: "PG dimension", Code: "pg_dimension",
		TableType: "dimension", Layer: layer.LayerCode, Status: "approved", Materialization: models.JSONB{}, Version: 1, CreatedBy: 1,
	}
	for _, table := range []*models.LogicalTable{fact, dimension} {
		if err := tx.Create(table).Error; err != nil {
			t.Fatal(err)
		}
	}
	sourceField := &models.LogicalField{TableID: fact.ID, Name: "Customer ID", ColumnName: "customer_id", DataType: "bigint"}
	targetField := &models.LogicalField{TableID: dimension.ID, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: true}
	for _, field := range []*models.LogicalField{sourceField, targetField} {
		if err := tx.Create(field).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Create(&models.TableRelation{
		TenantID: tenantID, SourceTable: fact.ID, SourceField: sourceField.ID,
		TargetTable: dimension.ID, TargetField: targetField.ID, RelationType: "fk",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&models.FactMetricMapping{
		TenantID: tenantID, FactTableID: fact.ID, MetricID: 9001, FieldID: &sourceField.ID, CreatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	tableService := NewTableRelationService(repository.NewTableRelationRepository(tx), repository.NewLogicalTableRepository(tx))
	tableService.SetProfessionalRelationSources(entityRepo, repository.NewFactMetricRepository(tx))
	tableGraph, err := tableService.GetProfessionalRelations(tenantID, fact.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tableGraph.Edges) != 3 || tableGraph.Edges[2].Target.OwnerModule != "standard" {
		t.Fatalf("logical table graph = %#v", tableGraph)
	}
}
