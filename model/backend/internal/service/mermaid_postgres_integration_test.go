package service

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresMermaidRoundTripAndRevisionConflict(t *testing.T) {
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

	tenantID := time.Now().UnixNano()
	userID := tenantID + 1
	domainID := tenantID + 2
	elementID := tenantID + 3
	entityRepo := repository.NewEntityRepository(tx)
	relationRepo := repository.NewEntityRelationRepository(tx)
	svc := NewEntityService(entityRepo, relationRepo)

	source := models.Entity{
		TenantID: tenantID, DomainID: &domainID, Name: "PostgreSQL Customer",
		Code: "pg_customer", Description: "customer round-trip description",
		Status: "draft", Version: 1, CreatedBy: userID,
	}
	target := models.Entity{
		TenantID: tenantID, Name: "PostgreSQL Order", Code: "pg_order",
		Description: "order round-trip description", Status: "draft", Version: 1, CreatedBy: userID,
	}
	if err := tx.Create(&source).Error; err != nil {
		t.Fatalf("create source entity: %v", err)
	}
	if err := tx.Create(&target).Error; err != nil {
		t.Fatalf("create target entity: %v", err)
	}
	attribute := models.EntityAttribute{
		EntityID: source.ID, ElementID: &elementID, Name: "PostgreSQL Customer ID",
		ColumnName: "customer_id", DataType: "bigint", IsPK: true, Nullable: true,
		Description: "attribute round-trip description", SortOrder: 7,
	}
	if err := tx.Create(&attribute).Error; err != nil {
		t.Fatalf("create source attribute: %v", err)
	}
	relation := models.EntityRelation{
		TenantID: tenantID, SourceEntity: source.ID, TargetEntity: target.ID,
		RelationType: "one_to_many", Name: "places", Description: "relation round-trip description", Version: 1,
	}
	if err := tx.Create(&relation).Error; err != nil {
		t.Fatalf("create relation: %v", err)
	}

	exported, err := svc.ExportToMermaid(tenantID)
	if err != nil {
		t.Fatalf("export Mermaid snapshot: %v", err)
	}
	result, err := svc.ImportFromMermaid(tenantID, userID, &models.MermaidImportRequest{
		MermaidCode: exported.MermaidCode,
		Revision:    exported.Revision,
	})
	if err != nil {
		t.Fatalf("import exported Mermaid snapshot: %v", err)
	}
	if result.CreatedEntities != 2 || result.CreatedRelations != 1 || result.Revision != exported.Revision+1 {
		t.Fatalf("round-trip result = %+v, exported revision = %d", result, exported.Revision)
	}

	reloadedSource, err := entityRepo.GetByCode(tenantID, source.Code)
	if err != nil {
		t.Fatalf("reload round-trip source entity: %v", err)
	}
	reloadedTarget, err := entityRepo.GetByCode(tenantID, target.Code)
	if err != nil {
		t.Fatalf("reload round-trip target entity: %v", err)
	}
	if reloadedSource.Name != source.Name || !reflect.DeepEqual(reloadedSource.DomainID, source.DomainID) ||
		reloadedSource.Description != source.Description || reloadedTarget.Name != target.Name ||
		reloadedTarget.Description != target.Description {
		t.Fatalf("round-trip entities = (%+v, %+v), want editable fields from (%+v, %+v)", reloadedSource, reloadedTarget, source, target)
	}
	attributes, err := entityRepo.GetAttributes(reloadedSource.ID)
	if err != nil {
		t.Fatalf("reload round-trip attributes: %v", err)
	}
	if len(attributes) != 1 {
		t.Fatalf("round-trip attribute count = %d, want 1", len(attributes))
	}
	actualAttribute := attributes[0]
	if actualAttribute.Name != attribute.Name || actualAttribute.ColumnName != attribute.ColumnName ||
		actualAttribute.DataType != attribute.DataType || actualAttribute.IsPK != attribute.IsPK ||
		actualAttribute.Nullable != attribute.Nullable || !reflect.DeepEqual(actualAttribute.ElementID, attribute.ElementID) ||
		actualAttribute.Description != attribute.Description || actualAttribute.SortOrder != attribute.SortOrder {
		t.Fatalf("round-trip attribute = %+v, want editable fields from %+v", actualAttribute, attribute)
	}
	relations, err := relationRepo.ListByTenantID(tenantID)
	if err != nil {
		t.Fatalf("reload round-trip relations: %v", err)
	}
	if len(relations) != 1 || relations[0].SourceEntity != reloadedSource.ID ||
		relations[0].TargetEntity != reloadedTarget.ID || relations[0].RelationType != relation.RelationType ||
		relations[0].Name != relation.Name || relations[0].Description != relation.Description {
		t.Fatalf("round-trip relations = %+v, want editable fields from %+v", relations, relation)
	}

	staleSnapshot, err := svc.ExportToMermaid(tenantID)
	if err != nil {
		t.Fatalf("export stale Mermaid snapshot: %v", err)
	}
	updated, err := svc.UpdateEntity(reloadedSource.ID, tenantID, userID, &models.UpdateEntityRequest{
		Version: reloadedSource.Version, DomainID: reloadedSource.DomainID,
		Name: reloadedSource.Name, Description: "newer PostgreSQL write",
	})
	if err != nil {
		t.Fatalf("advance entity after Mermaid export: %v", err)
	}
	_, err = svc.ImportFromMermaid(tenantID, userID, &models.MermaidImportRequest{
		MermaidCode: staleSnapshot.MermaidCode,
		Revision:    staleSnapshot.Revision,
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	reloadedAfterConflict, err := entityRepo.GetByID(updated.ID, tenantID)
	if err != nil {
		t.Fatalf("reload entity after rejected stale import: %v", err)
	}
	if reloadedAfterConflict.Description != "newer PostgreSQL write" || reloadedAfterConflict.Version != updated.Version {
		t.Fatalf("entity changed after rejected stale import: %+v", reloadedAfterConflict)
	}
}
