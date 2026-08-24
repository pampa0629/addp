package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListConfigurationManagementEntriesFiltersContextPermissionAndModuleStatus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	registryService := NewModuleRegistryService(repository.NewModuleRegistryRepository(db))

	registrations := []models.ModuleRegistrationRequest{
		configurationRegistration("manager", commonconfiguration.ScopePlatformOnly),
		configurationRegistration("meta", commonconfiguration.ScopeTenantOnly),
		configurationRegistration("transfer", commonconfiguration.ScopePlatformDefaultWithTenantOverride),
		configurationRegistration("quality", commonconfiguration.ScopePlatformOnly),
	}
	for index := range registrations {
		if err := registryService.Register(&registrations[index]); err != nil {
			t.Fatalf("register %s: %v", registrations[index].ModuleName, err)
		}
	}
	var quality models.ModuleDefinition
	if err := db.Where("module_name = ?", "quality").First(&quality).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ModuleRuntimeInstance{}).Where("module_definition_id = ?", quality.ID).Update("status", models.ModuleRuntimeStatusDown).Error; err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		contextType   string
		permissions   []string
		wantIDs       []string
		wantAvailable []bool
	}{
		{
			name:          "platform sees platform and shared scopes",
			contextType:   "platform",
			permissions:   []string{"manager.configuration.read", "quality.configuration.read", "transfer.configuration.read"},
			wantIDs:       []string{"manager.configuration", "quality.configuration", "transfer.configuration"},
			wantAvailable: []bool{true, false, true},
		},
		{
			name:          "tenant cannot see platform only scope",
			contextType:   "tenant",
			permissions:   []string{"manager.configuration.read", "meta.configuration.read", "transfer.configuration.read"},
			wantIDs:       []string{"meta.configuration", "transfer.configuration"},
			wantAvailable: []bool{true, true},
		},
		{
			name:          "missing read permission hides entry",
			contextType:   "platform",
			permissions:   []string{},
			wantIDs:       []string{},
			wantAvailable: []bool{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			permissions := make(map[string]struct{}, len(test.permissions))
			for _, permission := range test.permissions {
				permissions[permission] = struct{}{}
			}
			entries, err := registryService.ListConfigurationManagementEntries(test.contextType, permissions)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, len(entries))
			available := make([]bool, len(entries))
			for index := range entries {
				ids[index] = entries[index].ID
				available[index] = entries[index].Available
			}
			if !reflect.DeepEqual(ids, test.wantIDs) {
				t.Fatalf("entry IDs = %v, want %v", ids, test.wantIDs)
			}
			if !reflect.DeepEqual(available, test.wantAvailable) {
				t.Fatalf("entry availability = %v, want %v", available, test.wantAvailable)
			}
		})
	}
}

func TestModuleRegistrySeparatesDefinitionFromRuntimeInstanceLease(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewModuleRegistryRepository(db)
	registry := NewModuleRegistryService(repo)
	for name, request := range map[string]*models.ModuleRegistrationRequest{
		"unknown role": {
			ModuleName: "manager", InstanceID: "invalid-role", Role: "api", RoutePrefix: "/manager",
		},
		"backend without URL": {
			ModuleName: "manager", InstanceID: "invalid-backend", Role: models.ModuleRuntimeRoleBackend, RoutePrefix: "/manager",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := registry.Register(request); !errors.Is(err, ErrInvalidModuleRegistration) {
				t.Fatalf("Register() error = %v, want invalid registration", err)
			}
		})
	}
	backend := &models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "backend-a", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://manager-a:8080", RoutePrefix: "/manager",
		ConfigurationManagement: &commonconfiguration.ManagementDeclaration{
			SchemaVersion: commonconfiguration.ManagementSchemaVersion,
			Entries: []commonconfiguration.ManagementEntry{{
				ID: "manager.configuration", OwnerModule: "manager",
				ScopeTypes:    []string{commonconfiguration.ScopePlatformOnly},
				FrontendRoute: "/manager/settings", ReadPermission: "manager.configuration.read",
				UpdatePermission: "manager.configuration.update",
			}},
		},
	}
	worker := &models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "worker-a", Role: models.ModuleRuntimeRoleWorker, RoutePrefix: "/manager",
	}
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(worker); err != nil {
		t.Fatal(err)
	}
	module, err := registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	if len(module.Instances) != 2 || module.ID == 0 {
		t.Fatalf("module = %#v", module)
	}
	if module.ConfigurationManagement == nil || len(module.ConfigurationManagement.Entries) != 1 {
		t.Fatalf("worker registration cleared module definition: %#v", module.ConfigurationManagement)
	}

	if err := db.Model(&models.ModuleDefinition{}).Where("id = ?", module.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	backend.ModuleURL = "http://manager-b:8080"
	if err := registry.Register(backend); err != nil {
		t.Fatal(err)
	}
	if err := registry.SendHeartbeat("manager", "backend-a"); err != nil {
		t.Fatal(err)
	}
	module, err = registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	if module.Enabled {
		t.Fatal("heartbeat overwrote administrator enabled=false")
	}
	active, err := registry.ListActiveModules()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("disabled module appeared active: %#v", active)
	}

	if _, err := repo.MarkStaleModules(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	module, err = registry.GetModule("manager")
	if err != nil {
		t.Fatalf("stale instances removed persistent definition: %v", err)
	}
	for _, instance := range module.Instances {
		if instance.Status != models.ModuleRuntimeStatusDown {
			t.Fatalf("instance remained up: %#v", instance)
		}
	}
	if err := registry.SendHeartbeat("manager", "missing-instance"); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("missing instance heartbeat error = %v", err)
	}
}

func TestModuleRegistrySeparatesBoundedCurrentProjectionFromPaginatedHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	registry := NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	register := func(instanceID, role, moduleURL string) {
		t.Helper()
		if err := registry.Register(&models.ModuleRegistrationRequest{
			ModuleName: "manager", InstanceID: instanceID, Role: role,
			ModuleURL: moduleURL, RoutePrefix: "/manager",
		}); err != nil {
			t.Fatalf("register %s: %v", instanceID, err)
		}
	}
	register("backend-old", models.ModuleRuntimeRoleBackend, "http://manager-old:8081")
	if err := registry.Deregister("manager", "backend-old"); err != nil {
		t.Fatal(err)
	}
	register("backend-current", models.ModuleRuntimeRoleBackend, "http://manager-current:8081")
	register("backend-current-second", models.ModuleRuntimeRoleBackend, "http://manager-current-second:8081")
	register("worker-old", models.ModuleRuntimeRoleWorker, "")
	if err := registry.Deregister("manager", "worker-old"); err != nil {
		t.Fatal(err)
	}
	register("worker-latest", models.ModuleRuntimeRoleWorker, "")
	if err := registry.Deregister("manager", "worker-latest"); err != nil {
		t.Fatal(err)
	}
	register("scheduler-current", models.ModuleRuntimeRoleScheduler, "")
	if err := registry.Register(&models.ModuleRegistrationRequest{
		ModuleName: "service", InstanceID: "service-old", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://service-old:8086", RoutePrefix: "/service",
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Deregister("service", "service-old"); err != nil {
		t.Fatal(err)
	}

	module, err := registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	if len(module.Instances) != 4 {
		t.Fatalf("current projection instances = %#v, want every effective-up instance and one fallback for an offline-only role", module.Instances)
	}
	projection := make(map[string]map[string]struct{}, len(module.Instances))
	for _, instance := range module.Instances {
		if projection[instance.Role] == nil {
			projection[instance.Role] = make(map[string]struct{})
		}
		projection[instance.Role][instance.InstanceID] = struct{}{}
	}
	if _, ok := projection[models.ModuleRuntimeRoleBackend]["backend-current"]; !ok {
		t.Fatalf("current projection = %#v", projection)
	}
	if _, ok := projection[models.ModuleRuntimeRoleBackend]["backend-current-second"]; !ok {
		t.Fatalf("current projection = %#v", projection)
	}
	if _, ok := projection[models.ModuleRuntimeRoleWorker]["worker-latest"]; !ok {
		t.Fatalf("current projection = %#v", projection)
	}
	if _, ok := projection[models.ModuleRuntimeRoleScheduler]["scheduler-current"]; !ok {
		t.Fatalf("current projection = %#v", projection)
	}

	page, total, err := registry.ListModuleRuntimeInstances("manager", models.ModuleRuntimeInstanceFilter{
		Page: 1, PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 6 || len(page) != 2 || page[0].InstanceID != "scheduler-current" || page[1].InstanceID != "worker-latest" {
		t.Fatalf("history page = %#v, total=%d", page, total)
	}

	up, upTotal, err := registry.ListModuleRuntimeInstances("manager", models.ModuleRuntimeInstanceFilter{
		Status: models.ModuleRuntimeStatusUp, Page: 1, PageSize: 10,
	})
	if err != nil || upTotal != 3 || len(up) != 3 {
		t.Fatalf("up history = %#v, total=%d, error=%v", up, upTotal, err)
	}
	down, downTotal, err := registry.ListModuleRuntimeInstances("manager", models.ModuleRuntimeInstanceFilter{
		Status: models.ModuleRuntimeStatusDown, Page: 1, PageSize: 10,
	})
	if err != nil || downTotal != 3 || len(down) != 3 || down[0].InstanceID != "worker-latest" {
		t.Fatalf("down history = %#v, total=%d, error=%v", down, downTotal, err)
	}
	workers, workerTotal, err := registry.ListModuleRuntimeInstances("manager", models.ModuleRuntimeInstanceFilter{
		Role: models.ModuleRuntimeRoleWorker, Page: 1, PageSize: 10,
	})
	if err != nil || workerTotal != 2 || len(workers) != 2 {
		t.Fatalf("worker history = %#v, total=%d, error=%v", workers, workerTotal, err)
	}
	if _, _, err := registry.ListModuleRuntimeInstances("manager", models.ModuleRuntimeInstanceFilter{
		Role: "api", Page: 1, PageSize: 10,
	}); !errors.Is(err, ErrInvalidModuleRuntimeInstanceQuery) {
		t.Fatalf("invalid role error = %v", err)
	}
}

func TestModuleRoutingRevisionChangesOnlyWithTopologyAndWatchReturnsFreshSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewModuleRegistryRepository(db)
	registry := NewModuleRegistryService(repo)
	registration := &models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "manager-a", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://manager-a:8081", RoutePrefix: "/manager",
	}

	if err := registry.Register(registration); err != nil {
		t.Fatal(err)
	}
	revision, err := repo.GetRegistryRevision()
	if err != nil || revision != 2 {
		t.Fatalf("revision after registration = %d, error=%v, want 2", revision, err)
	}
	if err := registry.Register(registration); err != nil {
		t.Fatal(err)
	}
	if err := registry.SendHeartbeat("manager", "manager-a"); err != nil {
		t.Fatal(err)
	}
	if revision, _ = repo.GetRegistryRevision(); revision != 2 {
		t.Fatalf("idempotent registration and heartbeat changed revision to %d", revision)
	}

	snapshot, err := registry.WatchRoutingSnapshot(context.Background(), revision, 20*time.Millisecond)
	if err != nil || snapshot.Revision != revision || len(snapshot.Modules) != 1 {
		t.Fatalf("timeout snapshot = %#v, error=%v", snapshot, err)
	}

	registration.ModuleURL = "http://manager-b:8081"
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = registry.Register(registration)
	}()
	snapshot, err = registry.WatchRoutingSnapshot(context.Background(), revision, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 3 || len(snapshot.Modules) != 1 || snapshot.Modules[0].Instances[0].ModuleURL != registration.ModuleURL {
		t.Fatalf("changed topology snapshot = %#v", snapshot)
	}

	if err := registry.Deregister("manager", "manager-a"); err != nil {
		t.Fatal(err)
	}
	if revision, _ = repo.GetRegistryRevision(); revision != 4 {
		t.Fatalf("revision after deregistration = %d, want 4", revision)
	}
}

func TestModuleDefinitionAdminUpdateUsesVersionAndKeepsRegistrationIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	registry := NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
	registration := configurationRegistration("manager", commonconfiguration.ScopePlatformOnly)
	if err := registry.Register(&registration); err != nil {
		t.Fatal(err)
	}
	module, err := registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	if module.Version != 1 || !module.Enabled {
		t.Fatalf("initial module = %#v", module)
	}

	disabled := false
	updated, err := registry.UpdateModuleDefinition("manager", &models.ModuleDefinitionUpdateRequest{
		Enabled: &disabled, Version: module.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || updated.Version != 2 {
		t.Fatalf("updated module = %#v", updated)
	}
	if _, err := registry.UpdateModuleDefinition("manager", &models.ModuleDefinitionUpdateRequest{
		Enabled: &disabled, Version: 1,
	}); !errors.Is(err, ErrModuleDefinitionVersionConflict) {
		t.Fatalf("stale update error = %v, want version conflict", err)
	}

	if err := registry.Register(&registration); err != nil {
		t.Fatal(err)
	}
	afterIdempotentRegistration, err := registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	if afterIdempotentRegistration.Version != 2 || afterIdempotentRegistration.Enabled {
		t.Fatalf("idempotent registration changed administrator state: %#v", afterIdempotentRegistration)
	}

	registration.RoutePrefix = "/manager-v2"
	if err := registry.Register(&registration); err != nil {
		t.Fatal(err)
	}
	afterDeclarationChange, err := registry.GetModule("manager")
	if err != nil {
		t.Fatal(err)
	}
	if afterDeclarationChange.Version != 3 || afterDeclarationChange.RoutePrefix != "/manager-v2" || afterDeclarationChange.Enabled {
		t.Fatalf("owner declaration update = %#v", afterDeclarationChange)
	}

	enabled := true
	if _, err := registry.UpdateModuleDefinition("missing", &models.ModuleDefinitionUpdateRequest{
		Enabled: &enabled, Version: 1,
	}); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("missing module update error = %v, want not found", err)
	}
}

func configurationRegistration(owner, scopeType string) models.ModuleRegistrationRequest {
	return models.ModuleRegistrationRequest{
		ModuleName:  owner,
		InstanceID:  owner + "-backend-test",
		Role:        models.ModuleRuntimeRoleBackend,
		ModuleURL:   "http://" + owner + ":8080",
		RoutePrefix: "/" + owner,
		ConfigurationManagement: &commonconfiguration.ManagementDeclaration{
			SchemaVersion: commonconfiguration.ManagementSchemaVersion,
			Entries: []commonconfiguration.ManagementEntry{{
				ID:               owner + ".configuration",
				OwnerModule:      owner,
				ScopeTypes:       []string{scopeType},
				FrontendRoute:    "/" + owner + "/settings/configuration",
				ReadPermission:   owner + ".configuration.read",
				UpdatePermission: owner + ".configuration.update",
			}},
		},
	}
}
