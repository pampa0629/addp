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
	if err := db.AutoMigrate(&models.ModuleDefinition{}, &models.ModuleRuntimeInstance{}); err != nil {
		t.Fatal(err)
	}
	return db, NewModuleRegistryService(repository.NewModuleRegistryRepository(db))
}

func taskProviderModuleRequestForTest() *models.ModuleRegistrationRequest {
	capabilities := commonmodels.JSONString(`{"schema_version":"task.capabilities/v2","task_capabilities":[{"type":"scan","display_name":"Scan","description":"Scan metadata","definition_schema":{"type":"object"},"supports_schedule":false,"supports_cancel":false,"supports_inline_execution":false,"create_url":"/meta/scan","edit_url":"/meta/scan?id=:id","deprecated":false}]}`)
	return &models.ModuleRegistrationRequest{
		ModuleName: "meta", InstanceID: "meta-a", Role: models.ModuleRuntimeRoleBackend,
		ModuleURL: "http://meta-a:8082", RoutePrefix: "/meta",
		TaskProvider: &commonmodels.TaskProviderDeclaration{
			DisplayName: "Meta", Description: "Metadata tasks",
			TaskListEndpoint: "/api/v1/meta/tasks", TaskDetailEndpoint: "/api/v1/meta/tasks/{task_type}/{id}",
			TaskExecuteEndpoint: "/api/v1/meta/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/api/v1/meta/executions/{execution_id}",
			Capabilities: &capabilities,
		},
	}
}

func TestTaskProviderProjectionKeepsDeclarationAndResolvesCurrentBackend(t *testing.T) {
	db, modules := taskProviderModuleRegistryForTest(t)
	providers := NewTaskProviderService(modules)
	request := taskProviderModuleRequestForTest()
	if err := modules.Register(request); err != nil {
		t.Fatal(err)
	}
	provider, err := providers.GetByModuleName("meta")
	if err != nil {
		t.Fatal(err)
	}
	if !provider.Available || provider.BaseURL != request.ModuleURL || provider.BackendInstanceID != request.InstanceID || provider.ID == 0 || provider.ModuleVersion != 1 {
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
	if provider.Available || provider.BaseURL != "" || provider.BackendInstanceID != "" || provider.Capabilities == nil {
		t.Fatalf("offline provider = %#v", provider)
	}
}

func TestTaskProviderDeclarationIsPartOfModuleVersion(t *testing.T) {
	_, modules := taskProviderModuleRegistryForTest(t)
	request := taskProviderModuleRequestForTest()
	if err := modules.Register(request); err != nil {
		t.Fatal(err)
	}
	if err := modules.Register(request); err != nil {
		t.Fatal(err)
	}
	module, _ := modules.GetModule("meta")
	if module.Version != 1 {
		t.Fatalf("idempotent declaration version = %d, want 1", module.Version)
	}
	request.TaskProvider.Description = "Updated metadata tasks"
	if err := modules.Register(request); err != nil {
		t.Fatal(err)
	}
	module, _ = modules.GetModule("meta")
	if module.Version != 2 || module.TaskProvider.Description != "Updated metadata tasks" {
		t.Fatalf("updated module = %#v", module)
	}
}

func TestBackendMayWithdrawTaskProviderButWorkerCannot(t *testing.T) {
	_, modules := taskProviderModuleRegistryForTest(t)
	request := taskProviderModuleRequestForTest()
	if err := modules.Register(request); err != nil {
		t.Fatal(err)
	}

	workerRequest := &models.ModuleRegistrationRequest{
		ModuleName: request.ModuleName, InstanceID: "meta-worker-a", Role: models.ModuleRuntimeRoleWorker,
		RoutePrefix: request.RoutePrefix,
	}
	if err := modules.Register(workerRequest); err != nil {
		t.Fatal(err)
	}
	module, err := modules.GetModule(request.ModuleName)
	if err != nil {
		t.Fatal(err)
	}
	if module.TaskProvider == nil || module.Version != 1 {
		t.Fatalf("worker registration changed declaration: %#v", module)
	}

	request.TaskProvider = nil
	if err := modules.Register(request); err != nil {
		t.Fatal(err)
	}
	module, err = modules.GetModule(request.ModuleName)
	if err != nil {
		t.Fatal(err)
	}
	if module.TaskProvider != nil || module.Version != 2 {
		t.Fatalf("backend withdrawal did not remove declaration: %#v", module)
	}
}
