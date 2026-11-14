package service

import (
	"context"
	"errors"
	"testing"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAlertRuleTargetsOnlyIncludeActiveTaskProviderTypes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:monitor-alert-rule-targets?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
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
			(tenant_id, execution_id, module, task_type, source, source_task_id, source_task_name, status, trigger_type, created_at, updated_at)
			VALUES (7, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, row.executionID, commonExecution.ModuleTransfer,
			row.taskType, commonExecution.ModuleTransfer, row.taskID, row.name,
			commonExecution.ExecutionStatusSuccess, commonExecution.TriggerTypeManual).Error; err != nil {
			t.Fatal(err)
		}
	}
	capabilities := commonModels.JSONString(monitorTaskCapabilitiesForTest(
		monitorTaskCapabilityForTest(commonExecution.TaskTypeSync, false),
	))
	service := NewAlertRuleService(db, nil, fakeTaskProviderLister{providers: []*commonModels.TaskProvider{{
		ModuleName: commonExecution.ModuleTransfer, Enabled: true,
		TaskProviderDeclaration: commonModels.TaskProviderDeclaration{Capabilities: &capabilities},
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
		{
			ModuleName: commonExecution.ModuleDevelop, Enabled: true,
			TaskProviderDeclaration: commonModels.TaskProviderDeclaration{Capabilities: &activeCapabilities},
		},
		{
			ModuleName: commonExecution.ModuleMeta, Enabled: false,
			TaskProviderDeclaration: commonModels.TaskProviderDeclaration{Capabilities: &disabledCapabilities},
		},
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

func TestCDCRuleTargetsComeFromCurrentOwnerAndFilterTenantAndCapabilities(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:cdc-current-rule-targets?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`INSERT INTO common.task_executions (tenant_id,execution_id,module,task_type,source,source_task_id,source_task_name,status,trigger_type,created_at,updated_at) VALUES(7,'cdc-old-bounded','transfer','sync','transfer','71','old bounded','success','manual',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatal(err)
	}
	capabilities := commonModels.JSONString(monitorTaskCapabilitiesForTest(monitorTaskCapabilityForTest("sync", false)))
	alertService := NewAlertService(db, nil)
	service := NewAlertRuleService(db, alertService, fakeTaskProviderLister{providers: []*commonModels.TaskProvider{{ModuleName: "transfer", Enabled: true, TaskProviderDeclaration: commonModels.TaskProviderDeclaration{Capabilities: &capabilities}}}})
	page := &commonClient.TransferCDCRisks{Items: []commonClient.TransferCDCRiskItem{
		{TenantID: 7, TaskID: 71, TaskName: "current PG", Generation: 1, State: "observing", SourceType: "postgresql"},
		{TenantID: 7, TaskID: 72, TaskName: "current Oracle", Generation: 1, State: "observing", SourceType: "oracle"},
		{TenantID: 8, TaskID: 73, Generation: 1, State: "observing", SourceType: "postgresql"},
		{TenantID: 7, TaskID: 74, Generation: 1, State: "closed", SourceType: "postgresql"},
	}}
	reset := func() { alertService.SetCDCRiskReader(&riskPageReader{pages: []*commonClient.TransferCDCRisks{page}}) }
	reset()
	targets, err := service.ListTargets(context.Background(), 7)
	if err != nil || len(targets) != 2 || !targets[0].CDC || !targets[0].CaptureDelayAvailable || targets[1].CaptureDelayAvailable {
		t.Fatal("current source targets", targets, err)
	}
	reset()
	if err = service.validateTypedAlertRuleTarget(db.WithContext(context.Background()), 7, "transfer", "sync", "72", monitorModels.AlertRuleCDCDelayed); !errors.Is(err, ErrAlertRuleInvalid) {
		t.Fatal("unsupported capture delay admitted", err)
	}
	reset()
	if err = service.validateTypedAlertRuleTarget(db.WithContext(context.Background()), 7, "transfer", "sync", "72", monitorModels.AlertRuleCDCStalled); err != nil {
		t.Fatal("consumer rule refused", err)
	}
}
