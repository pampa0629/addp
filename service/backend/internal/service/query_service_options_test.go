package service

import (
	"encoding/json"
	"github.com/addp/common/datatype"
	q "github.com/addp/common/query"
	"github.com/addp/service/internal/models"
	"testing"
)

func TestNamedParameterOptionsAreEnforcedAndFingerprintFrozen(t *testing.T) {
	options := []q.ParameterOption{{Value: "total", Labels: map[string]string{"zh-cn": "全期", "en": "Total"}}, {Value: "month", Labels: map[string]string{"zh-cn": "按月", "en": "Monthly"}}}
	service := consumerDescriptorTestService()
	service.SqlQuery = "SELECT :grain"
	service.NamedParameters = []models.QueryServiceNamedParameter{{Name: "grain", Type: datatype.FieldTypeString, Required: true, Options: options}}
	if _, _, resolved, err := bindQueryServiceNamedParameters(service, "postgresql", service.SqlQuery, map[string]interface{}{"grain": "month"}); err != nil || resolved["grain"] != "month" {
		t.Fatalf("valid value failed: %v", err)
	}
	for _, bad := range []any{"week", "按月", true, 1, nil} {
		if _, _, _, err := bindQueryServiceNamedParameters(service, "postgresql", service.SqlQuery, map[string]interface{}{"grain": bad}); err == nil {
			t.Fatalf("bad value accepted: %v", bad)
		}
	}
	before, err := BuildQueryConsumerDescriptor(service)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.InputContract.NamedParameters[0].Options) != 2 {
		t.Fatal("options missing from descriptor")
	}
	raw, _ := json.Marshal(before.InputContract.NamedParameters[0].Options)
	if len(raw) == 0 {
		t.Fatal("empty metadata")
	}
	version := QueryServiceVersion(service)
	options[0].Labels["en"] = "Entire period"
	if QueryServiceVersion(service) == version {
		t.Fatal("parameter options did not change publication version")
	}
	after, err := BuildQueryConsumerDescriptor(service)
	if err != nil {
		t.Fatal(err)
	}
	if before.ContractFingerprint == after.ContractFingerprint {
		t.Fatal("labels not fingerprinted")
	}
	service.NamedParameters[0].Required = false
	service.NamedParameters[0].Default = "week"
	if _, err := validateQueryServiceNamedParameters("sql", service.SqlQuery, service.NamedParameters); err == nil {
		t.Fatal("invalid default accepted")
	}
}
