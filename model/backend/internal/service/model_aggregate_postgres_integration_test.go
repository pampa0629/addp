package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func beginModelAggregatePostgresTransaction(t *testing.T) (*gorm.DB, int64) {
	t.Helper()
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run model migrations: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin PostgreSQL test transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	return tx, time.Now().UnixNano()
}

func TestPostgresLogicalTableAggregateRejectsStaleFieldAndTableWrites(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	userID := tenantID + 1
	layerRepo := repository.NewDWLayerRepository(tx)
	if err := layerRepo.Create(&models.DWLayer{
		TenantID: tenantID, LayerCode: "pg_dwd", LayerName: "PostgreSQL DWD", Version: 1,
	}); err != nil {
		t.Fatalf("create PostgreSQL DW layer: %v", err)
	}

	tableRepo := repository.NewLogicalTableRepository(tx)
	svc := NewLogicalTableService(tableRepo, repository.NewEntityRepository(tx), layerRepo)
	table, err := svc.CreateLogicalTable(&models.CreateLogicalTableRequest{
		Name: "PostgreSQL Orders", Code: "pg_orders", TableType: "entity", Layer: "pg_dwd",
		Materialization: map[string]interface{}{},
	}, tenantID, userID)
	if err != nil {
		t.Fatalf("create PostgreSQL logical table: %v", err)
	}
	if table.Version != 1 {
		t.Fatalf("created logical table version = %d, want 1", table.Version)
	}

	firstField, err := svc.CreateField(table.ID, tenantID, &models.CreateLogicalFieldRequest{
		Version: 1, Name: "Order ID", ColumnName: "order_id", DataType: "bigint",
		IsPK: true, FieldRole: "regular",
	})
	if err != nil {
		t.Fatalf("create first PostgreSQL logical field: %v", err)
	}
	if firstField.Version != 2 {
		t.Fatalf("version after first field = %d, want 2", firstField.Version)
	}

	_, err = svc.CreateField(table.ID, tenantID, &models.CreateLogicalFieldRequest{
		Version: 1, Name: "Stale Field", ColumnName: "stale_field", DataType: "string",
		FieldRole: "regular",
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	fieldsAfterConflict, err := svc.GetFields(table.ID, tenantID)
	if err != nil {
		t.Fatalf("reload fields after stale create: %v", err)
	}
	if len(fieldsAfterConflict) != 1 || fieldsAfterConflict[0].ColumnName != "order_id" {
		t.Fatalf("fields after stale create = %+v, want only order_id", fieldsAfterConflict)
	}
	reloadedTable, err := tableRepo.GetByID(table.ID, tenantID)
	if err != nil {
		t.Fatalf("reload table after stale field create: %v", err)
	}
	if reloadedTable.Version != 2 {
		t.Fatalf("table version after stale field create = %d, want 2", reloadedTable.Version)
	}

	secondField, err := svc.CreateField(table.ID, tenantID, &models.CreateLogicalFieldRequest{
		Version: firstField.Version, Name: "Order Code", ColumnName: "order_code", DataType: "string",
		FieldRole: "regular", SortOrder: 1,
	})
	if err != nil {
		t.Fatalf("retry second PostgreSQL logical field: %v", err)
	}
	if secondField.Version != 3 {
		t.Fatalf("version after retried field = %d, want 3", secondField.Version)
	}

	scdType := 0
	_, err = svc.UpdateLogicalTable(table.ID, tenantID, userID, &models.UpdateLogicalTableRequest{
		Version: 2, Name: table.Name, Description: "stale table description", TableType: table.TableType,
		Layer: table.Layer, SCDType: &scdType, Materialization: map[string]interface{}{},
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	reloadedTable, err = tableRepo.GetByID(table.ID, tenantID)
	if err != nil {
		t.Fatalf("reload table after stale table update: %v", err)
	}
	if reloadedTable.Version != 3 || reloadedTable.Description != "" {
		t.Fatalf("table changed after stale update: %+v", reloadedTable)
	}

	updatedTable, err := svc.UpdateLogicalTable(table.ID, tenantID, userID, &models.UpdateLogicalTableRequest{
		Version: secondField.Version, Name: table.Name, Description: "current table description", TableType: table.TableType,
		Layer: table.Layer, SCDType: &scdType, Materialization: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("update logical table with current version: %v", err)
	}
	if updatedTable.Version != 4 || updatedTable.Description != "current table description" {
		t.Fatalf("updated logical table = %+v, want version 4 and current description", updatedTable)
	}
}

func TestPostgresLogicalTableAssociationsShareAggregateVersion(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	userID := tenantID + 1
	layer := models.DWLayer{
		TenantID: tenantID, LayerCode: "pg_dws", LayerName: "PostgreSQL DWS", Version: 1,
	}
	if err := tx.Create(&layer).Error; err != nil {
		t.Fatalf("create association test DW layer: %v", err)
	}
	fact := models.LogicalTable{
		TenantID: tenantID, Name: "PostgreSQL Sales Fact", Code: "pg_sales_fact", TableType: "fact",
		Layer: layer.LayerCode, Status: "draft", GrainDescription: "one row per sale", Version: 1,
		Materialization: models.JSONB{}, CreatedBy: userID,
	}
	dimension := models.LogicalTable{
		TenantID: tenantID, Name: "PostgreSQL Customer Dimension", Code: "pg_customer_dimension",
		TableType: "dimension", Layer: layer.LayerCode, Status: "draft", Version: 1,
		Materialization: models.JSONB{}, CreatedBy: userID,
	}
	if err := tx.Create(&fact).Error; err != nil {
		t.Fatalf("create association test fact table: %v", err)
	}
	if err := tx.Create(&dimension).Error; err != nil {
		t.Fatalf("create association test dimension table: %v", err)
	}
	factField := models.LogicalField{
		TableID: fact.ID, Name: "Customer ID", ColumnName: "customer_id", DataType: "bigint",
		FieldRole: "dimension_fk",
	}
	dimensionField := models.LogicalField{
		TableID: dimension.ID, Name: "Customer ID", ColumnName: "customer_id", DataType: "bigint",
		IsPK: true, FieldRole: "regular",
	}
	if err := tx.Create(&factField).Error; err != nil {
		t.Fatalf("create association test fact field: %v", err)
	}
	if err := tx.Create(&dimensionField).Error; err != nil {
		t.Fatalf("create association test dimension field: %v", err)
	}

	tableRepo := repository.NewLogicalTableRepository(tx)
	relationRepo := repository.NewTableRelationRepository(tx)
	metricRepo := repository.NewFactMetricRepository(tx)
	relationSvc := NewTableRelationService(relationRepo, tableRepo)
	metricSvc := NewFactMetricService(metricRepo, tableRepo)

	relationResult, err := relationSvc.AddDimensionRelation(fact.ID, tenantID, &models.CreateTableRelationRequest{
		Version: 1, TargetTable: dimension.ID, SourceField: factField.ID,
		TargetField: dimensionField.ID, RelationType: "fk",
	})
	if err != nil {
		t.Fatalf("add PostgreSQL dimension relation: %v", err)
	}
	if relationResult.Version != 2 {
		t.Fatalf("version after dimension relation = %d, want 2", relationResult.Version)
	}

	metricID := tenantID + 2
	_, err = metricSvc.AddMetric(fact.ID, tenantID, userID, &models.CreateFactMetricMappingRequest{
		Version: 1, MetricID: metricID, FieldID: &factField.ID, Note: "stale metric mapping",
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	assertFactMetricMappingCount(t, tx, fact.ID, tenantID, 0)
	var staleGuardCount int64
	if err := tx.Model(&models.StandardReferenceGuard{}).
		Where("tenant_id = ? AND resource_type = ? AND resource_id = ?", tenantID, models.StandardResourceMetric, metricID).
		Count(&staleGuardCount).Error; err != nil {
		t.Fatalf("count reference guards after stale metric mapping: %v", err)
	}
	if staleGuardCount != 0 {
		t.Fatalf("reference guard count after stale metric mapping = %d, want 0", staleGuardCount)
	}

	metricResult, err := metricSvc.AddMetric(fact.ID, tenantID, userID, &models.CreateFactMetricMappingRequest{
		Version: relationResult.Version, MetricID: metricID, FieldID: &factField.ID, Note: "current metric mapping",
	})
	if err != nil {
		t.Fatalf("add PostgreSQL fact metric mapping: %v", err)
	}
	if metricResult.Version != 3 {
		t.Fatalf("version after metric mapping = %d, want 3", metricResult.Version)
	}

	_, err = relationSvc.RemoveDimensionRelation(relationResult.Relation.ID, fact.ID, tenantID, relationResult.Version)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	_, err = metricSvc.RemoveMetric(metricResult.Mapping.ID, fact.ID, tenantID, relationResult.Version)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	assertTableRelationCount(t, tx, fact.ID, tenantID, 1)
	assertFactMetricMappingCount(t, tx, fact.ID, tenantID, 1)

	metricDelete, err := metricSvc.RemoveMetric(metricResult.Mapping.ID, fact.ID, tenantID, metricResult.Version)
	if err != nil {
		t.Fatalf("remove PostgreSQL fact metric mapping: %v", err)
	}
	if metricDelete.Version != 4 {
		t.Fatalf("version after metric removal = %d, want 4", metricDelete.Version)
	}
	relationDelete, err := relationSvc.RemoveDimensionRelation(
		relationResult.Relation.ID, fact.ID, tenantID, metricDelete.Version,
	)
	if err != nil {
		t.Fatalf("remove PostgreSQL dimension relation: %v", err)
	}
	if relationDelete.Version != 5 {
		t.Fatalf("version after relation removal = %d, want 5", relationDelete.Version)
	}
	assertTableRelationCount(t, tx, fact.ID, tenantID, 0)
	assertFactMetricMappingCount(t, tx, fact.ID, tenantID, 0)

	reloadedFact, err := tableRepo.GetByID(fact.ID, tenantID)
	if err != nil {
		t.Fatalf("reload fact table after association mutations: %v", err)
	}
	reloadedDimension, err := tableRepo.GetByID(dimension.ID, tenantID)
	if err != nil {
		t.Fatalf("reload dimension table after association mutations: %v", err)
	}
	if reloadedFact.Version != 5 || reloadedDimension.Version != 1 {
		t.Fatalf("association aggregate versions = fact:%d dimension:%d, want 5 and 1", reloadedFact.Version, reloadedDimension.Version)
	}
}

func TestPostgresEntityRelationUsesVersionAndAdvancesCollectionRevision(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	userID := tenantID + 1
	entities := []models.Entity{
		{TenantID: tenantID, Name: "PostgreSQL Customer", Code: "pg_relation_customer", Status: "draft", Version: 1, CreatedBy: userID},
		{TenantID: tenantID, Name: "PostgreSQL Order", Code: "pg_relation_order", Status: "draft", Version: 1, CreatedBy: userID},
		{TenantID: tenantID, Name: "PostgreSQL Product", Code: "pg_relation_product", Status: "draft", Version: 1, CreatedBy: userID},
	}
	for index := range entities {
		if err := tx.Create(&entities[index]).Error; err != nil {
			t.Fatalf("create entity relation endpoint %d: %v", index, err)
		}
	}

	relationRepo := repository.NewEntityRelationRepository(tx)
	svc := NewEntityRelationService(relationRepo, repository.NewEntityRepository(tx))
	relation, err := svc.Create(tenantID, &models.CreateEntityRelationRequest{
		SourceEntity: entities[0].ID, TargetEntity: entities[1].ID,
		RelationType: "one_to_many", Name: "places", Description: "customer places order",
	})
	if err != nil {
		t.Fatalf("create PostgreSQL entity relation: %v", err)
	}
	if relation.Version != 1 {
		t.Fatalf("created entity relation version = %d, want 1", relation.Version)
	}
	assertEntityModelRevision(t, tx, tenantID, 2)

	updated, err := svc.Update(relation.ID, tenantID, &models.UpdateEntityRelationRequest{
		Version: relation.Version, SourceEntity: entities[1].ID, TargetEntity: entities[2].ID,
		RelationType: "many_to_many", Name: "contains", Description: "order contains products",
	})
	if err != nil {
		t.Fatalf("update PostgreSQL entity relation endpoints: %v", err)
	}
	if updated.Version != 2 || updated.SourceEntity != entities[1].ID || updated.TargetEntity != entities[2].ID ||
		updated.RelationType != "many_to_many" || updated.Name != "contains" || updated.Description != "order contains products" {
		t.Fatalf("updated entity relation = %+v", updated)
	}
	assertEntityModelRevision(t, tx, tenantID, 3)

	_, err = svc.Update(relation.ID, tenantID, &models.UpdateEntityRelationRequest{
		Version: 1, SourceEntity: entities[0].ID, TargetEntity: entities[2].ID,
		RelationType: "one_to_one", Name: "stale", Description: "stale update",
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	if err := svc.Delete(relation.ID, tenantID, 1); err == nil {
		t.Fatal("stale entity relation delete succeeded")
	} else {
		requireDomainErrorCode(t, err, "resource_version_conflict")
	}
	reloaded, err := relationRepo.GetByID(relation.ID, tenantID)
	if err != nil {
		t.Fatalf("reload entity relation after stale writes: %v", err)
	}
	if reloaded.Version != 2 || reloaded.SourceEntity != entities[1].ID || reloaded.TargetEntity != entities[2].ID || reloaded.Name != "contains" {
		t.Fatalf("entity relation changed after stale writes: %+v", reloaded)
	}
	assertEntityModelRevision(t, tx, tenantID, 3)

	_, err = svc.Update(relation.ID, tenantID+1000, &models.UpdateEntityRelationRequest{
		Version: 2, SourceEntity: entities[1].ID, TargetEntity: entities[2].ID,
		RelationType: "many_to_many", Name: "foreign", Description: "foreign tenant probe",
	})
	requireDomainErrorCode(t, err, "entity_relation_not_found")

	if err := tx.Model(&models.Entity{}).Where("id = ?", entities[2].ID).Update("status", "approved").Error; err != nil {
		t.Fatalf("approve relation endpoint fixture: %v", err)
	}
	_, err = svc.Update(relation.ID, tenantID, &models.UpdateEntityRelationRequest{
		Version: 1, SourceEntity: entities[1].ID, TargetEntity: entities[2].ID,
		RelationType: "many_to_many", Name: "stale", Description: "stale before state",
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	_, err = svc.Update(relation.ID, tenantID, &models.UpdateEntityRelationRequest{
		Version: 2, SourceEntity: entities[1].ID, TargetEntity: entities[2].ID,
		RelationType: "many_to_many", Name: "blocked", Description: "state conflict",
	})
	requireDomainErrorCode(t, err, "entity_relation_state_conflict")
	assertEntityModelRevision(t, tx, tenantID, 3)

	if err := tx.Model(&models.Entity{}).Where("id = ?", entities[2].ID).Update("status", "draft").Error; err != nil {
		t.Fatalf("reopen relation endpoint fixture: %v", err)
	}
	if err := svc.Delete(relation.ID, tenantID, 2); err != nil {
		t.Fatalf("delete PostgreSQL entity relation: %v", err)
	}
	assertEntityModelRevision(t, tx, tenantID, 4)
	if _, err := relationRepo.GetByID(relation.ID, tenantID); err == nil {
		t.Fatal("deleted entity relation still exists")
	}
}

func TestPostgresLogicalTableDeleteAdvancesEachSurvivingFactOnce(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	userID := tenantID + 1
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "pg_delete", LayerName: "PostgreSQL Delete", Version: 1}
	if err := tx.Create(&layer).Error; err != nil {
		t.Fatalf("create delete test DW layer: %v", err)
	}
	tables := []models.LogicalTable{
		{TenantID: tenantID, Name: "Dimension To Delete", Code: "pg_delete_dimension", TableType: "dimension", Layer: layer.LayerCode, Status: "draft", Version: 2, Materialization: models.JSONB{}, CreatedBy: userID},
		{TenantID: tenantID, Name: "First Surviving Fact", Code: "pg_delete_fact_one", TableType: "fact", Layer: layer.LayerCode, Status: "draft", GrainDescription: "one", Version: 4, Materialization: models.JSONB{}, CreatedBy: userID},
		{TenantID: tenantID, Name: "Second Surviving Fact", Code: "pg_delete_fact_two", TableType: "fact", Layer: layer.LayerCode, Status: "draft", GrainDescription: "two", Version: 7, Materialization: models.JSONB{}, CreatedBy: userID},
		{TenantID: tenantID, Name: "Unrelated Fact", Code: "pg_delete_unrelated", TableType: "fact", Layer: layer.LayerCode, Status: "draft", GrainDescription: "unrelated", Version: 9, Materialization: models.JSONB{}, CreatedBy: userID},
	}
	for index := range tables {
		if err := tx.Create(&tables[index]).Error; err != nil {
			t.Fatalf("create delete test logical table %d: %v", index, err)
		}
	}
	fields := []models.LogicalField{
		{TableID: tables[0].ID, Name: "Dimension ID", ColumnName: "dimension_id", DataType: "bigint", IsPK: true},
		{TableID: tables[0].ID, Name: "Dimension Code", ColumnName: "dimension_code", DataType: "string", IsPK: true},
		{TableID: tables[1].ID, Name: "First Dimension ID", ColumnName: "dimension_id", DataType: "bigint", FieldRole: "dimension_fk"},
		{TableID: tables[1].ID, Name: "First Dimension Code", ColumnName: "dimension_code", DataType: "string", FieldRole: "dimension_fk"},
		{TableID: tables[2].ID, Name: "Second Dimension ID", ColumnName: "dimension_id", DataType: "bigint", FieldRole: "dimension_fk"},
		{TableID: tables[2].ID, Name: "Second Dimension Code", ColumnName: "dimension_code", DataType: "string", FieldRole: "dimension_fk"},
	}
	for index := range fields {
		if err := tx.Create(&fields[index]).Error; err != nil {
			t.Fatalf("create delete test logical field %d: %v", index, err)
		}
	}
	relations := []models.TableRelation{
		{TenantID: tenantID, SourceTable: tables[1].ID, SourceField: fields[2].ID, TargetTable: tables[0].ID, TargetField: fields[0].ID, RelationType: "fk"},
		{TenantID: tenantID, SourceTable: tables[1].ID, SourceField: fields[3].ID, TargetTable: tables[0].ID, TargetField: fields[1].ID, RelationType: "fk"},
		{TenantID: tenantID, SourceTable: tables[2].ID, SourceField: fields[4].ID, TargetTable: tables[0].ID, TargetField: fields[0].ID, RelationType: "fk"},
		{TenantID: tenantID, SourceTable: tables[2].ID, SourceField: fields[5].ID, TargetTable: tables[0].ID, TargetField: fields[1].ID, RelationType: "fk"},
	}
	if err := tx.Create(&relations).Error; err != nil {
		t.Fatalf("create delete test table relations: %v", err)
	}

	svc := NewLogicalTableService(repository.NewLogicalTableRepository(tx), repository.NewEntityRepository(tx), repository.NewDWLayerRepository(tx))
	err := svc.DeleteLogicalTable(tables[0].ID, tenantID, 1)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	assertLogicalTableVersions(t, tx, tenantID, map[int64]int64{
		tables[0].ID: 2, tables[1].ID: 4, tables[2].ID: 7, tables[3].ID: 9,
	})
	var relationCount int64
	if err := tx.Model(&models.TableRelation{}).Where("tenant_id = ? AND target_table = ?", tenantID, tables[0].ID).Count(&relationCount).Error; err != nil {
		t.Fatalf("count relations after stale logical table delete: %v", err)
	}
	if relationCount != 4 {
		t.Fatalf("relation count after stale logical table delete = %d, want 4", relationCount)
	}

	if err := svc.DeleteLogicalTable(tables[0].ID, tenantID, 2); err != nil {
		t.Fatalf("delete PostgreSQL dimension table: %v", err)
	}
	assertLogicalTableVersions(t, tx, tenantID, map[int64]int64{
		tables[1].ID: 5, tables[2].ID: 8, tables[3].ID: 9,
	})
	if err := tx.Model(&models.TableRelation{}).Where("tenant_id = ? AND target_table = ?", tenantID, tables[0].ID).Count(&relationCount).Error; err != nil {
		t.Fatalf("count relations after logical table delete: %v", err)
	}
	if relationCount != 0 {
		t.Fatalf("relation count after logical table delete = %d, want 0", relationCount)
	}
	var deletedCount int64
	if err := tx.Model(&models.LogicalTable{}).Where("id = ? AND tenant_id = ?", tables[0].ID, tenantID).Count(&deletedCount).Error; err != nil {
		t.Fatalf("count deleted logical table: %v", err)
	}
	if deletedCount != 0 {
		t.Fatalf("deleted logical table count = %d, want 0", deletedCount)
	}
}

func assertEntityModelRevision(t *testing.T, db *gorm.DB, tenantID, expected int64) {
	t.Helper()
	var revision models.EntityModelRevision
	if err := db.Where("tenant_id = ?", tenantID).First(&revision).Error; err != nil {
		t.Fatalf("reload entity model revision: %v", err)
	}
	if revision.Revision != expected {
		t.Fatalf("entity model revision = %d, want %d", revision.Revision, expected)
	}
}

func assertLogicalTableVersions(t *testing.T, db *gorm.DB, tenantID int64, expected map[int64]int64) {
	t.Helper()
	for id, expectedVersion := range expected {
		var table models.LogicalTable
		if err := db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&table).Error; err != nil {
			t.Fatalf("reload logical table %d: %v", id, err)
		}
		if table.Version != expectedVersion {
			t.Fatalf("logical table %d version = %d, want %d", id, table.Version, expectedVersion)
		}
	}
}

func assertTableRelationCount(t *testing.T, db *gorm.DB, factTableID, tenantID, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.TableRelation{}).
		Where("source_table = ? AND tenant_id = ?", factTableID, tenantID).Count(&count).Error; err != nil {
		t.Fatalf("count table relations: %v", err)
	}
	if count != expected {
		t.Fatalf("table relation count = %d, want %d", count, expected)
	}
}

func assertFactMetricMappingCount(t *testing.T, db *gorm.DB, factTableID, tenantID, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.FactMetricMapping{}).
		Where("fact_table_id = ? AND tenant_id = ?", factTableID, tenantID).Count(&count).Error; err != nil {
		t.Fatalf("count fact metric mappings: %v", err)
	}
	if count != expected {
		t.Fatalf("fact metric mapping count = %d, want %d", count, expected)
	}
}

func TestPostgresDWLayerRejectsStaleWritesBeforeReferenceConflict(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	userID := tenantID + 1
	layerRepo := repository.NewDWLayerRepository(tx)
	svc := NewDWLayerService(layerRepo)
	layer, err := svc.CreateDWLayer(&models.CreateDWLayerRequest{
		LayerCode: "pg_ads", LayerName: "PostgreSQL ADS", QualitySLA: map[string]interface{}{}, SortOrder: 1,
	}, tenantID)
	if err != nil {
		t.Fatalf("create PostgreSQL DW layer: %v", err)
	}

	sortOrder := 2
	updatedLayer, err := svc.UpdateDWLayer(layer.ID, tenantID, &models.UpdateDWLayerRequest{
		Version: 1, LayerName: "PostgreSQL Application Layer", Description: "current layer description",
		QualitySLA: map[string]interface{}{"freshness_hours": 24}, SortOrder: &sortOrder,
	})
	if err != nil {
		t.Fatalf("update PostgreSQL DW layer: %v", err)
	}
	if updatedLayer.Version != 2 {
		t.Fatalf("updated DW layer version = %d, want 2", updatedLayer.Version)
	}

	_, err = svc.UpdateDWLayer(layer.ID, tenantID, &models.UpdateDWLayerRequest{
		Version: 1, LayerName: "Stale Layer Name", Description: "stale layer description",
		QualitySLA: map[string]interface{}{}, SortOrder: &sortOrder,
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	reloadedLayer, err := layerRepo.GetByID(layer.ID, tenantID)
	if err != nil {
		t.Fatalf("reload layer after stale update: %v", err)
	}
	if reloadedLayer.Version != 2 || reloadedLayer.LayerName != "PostgreSQL Application Layer" ||
		reloadedLayer.Description != "current layer description" {
		t.Fatalf("DW layer changed after stale update: %+v", reloadedLayer)
	}

	_, err = svc.UpdateDWLayer(layer.ID, tenantID+100, &models.UpdateDWLayerRequest{
		Version: 2, LayerName: "Foreign Tenant Probe", QualitySLA: map[string]interface{}{}, SortOrder: &sortOrder,
	})
	requireDomainErrorCode(t, err, "dw_layer_not_found")

	tableSvc := NewLogicalTableService(
		repository.NewLogicalTableRepository(tx), repository.NewEntityRepository(tx), layerRepo,
	)
	if _, err := tableSvc.CreateLogicalTable(&models.CreateLogicalTableRequest{
		Name: "PostgreSQL Application Table", Code: "pg_application_table", TableType: "entity",
		Layer: layer.LayerCode, Materialization: map[string]interface{}{},
	}, tenantID, userID); err != nil {
		t.Fatalf("create logical table referencing DW layer: %v", err)
	}

	err = svc.DeleteDWLayer(layer.ID, tenantID, 1)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	err = svc.DeleteDWLayer(layer.ID, tenantID, updatedLayer.Version)
	requireDomainErrorCode(t, err, "dw_layer_in_use")
	reloadedLayer, err = layerRepo.GetByID(layer.ID, tenantID)
	if err != nil {
		t.Fatalf("reload layer after rejected deletes: %v", err)
	}
	if reloadedLayer.Version != 2 || reloadedLayer.LayerName != "PostgreSQL Application Layer" {
		t.Fatalf("DW layer changed after rejected deletes: %+v", reloadedLayer)
	}
}

func TestPostgresLogicalCleanupAdvancesVersionsAndEntityModelRevision(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	entityID, tableID := seedModelCleanupTenantState(t, tx, tenantID)

	stats, err := NewCleanupService(tx, nil, nil).ExecuteCleanup(
		context.Background(), uint(tenantID), events.CleanupModeLogical,
		map[string]interface{}{"tenant_id": uint(tenantID)},
	)
	if err != nil {
		t.Fatalf("execute PostgreSQL logical cleanup: %v", err)
	}
	if len(stats.Errors) != 0 || stats.DraftedEntities != 1 || stats.DraftedTables != 2 {
		t.Fatalf("PostgreSQL logical cleanup stats = %+v", stats)
	}

	var entity models.Entity
	if err := tx.First(&entity, entityID).Error; err != nil {
		t.Fatalf("reload logically cleaned entity: %v", err)
	}
	if entity.Status != "draft" || entity.Version != 2 {
		t.Fatalf("logically cleaned entity = %+v, want draft version 2", entity)
	}
	var table models.LogicalTable
	if err := tx.First(&table, tableID).Error; err != nil {
		t.Fatalf("reload logically cleaned table: %v", err)
	}
	if table.Status != "draft" || table.Version != 2 {
		t.Fatalf("logically cleaned table = %+v, want draft version 2", table)
	}
	var revision models.EntityModelRevision
	if err := tx.Where("tenant_id = ?", tenantID).First(&revision).Error; err != nil {
		t.Fatalf("reload entity model revision after logical cleanup: %v", err)
	}
	if revision.Revision != 2 {
		t.Fatalf("entity model revision after logical cleanup = %d, want 2", revision.Revision)
	}
}

func TestPostgresPhysicalCleanupDeletesOnlyTargetTenantAtomically(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	otherTenantID := tenantID + 1000
	seedModelCleanupTenantState(t, tx, tenantID)
	seedModelCleanupTenantState(t, tx, otherTenantID)

	stats, err := NewCleanupService(tx, nil, nil).ExecuteCleanup(
		context.Background(), uint(tenantID), events.CleanupModePhysical,
		map[string]interface{}{"tenant_id": uint(tenantID)},
	)
	if err != nil {
		t.Fatalf("execute PostgreSQL physical cleanup: %v", err)
	}
	if len(stats.Errors) != 0 || stats.DeletedRecords != 11 {
		t.Fatalf("PostgreSQL physical cleanup stats = %+v, want 11 deleted records", stats)
	}
	assertModelCleanupCount(t, tx, modelCleanupCountExpectation{tenantID: tenantID})
	assertModelCleanupCount(t, tx, modelCleanupCountExpectation{
		tenantID:           otherTenantID,
		dwLayers:           1,
		entities:           2,
		entityAttributes:   1,
		entityRelations:    1,
		logicalTables:      2,
		logicalFields:      2,
		tableRelations:     1,
		factMetricMappings: 1,
	})
	var revision models.EntityModelRevision
	if err := tx.Where("tenant_id = ?", tenantID).First(&revision).Error; err != nil {
		t.Fatalf("reload entity model revision after physical cleanup: %v", err)
	}
	if revision.Revision != 2 {
		t.Fatalf("entity model revision after physical cleanup = %d, want 2", revision.Revision)
	}
}
