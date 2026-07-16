package service

import (
	"context"
	"errors"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAlertRuleTargetsOnlyIncludeActiveTaskProviderTypes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:monitor-alert-rule-targets?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE common.task_executions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, execution_id TEXT NOT NULL,
		module TEXT NOT NULL, task_type TEXT NOT NULL, source TEXT NOT NULL, source_task_id TEXT,
		source_task_name TEXT, parent_execution_id TEXT, status TEXT NOT NULL, trigger_type TEXT NOT NULL,
		metadata JSON, created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		executionID string
		taskType    string
		taskID      string
		name        string
	}{
		{executionID: "sync-43", taskType: commonExecution.TaskTypeSync, taskID: "43", name: "current sync"},
		{executionID: "legacy-17", taskType: "transfer", taskID: "17", name: "legacy transfer"},
	} {
		if err := db.Exec(`INSERT INTO common.task_executions
			(tenant_id, execution_id, module, task_type, source, source_task_id, source_task_name, status, trigger_type)
			VALUES (7, ?, ?, ?, ?, ?, ?, ?, ?)`, row.executionID, commonExecution.ModuleTransfer,
			row.taskType, commonExecution.ModuleTransfer, row.taskID, row.name,
			commonExecution.ExecutionStatusSuccess, commonExecution.TriggerTypeManual).Error; err != nil {
			t.Fatal(err)
		}
	}
	capabilities := commonModels.JSONString(monitorTaskCapabilitiesForTest(
		monitorTaskCapabilityForTest(commonExecution.TaskTypeSync, false),
	))
	service := NewAlertRuleService(db, nil, fakeTaskProviderLister{providers: []*commonModels.TaskProvider{{
		ModuleName: commonExecution.ModuleTransfer, Capabilities: &capabilities, IsEnabled: true,
	}}})

	targets, err := service.ListTargets(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].TaskType != commonExecution.TaskTypeSync || targets[0].SourceTaskID != "43" {
		t.Fatalf("targets = %#v, want only transfer/sync/43", targets)
	}
	if err := service.validateAlertRuleTarget(db, 7, commonExecution.ModuleTransfer, "transfer", "17"); !errors.Is(err, ErrAlertRuleInvalid) {
		t.Fatalf("legacy target validation error = %v, want ErrAlertRuleInvalid", err)
	}
	if err := service.validateAlertRuleTarget(db, 7, commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, "43"); err != nil {
		t.Fatalf("current target validation: %v", err)
	}
}

func TestActiveTaskTypesExcludeDisabledAndDeprecatedCapabilities(t *testing.T) {
	activeCapabilities := commonModels.JSONString(monitorTaskCapabilitiesForTest(
		monitorTaskCapabilityForTest(commonExecution.TaskTypeWorkflow, false),
		monitorTaskCapabilityForTest(commonExecution.TaskTypeScript, true),
	))
	disabledCapabilities := commonModels.JSONString(monitorTaskCapabilitiesForTest(
		monitorTaskCapabilityForTest(commonExecution.TaskTypeScan, false),
	))
	service := NewAlertRuleService(nil, nil, fakeTaskProviderLister{providers: []*commonModels.TaskProvider{
		{ModuleName: commonExecution.ModuleDevelop, Capabilities: &activeCapabilities, IsEnabled: true},
		{ModuleName: commonExecution.ModuleMeta, Capabilities: &disabledCapabilities, IsEnabled: false},
	}})

	taskTypes, err := service.activeTaskTypes()
	if err != nil {
		t.Fatalf("activeTaskTypes: %v", err)
	}
	if !taskTypes.Contains(commonExecution.ModuleDevelop, commonExecution.TaskTypeWorkflow) {
		t.Fatal("active workflow capability is missing")
	}
	if taskTypes.Contains(commonExecution.ModuleDevelop, commonExecution.TaskTypeScript) {
		t.Fatal("deprecated script capability was included")
	}
	if taskTypes.Contains(commonExecution.ModuleMeta, commonExecution.TaskTypeScan) {
		t.Fatal("disabled provider capability was included")
	}
}
