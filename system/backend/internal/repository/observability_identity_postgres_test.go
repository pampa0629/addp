package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
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
	// A valid identity can contain the private port digits without exposing a port.
	node := models.HostNode{NodeID: "54330000-0000-4000-8000-000000000001", DisplayName: "node", NodeKind: "virtual", Enabled: true, Version: 1, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-manager", ModuleName: "manager"}}}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	engines := service.NewEngineDeploymentService(repository.NewEngineDeploymentRepository(db))
	for _, project := range []string{"addp-infra", "business"} {
		enabled := true
		input := models.EngineDeploymentInput{DisplayName: "private-engine-name", Enabled: &enabled, DeclaredNodeID: node.NodeID,
			Inventory: models.EngineDeploymentInventory{Schema: "addp.engine-deployment-inventory/v1", ObservedAt: "2026-10-19T00:00:00Z",
				Source:   models.EngineDeploymentSource{Type: "docker_compose", Runtime: "desktop_linux_vm", DockerEngineID: "projection-daemon", Project: project},
				Services: []models.EngineDeploymentService{{Service: "postgres", EngineType: "postgresql", Containers: []models.EngineDeploymentContainer{{ContainerID: strings.Repeat("a", 64), StartedAt: "2026-10-18T00:00:00Z", PublishedPorts: []models.EngineDeploymentPort{{Address: "127.0.0.1", Port: 5433, ContainerPort: 5432, Protocol: "tcp"}}}}}}}}
		if _, err := engines.Create(ctx, input, iam.AuditMetadata{}); err != nil {
			t.Fatal(err)
		}
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
	if len(snapshot.EngineDeployments) != 2 {
		t.Fatalf("mixed engine snapshot: %+v", snapshot)
	}
	for _, engine := range snapshot.EngineDeployments {
		if engine.Validate() != nil || engine.Version != 1 || engine.NodeID != node.NodeID || engine.SourceKey.DockerEngineID != "projection-daemon" {
			t.Fatalf("invalid projection: %+v", engine)
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Compare the complete wire projection, including every nested field. Searching
	// the whole JSON for port digits confuses legitimate UUIDs with SQL endpoints.
	engineProjection := make([]map[string]any, 0, len(snapshot.EngineDeployments))
	for _, engine := range snapshot.EngineDeployments {
		project := "addp-infra"
		if engine.Scope == "business" {
			project = "business"
		}
		engineProjection = append(engineProjection, map[string]any{
			"deployment_id": engine.DeploymentID, "version": 1, "scope": engine.Scope,
			"engine_type": "postgresql", "node_id": node.NodeID,
			"metrics":    map[string]any{"state": "unavailable"},
			"source_key": map[string]any{"docker_engine_id": "projection-daemon", "project": project, "service": "postgres"},
		})
	}
	wantJSON, err := json.Marshal(map[string]any{
		"observed_at": snapshot.ObservedAt,
		"nodes":       []map[string]any{{"node_id": node.NodeID, "version": 1}},
		"module_instances": []map[string]any{{
			"module_name": "manager", "instance_id": "worker-one", "role": "worker", "node_id": node.NodeID,
			"process_started_at": before.ProcessStartedAt, "lease_expires_at": before.LeaseExpiresAt.UTC(),
		}},
		"engine_deployments": engineProjection,
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotWire, wantWire any
	if err := json.Unmarshal(encoded, &gotWire); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wantJSON, &wantWire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotWire, wantWire) {
		t.Fatalf("unexpected observability projection fields or values: got=%s want=%s", encoded, wantJSON)
	}
	current, err := registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.Nodes) != 0 || len(current.ModuleInstances) != 0 || len(current.EngineDeployments) != 0 {
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
		ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:18100/metrics", Cgroup: &commonmodels.CgroupIdentity{BootID: "11111111-1111-4111-8111-111111111111", CgroupID: "100", PIDNamespaceID: "200"}}}
	for i := 0; i < 2; i++ {
		if err := registry.Register(process); err != nil {
			t.Fatal("same real process re-registration", err)
		}
	}
	copy := *process
	copy.ProcessMetrics = process.ProcessMetrics.Clone()
	if err := registry.Register(&copy); err != nil {
		t.Fatal("value-equal cgroup declaration rejected", err)
	}
	copy.ProcessMetrics.Cgroup.CgroupID = "101"
	if err := registry.Register(&copy); !errors.Is(err, service.ErrInvalidModuleRegistration) {
		t.Fatal("mutable cgroup declaration accepted", err)
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.ModuleInstances) != 1 || current.ModuleInstances[0].NodeID != "" || current.ModuleInstances[0].ProcessMetrics == nil || current.ModuleInstances[0].ProcessMetrics.Endpoint != process.ProcessMetrics.Endpoint {
		t.Fatalf("private process projection: %+v %v", current, err)
	}
	if !current.ModuleInstances[0].ProcessMetrics.Equal(process.ProcessMetrics) {
		t.Fatal("private kernel identity lost in JSONB projection")
	}
	rows, total, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{ModuleName: "monitor", Page: 1, PageSize: 100})
	if err != nil || total != 1 || len(rows) != 1 || !rows[0].ProcessMetricsDeclared {
		t.Fatalf("public process declaration: %+v %d %v", rows, total, err)
	}
	for index, cgroup := range []string{`null`, `{}`, `{"kernel_boot_id":"11111111-1111-4111-8111-111111111111","cgroup_id":"01","pid_namespace_id":"200"}`, `{"kernel_boot_id":"11111111-1111-4111-8111-111111111111","cgroup_id":"18446744073709551616","pid_namespace_id":"200"}`, `{"kernel_boot_id":"11111111-1111-4111-8111-111111111111","cgroup_id":"100","pid_namespace_id":"200","unknown":"field"}`} {
		document := `{"schema_version":"addp.process-metrics/v1","endpoint":"https://localhost:9443/metrics","cgroup":` + cgroup + `}`
		name := fmt.Sprintf("invalid-cgroup-%d", index)
		err := db.Exec(`INSERT INTO system.module_runtime_instances (module_definition_id,instance_id,role,registration_client_id,status,last_heartbeat,lease_expires_at,process_started_at,process_metrics,host_node_ips,registered_at) SELECT module_definition_id,?,role,registration_client_id,status,last_heartbeat,lease_expires_at,process_started_at,?::jsonb,host_node_ips,registered_at FROM system.module_runtime_instances WHERE id=?`, name, document, rows[0].ID).Error
		if err == nil || !strings.Contains(err.Error(), "process_metrics_declaration_shape") {
			t.Fatal("invalid cgroup JSONB was not rejected by its shape constraint", index, err)
		}
	}
	exact, total, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{IDs: []uint{rows[0].ID}, Page: 1, PageSize: 100})
	if err != nil || total != 1 || len(exact) != 1 || exact[0].InstanceID != process.InstanceID {
		t.Fatalf("exact authorized process reference: %+v %d %v", exact, total, err)
	}
	// Newer undeclared instances must not bury the declared source after paging.
	for _, id := range []string{"unobserved-a", "unobserved-b", "unobserved-c"} {
		if err := registry.Register(&models.ModuleRegistrationRequest{ModuleName: "monitor", InstanceID: id, Role: "worker", RoutePrefix: "/monitor", ProcessStartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	declared, undeclared := true, false
	for _, tc := range []struct {
		filter *bool
		page   int
		total  int64
	}{{&declared, 1, 1}, {&declared, 2, 1}, {&undeclared, 1, 3}, {&undeclared, 3, 3}, {nil, 1, 4}} {
		page, count, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{ModuleName: "monitor", Role: "worker", Status: "up", ProcessMetricsDeclared: tc.filter, Page: tc.page, PageSize: 1})
		if err != nil || count != tc.total || (int64(tc.page) > tc.total && len(page) != 0) || (int64(tc.page) <= tc.total && len(page) != 1) {
			t.Fatalf("declaration count/page: %+v %d %v", page, count, err)
		}
		if tc.filter != nil && len(page) > 0 && page[0].ProcessMetricsDeclared != *tc.filter {
			t.Fatalf("declaration filter: %+v", page)
		}
	}
	intersection, count, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{IDs: []uint{rows[0].ID}, ProcessMetricsDeclared: &undeclared, Page: 1, PageSize: 1})
	if err != nil || count != 0 || len(intersection) != 0 {
		t.Fatalf("ID/declaration intersection: %+v %d %v", intersection, count, err)
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
	for _, status := range []string{"up", "down"} {
		page, count, err := registry.ListModuleRuntimeInstances(models.ModuleRuntimeInstanceFilter{ModuleName: "monitor", Status: status, ProcessMetricsDeclared: &declared, Page: 1, PageSize: 1})
		if err != nil || (status == "up" && (count != 0 || len(page) != 0)) || (status == "down" && (count != 1 || len(page) != 1 || page[0].InstanceID != process.InstanceID)) {
			t.Fatalf("declared offline scope: %+v %d %v", page, count, err)
		}
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.ModuleInstances) != 0 {
		t.Fatalf("offline process remains discoverable: %+v %v", current, err)
	}
	// An engine cannot outlive current host intent, and changing a Desktop host
	// to a physical node uses exactly the same rejection as the User API.
	for _, kind := range []string{"physical", "virtual"} {
		if err := db.Model(&node).Updates(map[string]any{"enabled": true, "node_kind": kind}).Error; err != nil {
			t.Fatal(err)
		}
		current, err = registry.ObservabilityIdentities(ctx)
		want := 0
		if kind == "virtual" {
			want = 2
		}
		if err != nil || len(current.EngineDeployments) != want {
			t.Fatalf("host kind %s: %+v %v", kind, current, err)
		}
	}
	engine := current.EngineDeployments[0]
	disabled := false
	retired, err := engines.Update(ctx, engine.DeploymentID, models.EngineDeploymentUpdateRequest{DisplayName: "retired", Enabled: &disabled, DeclaredNodeID: node.NodeID, Version: engine.Version}, iam.AuditMetadata{})
	if err != nil || retired.Version != 2 {
		t.Fatalf("retire: %+v %v", retired, err)
	}
	current, err = registry.ObservabilityIdentities(ctx)
	if err != nil || len(current.EngineDeployments) != 1 || current.EngineDeployments[0].DeploymentID == engine.DeploymentID {
		t.Fatalf("disabled engine projected: %+v %v", current, err)
	}
	// A failed engine read cannot return the nodes/modules already read.
	if err := db.Exec("ALTER TABLE system.engine_deployments RENAME TO unavailable_engines").Error; err != nil {
		t.Fatal(err)
	}
	if partial, err := registry.ObservabilityIdentities(ctx); err == nil || partial != nil {
		t.Fatalf("partial engine snapshot: %+v %v", partial, err)
	}
	if err := db.Exec("ALTER TABLE system.unavailable_engines RENAME TO engine_deployments").Error; err != nil {
		t.Fatal(err)
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
