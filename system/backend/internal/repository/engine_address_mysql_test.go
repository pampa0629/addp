package repository_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/bootstrap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestEngineAddressObservationAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_ENGINE_OBSERVATION") != "1" {
		t.Skip("requires a disposable MySQL System installation")
	}
	db, err := bootstrap.OpenPlatformRuntime(config.LoadPlatformDatabase(), "system", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	t.Run("DeploymentDeclarations", func(t *testing.T) { checkDeploymentDeclarations(t, tx) })
	t.Run("RuntimeMetricsLease", func(t *testing.T) { checkRuntimeMetricsLease(t, tx) })
	repo := repository.NewEngineRepository(tx)
	engine := &models.Engine{Name: t.Name(), EngineType: "postgresql", ConnectionInfo: models.ConnectionInfo{"host": "probe.source", "port": 5432, "database": "orders"}}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	found, err := repo.FindByIdentityKey(engine.EngineType, engine.TenantID, current.IdentityKey)
	if err != nil || found.ID != engine.ID {
		t.Fatalf("find current address: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if applied, err := repo.UpdateConnectionObservation(engine.ID, models.EngineConnectionOnline, now, "reachable", &current.IdentityKey, "15"); err != nil || !applied {
		t.Fatalf("current address observation: applied=%t err=%v", applied, err)
	}
	stale := models.JSONString(`{"host":"former.source"}`)
	if _, err := repo.FindByIdentityKey(engine.EngineType, engine.TenantID, stale); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale address lookup: %v", err)
	}
	if applied, err := repo.UpdateConnectionObservation(engine.ID, models.EngineConnectionOffline, now.Add(time.Second), "stale", &stale, ""); err != nil || applied {
		t.Fatalf("stale address observation: applied=%t err=%v", applied, err)
	}
	observed, err := repo.GetByID(engine.ID)
	if err != nil || observed.ConnectionStatus != models.EngineConnectionOnline || observed.Version != current.Version {
		t.Fatalf("online observation and aggregate version: %v", err)
	}
}

func checkRuntimeMetricsLease(t *testing.T, tx *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	if err := tx.Exec(`INSERT INTO engines(name,engine_type,engine_origin,connection_info,identity_key,is_builtin)
      VALUES ('runtime-metrics-fixture','geopython_workflow','extension','{}','{"name":"runtime-metrics-fixture"}',true)`).Error; err != nil {
		t.Fatal(err)
	}
	var id uint
	if err := tx.Raw("SELECT id FROM engines WHERE name='runtime-metrics-fixture'").Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	svc := service.NewEngineService(repository.NewEngineRepository(tx), nil, nil)
	first := commonmodels.RuntimeMetricsDeclaration{InstanceID: uuid.NewString(), StartedAt: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond), Endpoint: "https://127.0.0.1:19444/metrics"}
	declare := func(claim commonmodels.RuntimeMetricsDeclaration, want error) {
		t.Helper()
		if err := svc.DeclareRuntimeMetrics(ctx, id, "geopython", claim); !errors.Is(err, want) {
			t.Fatalf("declare generation: got %v want %v", err, want)
		}
	}
	declare(first, nil)
	declare(first, nil)
	next := first
	next.InstanceID = uuid.NewString()
	declare(next, service.ErrRuntimeMetricsConflict)
	next.StartedAt = first.StartedAt.Add(time.Second)
	declare(next, nil)
	declare(first, service.ErrRuntimeMetricsConflict)
	var row models.RuntimeMetricsLease
	if err := tx.Where("engine_id=?", id).First(&row).Error; err != nil || row.InstanceID != next.InstanceID || row.Endpoint != next.Endpoint || !row.StartedAt.Equal(next.StartedAt) || !row.LeaseExpiresAt.After(time.Now()) {
		t.Fatalf("current runtime lease: %+v, %v", row, err)
	}
	registry := repository.NewModuleRegistryRepository(tx)
	snapshot, err := registry.ObservabilityIdentities(ctx, nil)
	if err != nil || len(snapshot.RuntimeMetrics) != 1 || snapshot.RuntimeMetrics[0].InstanceID != next.InstanceID {
		t.Fatalf("current runtime discovery: %+v, %v", snapshot, err)
	}
	if err := tx.Exec("UPDATE engines SET lifecycle_state='disabled' WHERE id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	declare(next, service.ErrRuntimeMetricsConflict)
	snapshot, err = registry.ObservabilityIdentities(ctx, nil)
	if err != nil || len(snapshot.RuntimeMetrics) != 0 {
		t.Fatalf("disabled runtime discovery: %+v, %v", snapshot, err)
	}

}

func checkDeploymentDeclarations(t *testing.T, tx *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	node := uuid.NewString()
	if err := tx.Exec(`INSERT INTO host_nodes(node_id,display_name,node_kind,addresses,enabled,allowed_module_bindings,version,created_at,updated_at) VALUES (?,'metrics node','physical','[]',true,'[]',1,NOW(6),NOW(6))`, node).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewEngineDeploymentRepository(tx)
	decl := models.EngineDeployment{DeploymentID: uuid.NewString(), DisplayName: "MySQL infra", DeclaredNodeID: node, Enabled: true, EngineType: "mysql", Inventory: models.EngineDeploymentInventory{
		Schema: "addp.engine-deployment-inventory/v1", Source: models.EngineDeploymentSource{Type: "docker_compose", Runtime: "linux_docker", DockerEngineID: "test-engine", Project: "addp-infra"},
		Services: []models.EngineDeploymentService{{Service: "mysql", EngineType: "mysql"}},
	}}
	audit := func(*gorm.DB, *models.EngineDeployment, models.EngineDeployment) error { return nil }
	if err := repo.Save(ctx, &decl, 0, audit); err != nil {
		t.Fatal(err)
	}
	key := [3]string{"test-engine", "addp-infra", "mysql"}
	registered, err := repo.RegisteredSources(ctx, [][3]string{key})
	if err != nil || registered[key] != decl.DeploymentID {
		t.Fatalf("registered sources: %v, %v", registered, err)
	}
	list, total, err := repo.List(ctx, 1, 10, models.EngineDeploymentFilter{Scope: "infra", EngineType: "mysql"})
	if err != nil || total != 1 || len(list) != 1 || list[0].DeploymentID != decl.DeploymentID || list[0].HostBindingState != "bound" {
		t.Fatalf("infra deployments: %v, %d, %v", list, total, err)
	}
	other := decl
	other.DeploymentID = uuid.NewString()
	if err := repo.Save(ctx, &other, 0, audit); !errors.Is(err, repository.ErrDeploymentSourceOccupied) {
		t.Fatalf("duplicate source: %v", err)
	}
	decl.DisplayName = "renamed"
	if err := repo.Save(ctx, &decl, 1, audit); err != nil {
		t.Fatal(err)
	}
	var versions, active int
	if err := tx.Raw("SELECT COUNT(*),COUNT(IF(valid_to IS NULL,1,NULL)) FROM engine_deployment_declarations WHERE deployment_id=?", decl.DeploymentID).Row().Scan(&versions, &active); err != nil || versions != 2 || active != 1 {
		t.Fatalf("declaration versions=%d active=%d: %v", versions, active, err)
	}
	if err := tx.Exec("DELETE FROM engine_deployment_declarations WHERE deployment_id=?", decl.DeploymentID).Error; err == nil {
		t.Fatal("deployment history deleted")
	}
}
