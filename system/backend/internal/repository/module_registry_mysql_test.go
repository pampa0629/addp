package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestModuleRegistryAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_SYSTEM_REGISTRY") != "1" {
		t.Skip("requires the disposable System module registry gate")
	}
	driver, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"), DBName: "system",
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	var pending *migration.PendingMySQLSystemTriggers
	if err := migration.MigrateMySQLModuleRegistry(db); !errors.As(err, &pending) || len(pending.Statements) != 2 {
		t.Fatalf("expected two pending registry triggers, got %v", err)
	}
	for _, ddl := range pending.Statements {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migration.MigrateMySQLModuleRegistry(db); err != nil {
		t.Fatal(err)
	}
	if err := migration.MigrateMySQLModuleRegistry(db); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	repo := repository.NewModuleRegistryRepository(db)
	route := "/meta"
	fields := models.ModuleDeclarationFields{RoutePrefix: &route}
	if _, err := repo.PublishDeclaration("meta", 0, fields); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PublishDeclaration("meta", 0, fields); !errors.Is(err, repository.ErrModuleDefinitionVersionConflict) {
		t.Fatalf("duplicate declaration: %v", err)
	}
	if _, err := repo.CreateCatalogDefinition(false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateCatalogDefinition(true); !errors.Is(err, repository.ErrModuleDefinitionVersionConflict) {
		t.Fatalf("duplicate administrator intent: %v", err)
	}

	nodes := repository.NewHostNodeRepository(db)
	node := models.HostNode{
		NodeID: "12345678-1234-1234-1234-123456789abc", DisplayName: "registry fixture", NodeKind: "virtual", Enabled: true,
		Addresses: []string{"192.0.2.1"}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-meta", ModuleName: "meta"}},
	}
	if err := nodes.Save(ctx, &node, 0, func(*gorm.DB, *models.HostNode, models.HostNode) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, count, err := nodes.List(ctx, 1, 10, models.HostNodeFilter{Search: "192.0.2.1"}); err != nil || count != 1 {
		t.Fatalf("search nodes: count=%d err=%v", count, err)
	}
	req := models.ModuleRegistrationRequest{
		ModuleName: "meta", InstanceID: "meta-1", Role: "backend", ModuleURL: "http://meta.example:8080", ProcessStartedAt: time.Now().UTC().Truncate(time.Microsecond),
		NodeID: node.NodeID, RegistrationClientID: "addp-meta", HostNodeIPs: node.Addresses,
		ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "http://meta.example:8080/metrics"},
	}
	if changed, err := repo.Register(&req, time.Minute); err != nil || !changed {
		t.Fatalf("register: changed=%v err=%v", changed, err)
	}
	if changed, err := repo.Register(&req, time.Minute); err != nil || changed {
		t.Fatalf("renew: changed=%v err=%v", changed, err)
	}
	rows, count, err := repo.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{NodeIP: "192.0.2.1", Page: 1, PageSize: 10}, time.Now())
	if err != nil || count != 1 || len(rows) != 1 || rows[0].NodeBindingState != "bound" || rows[0].InstanceID != req.InstanceID {
		t.Fatalf("node projection: count=%d rows=%v err=%v", count, rows, err)
	}
	for _, stmt := range []string{"SET declared_node_id=''", "SET process_metrics=NULL", "SET role='worker'"} {
		if err := db.Exec("UPDATE module_runtime_instances " + stmt + " WHERE instance_id='meta-1'").Error; err == nil {
			t.Fatalf("accepted immutable change: %s", stmt)
		}
	}
	live, err := repo.RoutingSnapshot(ctx, nil, "meta")
	if err != nil || len(live.Endpoints) != 1 || live.Endpoints[0].URL != req.ModuleURL {
		t.Fatalf("lease routing: %+v %v", live, err)
	}
	endpoints := []commonmodels.ModuleRoutingEndpoint{{ModuleName: "meta", EndpointID: "external", URL: "http://external.example", ExpiresAt: req.ProcessStartedAt.Add(time.Minute)}}
	bindings := map[string]string{"meta": "bound"}
	var disconnected func([]commonmodels.ModuleRoutingEndpoint) error
	err = repo.WithDiscoveryWriter(ctx, "meta", "bound", func(publish func([]commonmodels.ModuleRoutingEndpoint) error) error {
		if err := repo.WithDiscoveryWriter(ctx, "meta", "bound", func(func([]commonmodels.ModuleRoutingEndpoint) error) error {
			return errors.New("simultaneous writer")
		}); err != nil {
			return err
		}
		for range 2 {
			if err := publish(endpoints); err != nil {
				return err
			}
		}
		// Target just this writer's named lock in the gate-owned MySQL instance.
		var id uint64
		if err := db.Raw("SELECT IS_USED_LOCK(SHA2(CONCAT(DATABASE(), '/system.module_discovery/meta'),256))").Row().Scan(&id); err != nil {
			return err
		}
		if err := db.Exec(fmt.Sprintf("KILL CONNECTION %d", id)).Error; err != nil {
			return err
		}
		disconnected = publish
		if publish(endpoints) == nil {
			return errors.New("disconnected writer published")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := repo.RoutingSnapshot(ctx, bindings, "meta")
	if err != nil || found.Revision != live.Revision+1 || len(found.Endpoints) != 1 || found.Endpoints[0].EndpointID != "external" {
		t.Fatalf("published routing: %+v %v", found, err)
	}
	err = repo.WithDiscoveryWriter(ctx, "meta", "bound", func(publish func([]commonmodels.ModuleRoutingEndpoint) error) error {
		if err := publish([]commonmodels.ModuleRoutingEndpoint{}); err != nil {
			return err
		}
		if disconnected(endpoints) == nil {
			return errors.New("previous writer restored endpoints after takeover")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := repo.RoutingSnapshot(ctx, bindings, "meta")
	if err != nil || empty.Revision != found.Revision+1 || len(empty.Endpoints) != 0 {
		t.Fatalf("withdrawn routing: %+v %v", empty, err)
	}
}
