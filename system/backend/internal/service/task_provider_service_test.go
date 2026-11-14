package service

import (
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func taskProviderModuleRegistryForTest(t *testing.T) (*gorm.DB, *ModuleRegistryService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}, &models.ModuleRegistryState{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModuleRegistryState{ID: 1, Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	return db, NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
}

func taskProviderModuleRequestForTest() *models.ModuleRegistrationRequest {
	return &models.ModuleRegistrationRequest{ProcessStartedAt: time.Now(),
		ModuleName: "meta", InstanceID: "meta-a", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://meta-a:8082",
	}
}

func providerFields() models.ModuleDeclarationFields {
	capabilities := commonmodels.JSONString(`{"schema_version":"task.capabilities/v2","task_capabilities":[{"type":"scan","display_name":"Scan","description":"Scan metadata","definition_schema":{"type":"object"},"supports_schedule":false,"supports_cancel":false,"supports_inline_execution":false,"create_url":"/meta/scan","edit_url":"/meta/scan?id=:id","deprecated":false}]}`)
	prefix := "/meta"
	return models.ModuleDeclarationFields{RoutePrefix: &prefix, TaskProvider: &commonmodels.TaskProviderDeclaration{
		DisplayName: "Meta", Description: "Metadata tasks",
		TaskListEndpoint: "/api/v1/meta/task-provider/tasks", TaskDetailEndpoint: "/api/v1/meta/task-provider/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/meta/task-provider/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/api/v1/meta/task-provider/executions/{execution_id}",
		Capabilities: &capabilities,
	}}
}

func TestTaskProviderProjectionKeepsDeclarationAndResolvesCurrentBackend(t *testing.T) {
	db, modules := taskProviderModuleRegistryForTest(t)
	if _, err := modules.PublishDeclaration("meta", 0, providerFields()); err != nil {
		t.Fatal(err)
	}
	providers := NewTaskProviderService(modules)
	request := taskProviderModuleRequestForTest()
	if err := modules.Register(request); err != nil {
		t.Fatal(err)
	}
	provider, err := providers.GetByModuleName("meta")
	if err != nil {
		t.Fatal(err)
	}
	if !provider.Available || len(provider.Backends) != 1 || provider.Backends[0].BaseURL != request.ModuleURL ||
		provider.Backends[0].InstanceID != request.InstanceID || provider.ID == 0 || provider.ModuleVersion != 1 {
		t.Fatalf("provider = %#v", provider)
	}

	if err := db.Model(&models.ModuleRuntimeInstance{}).Where("instance_id = ?", request.InstanceID).
		Updates(map[string]interface{}{"status": models.ModuleRuntimeStatusDown, "lease_expires_at": time.Now().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	provider, err = providers.GetByModuleName("meta")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Available || len(provider.Backends) != 0 || provider.UnavailableReason != "no_valid_backend" || provider.Capabilities == nil {
		t.Fatalf("offline provider = %#v", provider)
	}
}

func TestTaskProviderProjectionReturnsAllCurrentBackendsInStableOrder(t *testing.T) {
	_, modules := taskProviderModuleRegistryForTest(t)
	if _, err := modules.PublishDeclaration("meta", 0, providerFields()); err != nil {
		t.Fatal(err)
	}
	providers := NewTaskProviderService(modules)
	first := taskProviderModuleRequestForTest()
	first.InstanceID = "meta-b"
	first.ModuleURL = "http://meta-b:8082"
	if err := modules.Register(first); err != nil {
		t.Fatal(err)
	}
	second := taskProviderModuleRequestForTest()
	second.InstanceID = "meta-a"
	second.ModuleURL = "http://meta-a:8082/"
	if err := modules.Register(second); err != nil {
		t.Fatal(err)
	}

	provider, err := providers.GetByModuleName("meta")
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Backends) != 2 || provider.Backends[0].InstanceID != "meta-a" || provider.Backends[0].BaseURL != "http://meta-a:8082" ||
		provider.Backends[1].InstanceID != "meta-b" {
		t.Fatalf("backends = %#v", provider.Backends)
	}
}

func TestReconnectingInstancesCannotRepublishOrWithdrawCapabilities(t *testing.T) {
	_, registry := taskProviderModuleRegistryForTest(t)
	fields := providerFields()
	if _, err := registry.PublishDeclaration("meta", 0, fields); err != nil {
		t.Fatal(err)
	}
	request := taskProviderModuleRequestForTest()
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	fields.TaskProvider.Description = "Updated tasks"
	published, err := registry.PublishDeclaration("meta", 1, fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(request); err != nil {
		t.Fatal(err)
	}
	worker := *request
	worker.Role = "worker"
	worker.InstanceID = "worker"
	if err := registry.Register(&worker); err != nil {
		t.Fatal(err)
	}
	current, err := registry.GetModule("meta")
	if err != nil || current.Version != published.Version || current.TaskProvider.Description != "Updated tasks" {
		t.Fatalf("reconnected: %+v %v", current, err)
	}
	fields.TaskProvider = nil
	if _, err := registry.PublishDeclaration("meta", published.Version, fields); err != nil {
		t.Fatal(err)
	}
	current, err = registry.GetModule("meta")
	if err != nil || current.Version != 3 || current.TaskProvider != nil {
		t.Fatalf("withdrawn: %+v %v", current, err)
	}
}
