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
	t.Logf("exact revision: code=%s revision=%d status=%s name=%s definition=%s type=%s Catalog current=true", got.ElementCode, got.RevisionNo, got.Status, got.Name, got.Definition, got.DataType)
}
