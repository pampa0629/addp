package service

import (
	"encoding/json"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
)

func TestValidateMaterializationGateContractRejectsUnknownFieldsAndDuplicateBindings(t *testing.T) {
	document := json.RawMessage(`{"schema_version":"addp.quality.materialization-gate/v1","assertions":[{"assertion_key":"f3889a4a-1675-4623-b6e3-773f9125a04d","type":"not_null","severity":"error","params":{"table":"orders","column":"id","sql":"drop table x"}}]}`)
	_, err := validateMaterializationGateContract([]MaterializationGateTableBinding{{Alias: "orders", LogicalTableID: 3}}, document)
	if err == nil || !strings.Contains(err.Error(), "not_null params are invalid") {
		t.Fatalf("unknown params field error = %v", err)
	}

	valid := gateTestDocument(`{"table":"orders","column":"id"}`, "not_null")
	_, err = validateMaterializationGateContract([]MaterializationGateTableBinding{{Alias: "orders", LogicalTableID: 3}, {Alias: "orders_copy", LogicalTableID: 3}}, valid)
	if err == nil || !strings.Contains(err.Error(), "logical table binding is duplicated") {
		t.Fatalf("duplicate logical table error = %v", err)
	}
}

func TestValidateGateGroupRequiresExactVersionAndMemberSet(t *testing.T) {
	group := &commonClient.MaterializationGroup{ID: 9, Version: 4, Members: []commonClient.MaterializationGroupMember{{LogicalTableID: 3, Position: 0}, {LogicalTableID: 7, Position: 1}}}
	bindings := []MaterializationGateTableBinding{{Alias: "orders", LogicalTableID: 3}, {Alias: "customers", LogicalTableID: 7}}
	if err := validateGateGroup(group, bindings, 4); err != nil {
		t.Fatal(err)
	}
	if err := validateGateGroup(group, bindings, 3); err == nil {
		t.Fatal("version mismatch was accepted")
	}
	bindings[1].LogicalTableID = 8
	if err := validateGateGroup(group, bindings, 4); err == nil {
		t.Fatal("member mismatch was accepted")
	}
}

func TestCompileMaterializationGateUsesOnlyReadContextIdentifiers(t *testing.T) {
	config := &materializationGateExecutionConfig{
		TableBindings: []MaterializationGateTableBinding{{Alias: "orders", LogicalTableID: 3}, {Alias: "customers", LogicalTableID: 7}},
		Assertions: MaterializationGateAssertionDocument{Assertions: []MaterializationGateAssertion{
			{AssertionKey: "f3889a4a-1675-4623-b6e3-773f9125a04d", Type: "unique_key", Severity: "error", Params: json.RawMessage(`{"table":"orders","columns":["id"]}`)},
			{AssertionKey: "0cd81b20-8fe8-4fce-a77e-c4c385175d41", Type: "foreign_key", Severity: "error", Params: json.RawMessage(`{"table":"orders","columns":["customer_id"],"reference_table":"customers","reference_columns":["id"]}`)},
		}},
	}
	readContext := &commonClient.MaterializationReadContext{Items: []commonClient.MaterializationReadItem{
		{LogicalTableID: 3, BatchID: "batch-orders", EngineID: 9, StagingLocator: "addp://engine/9/path/materialized/orders_stage?type=table", Columns: []commonClient.MaterializationReadColumn{{Name: "id"}, {Name: "customer_id"}}},
		{LogicalTableID: 7, BatchID: "batch-customers", EngineID: 9, StagingLocator: "addp://engine/9/path/materialized/customers_stage?type=table", Columns: []commonClient.MaterializationReadColumn{{Name: "id"}}},
	}}
	compiled, _, batches, err := compileMaterializationGate(config, readContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 2 || !strings.Contains(compiled[0].SQL, `"materialized"."orders_stage"`) || !strings.Contains(compiled[1].SQL, `"materialized"."customers_stage"`) {
		t.Fatalf("compiled assertions = %#v", compiled)
	}
	if batches["orders"] != "batch-orders" {
		t.Fatalf("batch IDs = %#v", batches)
	}

	config.Assertions.Assertions[0].Params = json.RawMessage(`{"table":"orders","columns":["missing"]}`)
	if _, _, _, err := compileMaterializationGate(config, readContext); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("missing column error = %v", err)
	}
}

func TestGateRowCountBounds(t *testing.T) {
	exact := int64(4)
	if !gateRowCountPassed(4, gateRowCountParams{Exact: &exact}) || gateRowCountPassed(5, gateRowCountParams{Exact: &exact}) {
		t.Fatal("exact row count evaluation is invalid")
	}
	min, max := int64(2), int64(5)
	if !gateRowCountPassed(3, gateRowCountParams{Min: &min, Max: &max}) || gateRowCountPassed(6, gateRowCountParams{Min: &min, Max: &max}) {
		t.Fatal("range row count evaluation is invalid")
	}
}

func gateTestDocument(params, assertionType string) json.RawMessage {
	return json.RawMessage(`{"schema_version":"addp.quality.materialization-gate/v1","assertions":[{"assertion_key":"f3889a4a-1675-4623-b6e3-773f9125a04d","type":"` + assertionType + `","severity":"error","params":` + params + `}]}`)
}
