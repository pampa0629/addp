package service

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

func TestInstalledElementPublication(t *testing.T) {
	if os.Getenv("ADDP_TEST_STANDARD_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Standard deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "standard"}, "standard", &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewElementService(repository.NewElementRepository(db), repository.NewCodeSetRepository(db), repository.NewTenantReferenceRepository(db), nil)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const tenant, actor = 7, 9
	req := models.CreateElementRequest{Code: "customer_id", ScopeType: "tenant_common", Name: "客户编号", Definition: "客户稳定标识", DataType: "bigint", ValueDomainKind: "unrestricted", EffectiveFrom: &from}
	el, err := svc.CreateElement(&req, tenant, actor, "初始修订")
	if err != nil {
		t.Fatal(err)
	}
	revision := el.DraftRevision.ID
	review, err := svc.SubmitRevision(el.ID, revision, tenant, actor, el.Version)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishRevision(el.ID, revision, tenant, actor, review.Version)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetRevision(t.Context(), el.ID, revision, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if published.CurrentRevision == nil || published.CurrentRevision.ID != revision || got.ID != revision || got.RevisionNo != 1 || got.Status != "published" || got.ElementCode != req.Code || got.Name != req.Name || got.Definition != req.Definition || got.DataType != req.DataType || got.EffectiveFrom == nil || !got.EffectiveFrom.Equal(from) {
		t.Fatalf("published revision differs: %+v", got)
	}
	changes, err := repository.NewCatalogResourceRepository(db).ListChanges(t.Context(), tenant, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("missing Catalog change after publication")
	}
	last := changes[len(changes)-1]
	if last.SourceType != "element" || last.SourceIdentity != el.ID || last.Snapshot["revision_status"] != "published" || last.Snapshot["is_current"] != true || last.Snapshot["name"] != req.Name {
		t.Fatalf("invalid Catalog change: %+v", last)
	}
	// Catalog must retain the last name on deletion and roll back that
	// observation together with the owner mutation.
	rollback := errors.New("rollback verification")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := repository.NewElementRepository(tx).DeleteTx(tx, el.ID, tenant); err != nil {
			return err
		}
		deleted, err := repository.NewCatalogResourceRepository(tx).ListChanges(t.Context(), tenant, last.ID, 10)
		if err != nil {
			return err
		}
		if len(deleted) != 1 || deleted[0].Operation != "missing" || deleted[0].Snapshot["name"] != req.Name {
			return fmt.Errorf("missing Catalog deletion snapshot: %+v", deleted)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err = svc.GetRevision(t.Context(), el.ID, revision, tenant); err != nil {
		t.Fatal(err)
	}
	t.Run("domain reference evidence", func(t *testing.T) {
		checkInstalledDomainReferences(t, db, tenant, actor, el.ID)
	})
	t.Logf("exact revision: code=%s revision=%d status=%s name=%s definition=%s type=%s Catalog current=true", got.ElementCode, got.RevisionNo, got.Status, got.Name, got.Definition, got.DataType)
}

func checkInstalledDomainReferences(t *testing.T, db *gorm.DB, tenant, actor, publishedElementID int64) {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	create := func(row any) {
		t.Helper()
		if err := tx.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	domain := models.Domain{TenantID: tenant, Code: "orders", Name: "订单", CreatedBy: actor}
	create(&domain)
	glossary := models.Glossary{TenantID: tenant, ScopeType: "domain", OwnerDomainID: &domain.ID, Code: "orders_glossary", CreatedBy: actor}
	create(&glossary)
	from := time.Now().UTC().Add(-time.Hour)
	glossaryRevision := models.GlossaryRevision{GlossaryID: glossary.ID, RevisionNo: 1, Status: "published", Name: "生效术语", Definition: "订单", EffectiveFrom: &from, ChangeSummary: "first", CreatedBy: actor}
	create(&glossaryRevision)
	create(&models.GlossaryRevision{GlossaryID: glossary.ID, RevisionNo: 2, Status: "draft", Name: "后续草稿", Definition: "订单", ChangeSummary: "next", CreatedBy: actor})
	create(&models.GlossaryElementMapping{GlossaryID: glossary.ID, ElementID: publishedElementID})

	set := models.CodeSet{TenantID: tenant, Code: "order_states", CreatedBy: actor}
	create(&set)
	setRev := models.CodeSetRevision{CodeSetID: set.ID, RevisionNo: 1, Status: "published", Name: "状态码", ValueType: "string", EffectiveFrom: &from, CreatedBy: actor}
	create(&setRev)
	element := models.Element{TenantID: tenant, ScopeType: "domain", OwnerDomainID: &domain.ID, Code: "order_state", CreatedBy: actor}
	create(&element)
	elementRev := models.ElementRevision{ElementID: element.ID, RevisionNo: 1, Status: "draft", Name: "订单状态", Definition: "状态", DataType: "string", ValueDomainKind: "enumeration", CodeSetRevisionID: &setRev.ID, CreatedBy: actor}
	create(&elementRev)

	base := models.MetricDefinition{TenantID: tenant, Code: "order_amount", CreatedBy: actor}
	create(&base)
	derived := models.MetricDefinition{TenantID: tenant, ScopeType: "domain", OwnerDomainID: &domain.ID, Code: "order_total", CreatedBy: actor}
	create(&derived)
	metricRev := models.MetricDefinitionRevision{MetricDefinitionID: derived.ID, RevisionNo: 1, Status: "draft", MetricType: "derived", Name: "订单总额", Definition: "金额", StatisticalCaliber: "All", CreatedBy: actor}
	create(&metricRev)
	create(&models.MetricDefinitionRevisionDependency{MetricDefinitionRevisionID: metricRev.ID, DependencyDefinitionID: base.ID, RelationKind: "base"})

	repo := repository.NewCatalogResourceRepository(tx)
	for _, tc := range []struct {
		kind, sourceType                 string
		sourceID                         int64
		name, revision, resourceRevision string
	}{
		{"glossary", "element", publishedElementID, glossaryRevision.Name, "", ""},
		{"element", "code_set", set.ID, elementRev.Name, fmt.Sprint(setRev.ID), fmt.Sprint(elementRev.ID)},
		{"metric", "metric", base.ID, metricRev.Name, "", fmt.Sprint(metricRev.ID)},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			page, err := repo.ListDomainStandardReferences(t.Context(), tenant, domain.ID, tc.kind, 1, 5)
			if err != nil || page.Total != 1 || len(page.Data) != 1 {
				t.Fatalf("references=%+v err=%v", page, err)
			}
			item := page.Data[0]
			if item.SourceType != tc.sourceType || item.SourceIdentity != fmt.Sprint(tc.sourceID) || len(item.Evidence) != 1 {
				t.Fatalf("reference=%+v", item)
			}
			ev := item.Evidence[0]
			if ev.ResourceName != tc.name || ev.RevisionID != tc.revision || ev.ResourceRevisionID != tc.resourceRevision {
				t.Fatalf("evidence=%+v", ev)
			}
			other, err := repo.ListDomainStandardReferences(t.Context(), tenant+1, domain.ID, tc.kind, 1, 5)
			if err != nil || other.Total != 0 {
				t.Fatalf("other tenant=%+v err=%v", other, err)
			}
		})
	}
}
