package service

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

func TestInstalledModelEntity(t *testing.T) {
	if os.Getenv("ADDP_TEST_MODEL_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Model deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "model"}, "model", &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewEntityService(repository.NewEntityRepository(db), repository.NewEntityRelationRepository(db))
	const tenant, actor = 7, 9
	input := models.CreateEntityRequest{Code: "customer", Name: "客户", Description: "模型部署验证"}
	e, err := svc.CreateEntity(&input, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetEntity(e.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != e.ID || got.Name != input.Name || got.Code != input.Code || got.Description != input.Description || got.Status != "draft" || got.Version != 1 || got.TenantID != tenant {
		t.Fatalf("unexpected persisted entity: %+v", got)
	}
	if _, err := svc.GetEntity(e.ID, tenant+1); err == nil {
		t.Fatal("entity visible in a different tenant")
	}
	changes, err := repository.NewCatalogResourceRepository(db).ListChanges(t.Context(), tenant, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].SourceIdentity != e.ID || changes[0].Operation != "upsert" || changes[0].Snapshot["name"] != e.Name {
		t.Fatalf("unexpected Catalog changes: %+v", changes)
	}
	verifyInstalledModelConstraints(t, db, e.ID)
	t.Run("entity_relation_revision", func(t *testing.T) {
		const relationTenant = 11
		exerciseEntityRelationRevision(t, db, relationTenant)
	})
	t.Logf("entity=%d code=%s name=%s version=%d status=%s Catalog=%s", e.ID, got.Code, got.Name, got.Version, got.Status, changes[0].Operation)
}

func verifyInstalledModelConstraints(t *testing.T, db *gorm.DB, entityID int64) {
	t.Helper()
	if err := db.Exec("INSERT INTO model.entity_attributes(entity_id,element_id,name,column_name,data_type) VALUES (?,1,'ID','customer_id','bigint')", entityID).Error; err != nil {
		t.Fatal(err)
	}
	approve := func() error { return db.Exec("UPDATE model.entities SET status='approved' WHERE id=?", entityID).Error }
	if err := approve(); !errors.Is(err, gorm.ErrCheckConstraintViolated) && (err == nil || !strings.Contains(err.Error(), "freeze") && !strings.Contains(err.Error(), "unfrozen")) {
		t.Fatalf("unfrozen revision not rejected by owner constraint: %v", err)
	}
	if err := db.Exec("UPDATE model.entity_attributes SET element_revision_id=1 WHERE entity_id=?", entityID).Error; err != nil {
		t.Fatal(err)
	}
	if err := approve(); err != nil {
		t.Fatal(err)
	}
	insert := func(tenant int, code, target string) error {
		return db.Exec("INSERT INTO model.logical_tables(tenant_id,code,name,table_type,created_by,materialization) VALUES (?,?,'Customer','entity',9,?)", tenant, code, target).Error
	}
	const parent = "addp://engine/1/path/public?type=schema"
	const physical = `{"target_parent_locator":"` + parent + `","target_name":"customer"}`
	if err := insert(7, "table_a", physical); err != nil {
		t.Fatal(err)
	}
	if err := insert(8, "table_b", physical); !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("physical target must be unique across tenants: %v", err)
	}
	const unbound = `{"target_parent_locator":null,"target_name":"customer"}`
	for _, code := range []string{"unbound_a", "unbound_b"} {
		if err := insert(8, code, unbound); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewLogicalTableRepository(db)
	var table models.LogicalTable
	if err := db.Where("tenant_id=? AND code=?", 7, "table_a").First(&table).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		exclude int64
		want    bool
	}{{"customer", 0, true}, {"customer", table.ID, false}, {"customer_history", 0, false}} {
		found, err := repo.ExistsByPhysicalTarget(parent, tc.name, tc.exclude)
		if err != nil || found != tc.want {
			t.Fatalf("target %s, exclude %d: found=%v want=%v err=%v", tc.name, tc.exclude, found, tc.want, err)
		}
	}
	if err := db.Model(&models.LogicalTable{}).Where("code IN ?", []string{"table_a", "unbound_a", "unbound_b"}).Update("status", "approved").Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMaterializationService(nil, repo, nil)
	items, total, err := svc.ListMaterializationTasks(t.Context(), 7, 1, 1)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != table.ID {
		t.Fatalf("approved materialization list: count=%d records=%+v err=%v", total, items, err)
	}
	items, total, err = svc.ListMaterializationTasks(t.Context(), 8, 1, 10)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("unbound targets or other tenant returned: count=%d records=%+v err=%v", total, items, err)
	}

}
