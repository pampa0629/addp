package repository

import (
	"context"
	"fmt"
	"github.com/addp/standard/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestPostgresCatalogFiveStandardTypesCaptureRevisionsAndRollback(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	defer tx.Rollback()
	if err := Migrate(tx); err != nil {
		t.Fatal(err)
	}
	tenant := time.Now().UnixNano()
	identities := map[string]int64{}
	configs := [][4]string{{"metric", "metric_definitions", "metric_definition_revisions", "metric_definition_id"}, {"element", "elements", "element_revisions", "element_id"}, {"glossary", "glossaries", "glossary_revisions", "glossary_id"}, {"code_set", "code_sets", "code_set_revisions", "code_set_id"}, {"document", "documents", "document_revisions", "document_id"}}
	for _, cfg := range configs {
		var id int64
		if err := tx.Raw(fmt.Sprintf(`INSERT INTO standard.%s(tenant_id,code,scope_type,created_by,version,lifecycle_state,tags) VALUES (? ,?,'tenant_common',1,1,'active','[]') RETURNING id`, cfg[1]), tenant, "catalog_five_"+cfg[0]).Scan(&id).Error; err != nil {
			t.Fatal(err)
		}
		identities[cfg[0]] = id
		var revision any
		switch cfg[0] {
		case "metric":
			revision = &models.MetricDefinitionRevision{MetricDefinitionID: id, RevisionNo: 1, Status: "draft", MetricType: "atomic", Name: "Initial", Definition: "Definition", StatisticalCaliber: "All", ChangeSummary: "Initial", CreatedBy: 1}
		case "element":
			revision = &models.ElementRevision{ElementID: id, RevisionNo: 1, Status: "draft", Name: "Initial", Definition: "Definition", DataType: "string", ValueDomainKind: "unrestricted", ChangeSummary: "Initial", CreatedBy: 1}
		case "glossary":
			revision = &models.GlossaryRevision{GlossaryID: id, RevisionNo: 1, Status: "draft", Name: "Initial", Definition: "Definition", Alias: models.StringArray{}, RelatedIDs: models.Int64Array{}, ChangeSummary: "Initial", CreatedBy: 1}
		case "code_set":
			revision = &models.CodeSetRevision{CodeSetID: id, RevisionNo: 1, Status: "draft", Name: "Initial", Description: "Description", ValueType: "string", ChangeSummary: "Initial", CreatedBy: 1}
		case "document":
			revision = &models.DocumentRevision{DocumentID: id, RevisionNo: 1, Status: "draft", Name: "Initial", ChangeSummary: "Initial", CreatedBy: 1}
		}
		if err := tx.Create(revision).Error; err != nil {
			t.Fatalf("%s revision: %v", cfg[0], err)
		}
		if err := tx.Exec(fmt.Sprintf(`UPDATE standard.%s SET name='Changed' WHERE %s=?`, cfg[2], cfg[3]), id).Error; err != nil {
			t.Fatal(err)
		}
		references := []models.CatalogReference{{SourceType: cfg[0], SourceIdentity: fmt.Sprint(id)}}
		rows, err := NewCatalogResourceRepository(tx).ListResources(context.Background(), tenant, references)
		if err != nil || len(rows) != 1 || rows[0].Summary["name"] != "Changed" || rows[0].Summary["definition_complete"] != true {
			t.Fatalf("%s resolve=%+v err=%v", cfg[0], rows, err)
		}
		changes, err := NewCatalogResourceRepository(tx).ListChanges(context.Background(), tenant, 0, 100)
		if err != nil || len(changes) == 0 {
			t.Fatal(err)
		}
		last := changes[len(changes)-1]
		if last.SourceType != cfg[0] || last.Snapshot["name"] != "Changed" {
			t.Fatalf("revision did not capture current projection: %+v", last)
		}
		if err := tx.SavePoint("capture_rollback").Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.Exec(fmt.Sprintf(`UPDATE standard.%s SET name='Rolled back' WHERE %s=?`, cfg[2], cfg[3]), id).Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.RollbackTo("capture_rollback").Error; err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := tx.Table("standard.catalog_resource_changes").Where("tenant_id=? AND id>?", tenant, last.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rolled back changes=%d err=%v", count, err)
		}

		// A newer draft must not replace the currently effective definition in either route.
		if err := tx.Model(revision).Updates(map[string]any{"status": "published", "effective_from": time.Now().UTC().Add(-time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
		value := reflect.ValueOf(revision).Elem()
		value.FieldByName("ID").SetInt(0)
		value.FieldByName("RevisionNo").SetInt(2)
		value.FieldByName("Status").SetString("draft")
		value.FieldByName("Name").SetString("Next draft")
		if err := tx.Create(revision).Error; err != nil {
			t.Fatal(err)
		}
		rows, err = NewCatalogResourceRepository(tx).ListResources(context.Background(), tenant, references)
		if err != nil || rows[0].Summary["name"] != "Changed" || rows[0].Summary["is_current"] != true {
			t.Fatalf("%s current vs draft=%+v err=%v", cfg[0], rows, err)
		}
		var snapshot models.JSONB
		if err := tx.Raw(`SELECT snapshot FROM standard.catalog_resource_changes WHERE tenant_id=? AND source_type=? AND source_identity=? ORDER BY id DESC LIMIT 1`, tenant, cfg[0], id).Row().Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot["name"] != "Changed" || snapshot["is_current"] != true {
			t.Fatalf("%s capture differs from dynamic resolution: %+v", cfg[0], snapshot)
		}
	}
	domain := models.Domain{TenantID: tenant, Name: "Reference domain", Code: "catalog_five_domain", CreatedBy: 1}
	if err := tx.Create(&domain).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"glossaries", "elements", "metric_definitions"} {
		if err := tx.Exec("UPDATE standard."+table+" SET scope_type='domain',owner_domain_id=? WHERE tenant_id=?", domain.ID, tenant).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Create(&models.GlossaryElementMapping{GlossaryID: identities["glossary"], ElementID: identities["element"]}).Error; err != nil {
		t.Fatal(err)
	}
	var codeRevision int64
	if err := tx.Raw("SELECT id FROM standard.code_set_revisions WHERE code_set_id=? AND revision_no=1", identities["code_set"]).Scan(&codeRevision).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("UPDATE standard.element_revisions SET value_domain_kind='enumeration',code_set_revision_id=? WHERE element_id=? AND revision_no=1", codeRevision, identities["element"]).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewCatalogResourceRepository(tx)
	for _, kind := range []string{"glossary", "element", "metric"} {
		page, err := repo.ListDomainStandardReferences(context.Background(), tenant, domain.ID, kind, 1, 5)
		expected := int64(1)
		if kind == "metric" {
			expected = 0
		}
		if err != nil || page.Total != expected {
			t.Fatalf("%s references=%+v err=%v", kind, page, err)
		}
		if kind == "element" && (page.Data[0].SourceIdentity != fmt.Sprint(identities["code_set"]) || page.Data[0].Evidence[0].RevisionID != fmt.Sprint(codeRevision) || page.Data[0].Evidence[0].ResourceName != "Changed") {
			t.Fatalf("lost current pinned code set reference: %+v", page)
		}
		if kind == "glossary" && (page.Data[0].SourceIdentity != fmt.Sprint(identities["element"]) || page.Data[0].Evidence[0].ResourceRevisionID != "") {
			t.Fatalf("stable glossary mapping was presented as a revision fact: %+v", page)
		}
		other, err := repo.ListDomainStandardReferences(context.Background(), tenant+1, domain.ID, kind, 1, 5)
		if err != nil || other.Total != 0 {
			t.Fatalf("cross-tenant references: %+v err=%v", other, err)
		}
	}

}
