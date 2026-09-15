package service

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresMermaidIncrementalImportAndRevisionConflict(t *testing.T) {
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
	otherDomainID := tenantID + 4
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
		TenantID: tenantID, DomainID: &otherDomainID, Name: "PostgreSQL Order", Code: "pg_order",
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

	exported, err := svc.ExportToMermaid(tenantID, &domainID)
	if err != nil {
		t.Fatalf("export domain Mermaid document: %v", err)
	}
	if exported.Scope != "domain" || exported.DomainID == nil || *exported.DomainID != domainID ||
		!strings.Contains(exported.Markdown, "```mermaid") || strings.Contains(exported.Markdown, target.Code) {
		t.Fatalf("domain export = %+v, want only domain %d", exported, domainID)
	}
	preview, err := svc.PreviewMermaidImport(tenantID, &models.MermaidImportPreviewRequest{Markdown: exported.Markdown})
	if err != nil {
		t.Fatalf("preview exported Mermaid document: %v", err)
	}
	if preview.CreatedEntities != 0 || preview.UnchangedEntities != 1 || preview.CreatedRelations != 0 || len(preview.Conflicts) != 0 {
		t.Fatalf("domain import preview = %+v, want one unchanged entity", preview)
	}
	result, err := svc.ImportFromMermaid(tenantID, userID, &models.MermaidImportRequest{Markdown: exported.Markdown, Revision: preview.Revision})
	if err != nil || result.Revision != preview.Revision || result.UnchangedEntities != 1 {
		t.Fatalf("no-op import result = %+v, err = %v", result, err)
	}

	additiveMarkdown := fmt.Sprintf("# ADDP Entity Relationship Diagram\n\n```mermaid\nerDiagram\n  %%%% addp:document {\"format\":\"addp.model.er/v1\",\"scope\":\"all\"}\n  %%%% addp:entity {\"code\":\"%s\",\"name\":\"%s\",\"domain_id\":%d,\"description\":\"%s\"}\n  %s {\n    %%%% addp:attribute {\"entity\":\"%s\",\"column\":\"customer_id\",\"name\":\"PostgreSQL Customer ID\",\"nullable\":true,\"element_id\":%d,\"description\":\"attribute round-trip description\",\"sort_order\":7}\n    bigint customer_id PK\n  }\n  %%%% addp:entity {\"code\":\"pg_invoice\",\"name\":\"PostgreSQL Invoice\",\"domain_id\":null,\"description\":\"new invoice\"}\n  pg_invoice {\n  }\n  %%%% addp:relation {\"source\":\"%s\",\"target\":\"pg_invoice\",\"relation_type\":\"one_to_many\",\"name\":\"billed_as\",\"description\":\"new relation\"}\n  %s ||--o{ pg_invoice : \"billed_as\"\n```\n",
		source.Code, source.Name, domainID, source.Description, source.Code, source.Code, elementID, source.Code, source.Code)
	additivePreview, err := svc.PreviewMermaidImport(tenantID, &models.MermaidImportPreviewRequest{Markdown: additiveMarkdown})
	if err != nil {
		t.Fatalf("preview additive import: %v", err)
	}
	if additivePreview.CreatedEntities != 1 || additivePreview.UnchangedEntities != 1 || additivePreview.CreatedRelations != 1 || len(additivePreview.Conflicts) != 0 {
		t.Fatalf("additive preview = %+v", additivePreview)
	}
	additiveResult, err := svc.ImportFromMermaid(tenantID, userID, &models.MermaidImportRequest{Markdown: additiveMarkdown, Revision: additivePreview.Revision})
	if err != nil || additiveResult.CreatedEntities != 1 || additiveResult.CreatedRelations != 1 || additiveResult.Revision != additivePreview.Revision+1 {
		t.Fatalf("additive result = %+v, err = %v", additiveResult, err)
	}
	reloadedSource, err := entityRepo.GetByCode(tenantID, source.Code)
	if err != nil || reloadedSource.ID != source.ID {
		t.Fatalf("existing source was replaced: %+v, err = %v", reloadedSource, err)
	}
	if _, err := entityRepo.GetByCode(tenantID, target.Code); err != nil {
		t.Fatalf("out-of-document entity was removed: %v", err)
	}
	relations, err := relationRepo.ListByTenantID(tenantID)
	if err != nil || len(relations) != 2 {
		t.Fatalf("relations after additive import = %+v, err = %v", relations, err)
	}

	conflictMarkdown := strings.Replace(additiveMarkdown, source.Name, "Conflicting Customer", 1)
	conflictPreview, err := svc.PreviewMermaidImport(tenantID, &models.MermaidImportPreviewRequest{Markdown: conflictMarkdown})
	if err != nil || len(conflictPreview.Conflicts) == 0 || conflictPreview.Conflicts[0].ResourceType != "entity" {
		t.Fatalf("conflict preview = %+v, err = %v", conflictPreview, err)
	}
	_, err = svc.ImportFromMermaid(tenantID, userID, &models.MermaidImportRequest{Markdown: conflictMarkdown, Revision: conflictPreview.Revision})
	requireDomainErrorCode(t, err, "mermaid_import_conflict")

	staleSnapshot, err := svc.ExportToMermaid(tenantID, nil)
	if err != nil {
		t.Fatalf("export stale Mermaid document: %v", err)
	}
	stalePreview, err := svc.PreviewMermaidImport(tenantID, &models.MermaidImportPreviewRequest{Markdown: staleSnapshot.Markdown})
	if err != nil {
		t.Fatalf("preview stale Mermaid document: %v", err)
	}
	updated, err := svc.UpdateEntity(reloadedSource.ID, tenantID, userID, &models.UpdateEntityRequest{
		Version: reloadedSource.Version, DomainID: reloadedSource.DomainID,
		Name: reloadedSource.Name, Description: "newer PostgreSQL write",
	})
	if err != nil {
		t.Fatalf("advance entity after Mermaid export: %v", err)
	}
	_, err = svc.ImportFromMermaid(tenantID, userID, &models.MermaidImportRequest{
		Markdown: staleSnapshot.Markdown,
		Revision: stalePreview.Revision,
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
