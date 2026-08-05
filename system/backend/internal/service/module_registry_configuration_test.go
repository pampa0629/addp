package service

import (
	"reflect"
	"strings"
	"testing"

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
	if err := db.AutoMigrate(&models.ModuleRegistry{}); err != nil {
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
	if err := db.Model(&models.ModuleRegistry{}).Where("module_name = ?", "quality").Update("status", "down").Error; err != nil {
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

func configurationRegistration(owner, scopeType string) models.ModuleRegistrationRequest {
	return models.ModuleRegistrationRequest{
		ModuleName:  owner,
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
