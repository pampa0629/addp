package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresDocumentCandidateComparisonTargets(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	code := fmt.Sprintf("outdoor_comparison_%d", tenantID)
	category := models.MeasurementCategory{TenantID: tenantID, Name: "次数", Code: code, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Version: 1}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	unit := models.Unit{TenantID: tenantID, CategoryID: category.ID, Name: "次", Symbol: "次", Version: 1}
	if err := db.Create(&unit).Error; err != nil {
		t.Fatal(err)
	}

	effectiveFrom := time.Now().UTC().Add(-time.Hour)
	glossary := models.Glossary{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: code, CreatedBy: 1, Version: 1, LifecycleState: "active"}
	if err := db.Create(&glossary).Error; err != nil {
		t.Fatal(err)
	}
	glossaryRevision := models.GlossaryRevision{GlossaryID: glossary.ID, RevisionNo: 1, Status: models.RevisionStatusPublished, Name: "户外活动", Definition: "在户外开展的活动", ChangeSummary: "initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1}
	if err := db.Create(&glossaryRevision).Error; err != nil {
		t.Fatal(err)
	}

	element := models.Element{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: code, CreatedBy: 1, Version: 1, LifecycleState: "active"}
	elementRevision := models.ElementRevision{ElementID: 0, RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "活动次数", Definition: "参加活动的次数", DataType: "integer", ValueDomainKind: models.ValueDomainUnrestricted, UnitID: &unit.ID, ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&element).Error; err != nil {
		t.Fatal(err)
	}
	elementRevision.ElementID = element.ID
	if err := db.Create(&elementRevision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&element).Update("draft_revision_id", elementRevision.ID).Error; err != nil {
		t.Fatal(err)
	}

	codeSet := models.CodeSet{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: code, Origin: models.CodeSetOriginTenant, CreatedBy: 1, Version: 1, LifecycleState: "active"}
	codeSetRevision := models.CodeSetRevision{RevisionNo: 1, Status: models.RevisionStatusDraft, Name: "活动状态", Description: "户外活动状态", ValueType: "string", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&codeSet).Error; err != nil {
		t.Fatal(err)
	}
	codeSetRevision.CodeSetID = codeSet.ID
	if err := db.Create(&codeSetRevision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&codeSet).Update("draft_revision_id", codeSetRevision.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CodeSetRevisionItem{CodeSetRevisionID: codeSetRevision.ID, Code: "open", Label: "进行中", Status: models.CodeItemStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&elementRevision).Updates(map[string]interface{}{
		"value_domain_kind":    models.ValueDomainEnumeration,
		"code_set_revision_id": codeSetRevision.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	metric := models.MetricDefinition{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: code, CreatedBy: 1, Version: 1, LifecycleState: "active"}
	metricRevision := models.MetricDefinitionRevision{RevisionNo: 1, Status: models.RevisionStatusDraft, MetricType: models.MetricTypeAtomic, Name: "活动参与次数", Definition: "参加活动总次数", StatisticalCaliber: "有效活动", SemanticFormula: "count(*)", UnitID: &unit.ID, ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&metric).Error; err != nil {
		t.Fatal(err)
	}
	metricRevision.MetricDefinitionID = metric.ID
	if err := db.Create(&metricRevision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&metric).Update("draft_revision_id", metricRevision.ID).Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM standard.metric_definitions WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.elements WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.code_sets WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.glossaries WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.units WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM standard.measurement_categories WHERE tenant_id = ?", tenantID).Error
	})

	targets, err := NewDocumentRepository(db).ListCandidateComparisonTargets(tenantID, map[string][]string{
		"glossary": {code}, "element": {code}, "code_set": {code}, "metric": {code},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 4 {
		t.Fatalf("targets=%#v", targets)
	}
	if target := targets[documentCandidateComparisonKey("glossary", code)]; target.RevisionID != glossaryRevision.ID || target.RevisionStatus != models.RevisionStatusPublished {
		t.Fatalf("glossary target=%+v", target)
	}
	if target := targets[documentCandidateComparisonKey("element", code)]; target.RevisionID != elementRevision.ID || target.UnitName != "次" || target.ValueDomainKind != models.ValueDomainEnumeration || target.CodeSetCode != code {
		t.Fatalf("element target=%+v", target)
	}
	if target := targets[documentCandidateComparisonKey("code_set", code)]; target.RevisionID != codeSetRevision.ID || len(target.Items) != 1 || target.Items[0].Code != "open" {
		t.Fatalf("code set target=%+v", target)
	}
	if target := targets[documentCandidateComparisonKey("metric", code)]; target.RevisionID != metricRevision.ID || target.SemanticFormula != "count(*)" || target.UnitSymbol != "次" {
		t.Fatalf("metric target=%+v", target)
	}
}
