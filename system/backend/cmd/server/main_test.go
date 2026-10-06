package main

import (
	"context"
	"os"
	"testing"
	"time"

	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSystemRegistrationDeclarationIsValid(t *testing.T) {
	t.Setenv("ADDP_HOST_NODE_NAME", "system-host")
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	request := newSystemRegistrationRequest("http://localhost:8180", "system-instance")
	if request.ModuleName != "system" || request.Role != "backend" || request.ModuleURL != "http://localhost:8180" {
		t.Fatalf("unexpected System registration endpoint: %+v", request)
	}
	if err := commonconfiguration.ValidateManagementDeclaration(request.ModuleName, request.ConfigurationManagement); err != nil {
		t.Fatalf("System configuration management declaration is invalid: %v", err)
	}
	if request.HostNodeName != "system-host" || request.RuntimeHostname != hostname {
		t.Fatalf("System node identity = %#v", request)
	}
	entries := request.ConfigurationManagement.Entries
	if len(entries) != 2 {
		t.Fatalf("unexpected System configuration management route: %+v", entries)
	}
	byID := make(map[string]commonconfiguration.ManagementEntry)
	for _, entry := range entries {
		if entry.FrontendRoute != "/system/configuration" {
			t.Fatalf("configuration domains must share the System page: %+v", entry)
		}
		byID[entry.ID] = entry
	}
	security := byID["system.iam_security_policy"]
	raster := byID["system.engine_raster_policy"]
	if security.ReadPermission != "iam.security_policy.read" || len(security.ScopeTypes) != 1 || security.ScopeTypes[0] != commonconfiguration.ScopePlatformOnly {
		t.Fatalf("security policy declaration changed: %+v", security)
	}
	if raster.ReadPermission != "system.engine_raster_policy.read" || raster.UpdatePermission != "system.engine_raster_policy.update" || len(raster.ScopeTypes) != 1 || raster.ScopeTypes[0] != commonconfiguration.ScopePlatformDefaultWithTenantOverride {
		t.Fatalf("raster policy scope/permissions changed: %+v", raster)
	}
}

func TestSystemRegistrationDeregistersBeforeLifecycleCompletes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	registry := service.NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := newSystemRegistrationRequest("http://localhost:8180", "system-shutdown")
	if err := db.Exec(`CREATE TABLE permissions (permission_key text PRIMARY KEY, owner_module text NOT NULL, status text NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, entry := range request.ConfigurationManagement.Entries {
		for _, permission := range []string{entry.ReadPermission, entry.UpdatePermission} {
			if err := db.Exec(`INSERT INTO permissions (permission_key, owner_module, status) VALUES (?, ?, 'active')`, permission, request.ModuleName).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	done, err := startSystemRegistration(ctx, registry, request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := registry.GetModule("system")
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Instances) != 1 || before.Instances[0].Status != models.ModuleRuntimeStatusUp {
		t.Fatalf("registered instance = %#v", before)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("System registration did not finish on cancellation")
	}
	after, err := registry.GetModule("system")
	if err != nil {
		t.Fatal(err)
	}
	instance := after.Instances[0]
	if instance.Status != models.ModuleRuntimeStatusDown || instance.StopReason != models.ModuleRuntimeStopGraceful || instance.StoppedAt == nil {
		t.Fatalf("shutdown did not record graceful before completing: %#v", instance)
	}
	// A canceled startup cannot register a new instance or leave a heartbeat task.
	request.InstanceID = "system-canceled-start"
	if done, err := startSystemRegistration(ctx, registry, request); err != context.Canceled || done != nil {
		t.Fatalf("canceled startup = %v, %v", done, err)
	}
	after, err = registry.GetModule("system")
	if err != nil || len(after.Instances) != 1 {
		t.Fatalf("canceled startup changed instances: %#v, %v", after, err)
	}
	// A registration error also must not start a lifecycle task.
	request.Role = "invalid"
	if done, err := startSystemRegistration(context.Background(), registry, request); err == nil || done != nil {
		t.Fatalf("failed startup = %v, %v", done, err)
	}
}
