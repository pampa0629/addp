package repository_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestHostNodeIdentityVersionAndBindingAgainstPostgres(t *testing.T) {
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
	if err = runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err = runner.Run(ctx); err != nil {
		t.Fatal("migration idempotence", err)
	}

	var grants int64
	if err := db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'platform.host_node.%'`).Scan(&grants).Error; err != nil || grants != 3 {
		t.Fatalf("node grants=%d err=%v", grants, err)
	}
	var unauthorized int64
	if err := db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'platform.host_node.%' AND r.role_key <> 'platform.system_administrator'`).Scan(&unauthorized).Error; err != nil || unauthorized != 0 {
		t.Fatalf("excess grants=%d err=%v", unauthorized, err)
	}
	nodes := service.NewHostNodeService(repository.NewHostNodeRepository(db))
	enabled := true
	input := models.HostNodeInput{DisplayName: "node", NodeKind: "virtual", Enabled: &enabled, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{{ClientID: "addp-manager", ModuleName: "manager"}}}
	node, err := nodes.Create(ctx, input, iam.AuditMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	var auditCount int64
	if err := db.Model(&iam.AuditLog{}).Where("event_name = ? AND entity_id = ?", "host_node.created", node.NodeID).Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("audit=%d err=%v", auditCount, err)
	}
	// Two updates racing on one version: exactly one commits; the other reports a conflict.
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := nodes.Update(ctx, node.NodeID, models.HostNodeUpdateRequest{HostNodeInput: input, Version: 1}, iam.AuditMetadata{})
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, commonapi.ErrConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("updates success=%d conflict=%d", succeeded, conflicted)
	}
	registry := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	request := &models.ModuleRegistrationRequest{ModuleName: "manager", InstanceID: "host-node-test", Role: "worker", RoutePrefix: "/manager", NodeID: node.NodeID, RegistrationClientID: "addp-manager", ProcessStartedAt: time.Now().UTC()}
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	verify := func(state, reason, id string) {
		t.Helper()
		module, err := registry.GetModule("manager")
		if err != nil || len(module.Instances) != 1 {
			t.Fatalf("module=%+v err=%v", module, err)
		}
		instance := module.Instances[0]
		if instance.NodeBindingState != state || instance.NodeBindingReason != reason || instance.NodeID != id || instance.DeclaredNodeID != node.NodeID || instance.Status != "up" {
			t.Fatalf("instance=%+v", instance)
		}
	}
	verify("bound", "", node.NodeID)
	request.NodeID = uuid.NewString()
	request.RegistrationClientID = "addp-meta"
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	verify("bound", "", node.NodeID)
	result := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", request.InstanceID).Update("declared_node_id", uuid.NewString())
	if result.Error == nil {
		t.Fatal("database permitted immutable declaration overwrite")
	}
	enabled = false
	if _, err := nodes.Update(ctx, node.NodeID, models.HostNodeUpdateRequest{HostNodeInput: input, Version: 2}, iam.AuditMetadata{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.SendHeartbeat("manager", request.InstanceID); err != nil {
		t.Fatal(err)
	}
	verify("rejected", "node_disabled", "")
	enabled = true
	input.AllowedModuleBindings = []models.HostNodeModuleBinding{}
	if _, err := nodes.Update(ctx, node.NodeID, models.HostNodeUpdateRequest{HostNodeInput: input, Version: 3}, iam.AuditMetadata{}); err != nil {
		t.Fatal(err)
	}
	verify("rejected", "source_not_allowed", "")
	// A database-level audit failure must roll back the aggregate version and intent.
	if err := db.Exec(`CREATE FUNCTION system.fail_host_node_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.entity_type = 'host_node' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fail_host_node_audit BEFORE INSERT ON system.audit_logs FOR EACH ROW EXECUTE FUNCTION system.fail_host_node_audit()`).Error; err != nil {
		t.Fatal(err)
	}
	enabled = false
	if _, err := nodes.Update(ctx, node.NodeID, models.HostNodeUpdateRequest{HostNodeInput: input, Version: 4}, iam.AuditMetadata{}); err == nil {
		t.Fatal("audit failure accepted")
	}
	current, err := nodes.Get(ctx, node.NodeID)
	if err != nil || current.Version != 4 || !current.Enabled {
		t.Fatalf("audit failure changed node: %+v %v", current, err)
	}

}
