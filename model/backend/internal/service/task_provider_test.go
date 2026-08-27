package service

import (
	"context"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func TestModelTaskProviderDeclaration(t *testing.T) {
	declaration, err := ModelTaskProviderDeclaration()
	if err != nil {
		t.Fatal(err)
	}
	if err := taskprovider.ValidateDeclaration(declaration); err != nil {
		t.Fatalf("invalid declaration: %v", err)
	}
	capabilities, err := taskprovider.ParseCapabilities(string(*declaration.Capabilities))
	if err != nil {
		t.Fatal(err)
	}
	for _, taskType := range []string{commonExecution.TaskTypeMaterializationPrepare, commonExecution.TaskTypeMaterializationPublish, commonExecution.TaskTypeMaterializationGroupPublish} {
		if capabilities.CapabilityFor(taskType) == nil {
			t.Fatalf("missing capability %q", taskType)
		}
	}
}

func TestTaskProviderTreatsEmptyPartitionFieldsAsUnpartitioned(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	table := models.LogicalTable{
		TenantID: 1, Name: "Orders", Code: "orders", TableType: "entity", Layer: "dwd",
		Status: "approved", Version: 1, CreatedBy: 1,
		Materialization: models.JSONB{
			"target_parent_locator": "addp://engine/2/path/public?type=schema",
			"target_name":           "orders", "partition_by": "", "partition_type": "range",
		},
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	if err := db.Create(&models.LogicalField{
		TableID: table.ID, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: true,
	}).Error; err != nil {
		t.Fatalf("create logical field: %v", err)
	}
	service := &MaterializationService{logicalTableRepo: repository.NewLogicalTableRepository(db)}
	definitions, total, err := service.ListTaskDefinitions(context.Background(), 1, commonExecution.TaskTypeMaterializationPrepare, 1, 20)
	if err != nil {
		t.Fatalf("list task definitions: %v", err)
	}
	if total != 1 || len(definitions) != 1 || definitions[0].ID != table.ID {
		t.Fatalf("definitions = %#v, total = %d", definitions, total)
	}
}

func TestMaterializationPartitionDetectionRequiresNonEmptyField(t *testing.T) {
	if materializationHasPartitioning(models.JSONB{"partition_by": "", "partition_type": "range"}) {
		t.Fatal("empty partition field must be treated as unpartitioned")
	}
	if !materializationHasPartitioning(models.JSONB{"partition_by": "occurred_at", "partition_type": "range"}) {
		t.Fatal("non-empty partition field must be treated as partitioned")
	}
}

func TestMaterializationMarkerBindsLogicalTableAndFingerprint(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	marker := materializationMarker(7, fingerprint, "batch-1")
	if !materializationMarkerMatchesOwner(marker, 7, fingerprint) {
		t.Fatal("marker does not match its owner")
	}
	if materializationMarkerMatchesOwner(marker, 8, fingerprint) {
		t.Fatal("marker matched a different logical table")
	}
	if materializationMarkerMatchesOwner(marker, 7, strings.Repeat("b", 64)) {
		t.Fatal("marker matched a different schema fingerprint")
	}
}

func TestMaterializationExecutionContractsHaveNoRequiredInputs(t *testing.T) {
	for _, taskType := range []string{commonExecution.TaskTypeMaterializationPrepare, commonExecution.TaskTypeMaterializationPublish} {
		contract := materializationExecutionContract(taskType)
		raw := map[string]interface{}{
			"input_schema": contract.InputSchema, "input_defaults": contract.InputDefaults,
			"input_ui_schema": contract.InputUISchema, "output_schema": contract.OutputSchema,
		}
		if err := taskprovider.ValidateExecutionContract(raw); err != nil {
			t.Fatalf("%s contract is invalid: %v", taskType, err)
		}
	}
}

func TestMaterializationGroupExecutionContractCarriesCurrentExpectationDefaults(t *testing.T) {
	contract := materializationGroupExecutionContract(models.MaterializationGroup{ID: 12, Version: 4})
	raw := map[string]interface{}{
		"input_schema": contract.InputSchema, "input_defaults": contract.InputDefaults,
		"input_ui_schema": contract.InputUISchema, "output_schema": contract.OutputSchema,
	}
	if err := taskprovider.ValidateExecutionContract(raw); err != nil {
		t.Fatal(err)
	}
	if contract.InputDefaults["expected_group_id"] != int64(12) || contract.InputDefaults["expected_group_version"] != int64(4) {
		t.Fatalf("group defaults = %#v", contract.InputDefaults)
	}
}
