package repository_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCatalogDeploymentDeclarationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", "system")
	u.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	reset := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Error(err)
		}
	}
	reset()
	defer reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	// This is the only newly released machine permission. A declaration must
	// not add it to any human role or to Catalog's separate runtime identity.
	var roleKeys []string
	if err := db.Raw(`SELECT r.role_key FROM system.roles r
	 JOIN system.role_permissions rp ON rp.role_id=r.id
	 JOIN system.permissions p ON p.id=rp.permission_id
	 WHERE p.permission_key='meta.data_item_identity.read' ORDER BY r.role_key`).Scan(&roleKeys).Error; err != nil {
		t.Fatal(err)
	}
	if len(roleKeys) != 1 || roleKeys[0] != "tenant.system_runtime" {
		t.Fatalf("unexpected identity enumeration roles: %v", roleKeys)
	}
	var restricted int64
	if err := db.Raw(`SELECT count(*) FROM system.permissions WHERE permission_key='meta.data_item_identity.read'
	 AND NOT tenant_customizable AND NOT delegable AND allowed_scope_types=ARRAY['tenant']::text[]`).Scan(&restricted).Error; err != nil || restricted != 1 {
		t.Fatalf("identity permission scope: %d %v", restricted, err)
	}
	repo := repository.NewModuleRegistryRepository(db)
	type result struct {
		enabled bool
		row     *models.ModuleDefinition
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, enabled := range []bool{true, false} {
		go func(value bool) {
			<-start
			row, err := repo.CreateCatalogDefinition(value)
			results <- result{value, row, err}
		}(enabled)
	}
	close(start)
	successes, conflicts := 0, 0
	var winner bool
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			winner = result.enabled
			if result.row.Enabled != result.enabled || result.row.Version != 1 {
				t.Fatalf("wrong creation: %#v", result.row)
			}
		} else if errors.Is(result.err, repository.ErrModuleDefinitionVersionConflict) {
			conflicts++
		} else {
			t.Fatal(result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
	got, err := repo.GetModule("catalog")
	if err != nil || got.Enabled != winner {
		t.Fatalf("winner: %#v %v", got, err)
	}
	var count int64
	if err := db.Model(&models.ModuleRuntimeInstance{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("declaration created runtime: %d %v", count, err)
	}
	revision, err := repo.GetRegistryRevision()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateCatalogDefinition(!winner); !errors.Is(err, repository.ErrModuleDefinitionVersionConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	after, err := repo.GetRegistryRevision()
	if err != nil || after != revision {
		t.Fatalf("conflict changed revision: %d -> %d, %v", revision, after, err)
	}
	if _, _, err := repo.UpdateEnabled("catalog", !winner, got.Version); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetModule("catalog")
	if err != nil || got.Enabled == winner || got.Version != 2 {
		t.Fatalf("explicit update: %#v %v", got, err)
	}
}
