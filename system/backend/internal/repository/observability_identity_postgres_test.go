package repository_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestObservabilityIdentityProjectionAgainstPostgres(t *testing.T) {
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
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runner := migration.NewRunner(dsn)
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal("migration idempotence", err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.observability_identity.read' AND r.role_key='platform.monitor_runtime' AND r.tenant_id IS NULL`).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("monitor grant=%d %v", count, err)
	}
	if err := db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.observability_identity.read' AND (r.role_key<>'platform.monitor_runtime' OR r.tenant_id IS NOT NULL)`).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("excess grant=%d %v", count, err)
	}
	node := models.HostNode{NodeID: uuid.NewString(), DisplayName: "node", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-manager", ModuleName: "manager"}}}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	registry := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	if err := registry.Register(&models.ModuleRegistrationRequest{ModuleName: "manager", InstanceID: "worker-one", Role: "worker", RoutePrefix: "/manager", NodeID: node.NodeID, RegistrationClientID: "addp-manager", ProcessStartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var before models.ModuleRuntimeInstance
	if err := db.Where("instance_id = ?", "worker-one").First(&before).Error; err != nil {
		t.Fatal(err)
	}
	// The writer changes node intent after the first SELECT. Remaining reads must
	// still observe the same pre-change snapshot, never a mixed authorization view.
	var mutated atomic.Bool
	var inspected atomic.Bool
	var isolation, readOnly string
	var mutationError error
	callbackName := "test:observability_identity_snapshot"
	if err := db.Callback().Row().Before("gorm:row").Register(callbackName+":settings", func(tx *gorm.DB) {
		if tx.Statement.Table != "host_nodes" || !inspected.CompareAndSwap(false, true) {
			return
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SHOW transaction_isolation").Scan(&isolation).Error; err != nil {
			mutationError = err
			return
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SHOW transaction_read_only").Scan(&readOnly).Error; err != nil {
			mutationError = err
			return
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "host_nodes" || !mutated.CompareAndSwap(false, true) {
			return
		}
		mutationError = db.Model(&models.HostNode{}).Where("node_id = ?", node.NodeID).Updates(map[string]any{"enabled": false, "version": 2}).Error
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.ObservabilityIdentities(ctx)
	if err := db.Callback().Row().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Remove(callbackName + ":settings"); err != nil {
		t.Fatal(err)
	}
	if err != nil || mutationError != nil || !mutated.Load() || isolation != "repeatable read" || readOnly != "on" || len(snapshot.Nodes) != 1 || len(snapshot.ModuleInstances) != 1 || snapshot.Nodes[0].Version != 1 {
		t.Fatalf("snapshot=%+v err=%v writer=%v mutated=%v isolation=%q readOnly=%q", snapshot, err, mutationError, mutated.Load(), isolation, readOnly)
	}
	current, err := registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.Nodes) != 0 || len(current.ModuleInstances) != 0 {
		t.Fatalf("post-update=%+v err=%v", current, err)
	}
	if err := registry.SendHeartbeat("manager", "worker-one"); err != nil {
		t.Fatal("observation changed business heartbeat", err)
	}
	if err := db.Model(&node).Updates(map[string]any{"enabled": true, "version": 3}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", "worker-one").Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.Nodes) != 1 || len(current.ModuleInstances) != 0 {
		t.Fatalf("unscanned expired=%+v %v", current, err)
	}
	var after models.ModuleRuntimeInstance
	if err := db.Where("instance_id = ?", "worker-one").First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != "up" || after.ID != before.ID || after.DeclaredNodeID != before.DeclaredNodeID {
		t.Fatalf("projection mutated persisted instance %+v", after)
	}
	// A native process has no invented VM binding. Re-registration uses the
	// same nanosecond start identity even though PostgreSQL stores microseconds.
	process := &models.ModuleRegistrationRequest{ModuleName: "monitor", InstanceID: "native-process", Role: "worker", RoutePrefix: "/monitor", ProcessStartedAt: time.Now().UTC(), RegistrationClientID: "addp-monitor",
		ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:18100/metrics"}}
	for i := 0; i < 2; i++ {
		if err := registry.Register(process); err != nil {
			t.Fatal("same real process re-registration", err)
		}
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.ModuleInstances) != 1 || current.ModuleInstances[0].NodeID != "" || current.ModuleInstances[0].ProcessMetrics == nil || current.ModuleInstances[0].ProcessMetrics.Endpoint != process.ProcessMetrics.Endpoint {
		t.Fatalf("private process projection: %+v %v", current, err)
	}
	rows, total, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{ModuleName: "monitor", Page: 1, PageSize: 100})
	if err != nil || total != 1 || len(rows) != 1 || !rows[0].ProcessMetricsDeclared {
		t.Fatalf("public process declaration: %+v %d %v", rows, total, err)
	}
	exact, total, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{IDs: []uint{rows[0].ID}, Page: 1, PageSize: 100})
	if err != nil || total != 1 || len(exact) != 1 || exact[0].InstanceID != process.InstanceID {
		t.Fatalf("exact authorized process reference: %+v %d %v", exact, total, err)
	}
	for _, update := range []map[string]any{
		{"process_metrics": nil},
		{"process_metrics": `{"schema_version":"addp.process-metrics/v1","endpoint":"https://127.0.0.1:18101/metrics"}`},
		{"role": "scheduler"},
		{"process_started_at": time.Now().UTC().Add(time.Hour)},
	} {
		if err := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", "native-process").Updates(update).Error; err == nil {
			t.Fatal("database allowed immutable process identity mutation")
		}
	}
	if err := db.Model(&models.HostNode{}).Where("node_id = ?", node.NodeID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.Nodes) != 0 || len(current.ModuleInstances) != 1 {
		t.Fatalf("host intent disabled independent process: %+v %v", current, err)
	}
	if err := registry.Deregister("monitor", "native-process"); err != nil {
		t.Fatal(err)
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.ModuleInstances) != 0 {
		t.Fatalf("offline process remains discoverable: %+v %v", current, err)
	}
	// A database error returns no snapshot even if the node SELECT already succeeded.
	if err := db.Exec("ALTER TABLE system.module_runtime_instances RENAME TO unavailable_instances").Error; err != nil {
		t.Fatal(err)
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err == nil || current != nil || !strings.Contains(err.Error(), "module_runtime_instances") || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("database failure=%+v %v", current, err)
	}
}
