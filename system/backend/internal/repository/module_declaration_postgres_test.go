package repository_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func moduleRegistryTestDB(t *testing.T) (*gorm.DB, context.Context) {
	t.Helper()
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
	query.Set("application_name", t.Name())
	u.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	reset := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Error(err)
		}
	}
	reset()
	t.Cleanup(reset)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func TestCatalogDeploymentDeclarationAgainstPostgres(t *testing.T) {
	db, _ := moduleRegistryTestDB(t)
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

func TestDiscoveryWriterFailoverAgainstPostgres(t *testing.T) {
	db, ctx := moduleRegistryTestDB(t)
	first, second := repository.NewModuleRegistryRepository(db), repository.NewModuleRegistryRepository(db)
	if _, err := first.CreateCatalogDefinition(true); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ModuleDefinition{}).Where("module_name = ?", "catalog").Update("declaration_published_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	endpoints := []commonmodels.ModuleRoutingEndpoint{{ModuleName: "catalog", EndpointID: "pod/http", URL: "http://example.test:8192", ExpiresAt: time.Now().UTC().Add(time.Minute)}}
	var last int64
	readBoth := func(count int) {
		t.Helper()
		a, err := first.RoutingSnapshot(ctx, "bound")
		if err != nil {
			t.Fatal(err)
		}
		b, err := second.RoutingSnapshot(ctx, "bound")
		if err != nil || b.Revision != a.Revision || a.Revision <= last || len(a.Endpoints) != count || len(b.Endpoints) != count {
			t.Fatalf("readers %+v %+v, previous revision %d: %v", a, b, last, err)
		}
		last = a.Revision
	}
	err := first.WithDiscoveryWriter(ctx, "catalog", "bound", func(old func([]commonmodels.ModuleRoutingEndpoint) error) error {
		if err := old(endpoints); err != nil {
			return err
		}
		readBoth(1)
		if err := second.WithDiscoveryWriter(ctx, "catalog", "bound", func(func([]commonmodels.ModuleRoutingEndpoint) error) error {
			return errors.New("two simultaneous discovery writers")
		}); err != nil {
			return err
		}
		// Target only this test's locked backend in its disposable database.
		var stopped bool
		if err := db.Raw(`SELECT pg_terminate_backend(a.pid) FROM pg_stat_activity a JOIN pg_locks l USING (pid)
            WHERE a.datname=current_database() AND a.application_name=? AND l.locktype='advisory' AND l.granted`, t.Name()).Scan(&stopped).Error; err != nil || !stopped {
			t.Fatalf("writer disconnect: %v %v", stopped, err)
		}
		// Wait for the server to observe disconnection before asking for takeover.
		if err := old(endpoints); err == nil {
			t.Fatal("disconnected writer published")
		}
		tookOver := false
		err := second.WithDiscoveryWriter(ctx, "catalog", "bound", func(next func([]commonmodels.ModuleRoutingEndpoint) error) error {
			tookOver = true
			if err := next([]commonmodels.ModuleRoutingEndpoint{}); err != nil {
				return err
			}
			if err := old(endpoints); err == nil {
				t.Fatal("old writer restored withdrawn endpoint")
			}
			readBoth(0)
			return nil
		})
		if !tookOver {
			t.Fatal("standby failed to take over")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
