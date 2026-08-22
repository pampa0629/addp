package service

import (
	"reflect"
	"strings"
	"testing"
	"time"

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
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}); err != nil {
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
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewModuleRegistryRepository(db)
	registry := NewModuleRegistryService(repo)
	backend := &models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "backend-a", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://manager-a:8080", RoutePrefix: "/manager",
	}
	worker := &models.ModuleRegistrationRequest{
		ModuleName: "manager", InstanceID: "worker-a", Role: "worker", RoutePrefix: "/manager",
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

	if err := db.Model(&models.ModuleDefinition{}).Where("id = ?", module.ID).Update("enabled", false).Error; err != nil {
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

	if err := repo.MarkStaleModules(time.Now().Add(time.Hour)); err != nil {
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
