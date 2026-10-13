package execution_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
)

func TestFailureDiagnosticSurvivesPersistenceWithoutRuntimeText(t *testing.T) {
	item := &execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{
		"code": "develop.query.snapshot_invalid", "message": "SELECT secret-business-value password=credential-sentinel",
		"diagnostic": execution.NewFailureDiagnostic("validation", "query_template_invalid"),
	}}
	payload, err := json.Marshal(item.ErrorDetails)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &item.ErrorDetails); err != nil {
		t.Fatal(err)
	}
	observation := execution.Observe(item)
	projected, err := json.Marshal(observation.ErrorDetails)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projected), `"reason":"query_template_invalid"`) || !strings.Contains(string(projected), `"code":"develop.query.snapshot_invalid"`) {
		t.Fatalf("missing controlled failure facts: %s", projected)
	}
	for _, forbidden := range []string{"SELECT", "secret-business-value", "credential-sentinel", "message"} {
		if strings.Contains(string(projected), forbidden) {
			t.Fatalf("unsafe diagnostic: %s", projected)
		}
	}
	item.Status = "success"
	if len(execution.Observe(item).ErrorDetails) != 0 {
		t.Fatal("success retained a failure diagnostic")
	}
}

func TestFailureDiagnosticRejectsUnknownContractAndFreeText(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":"addp.execution-failure/v2","stage":"validation","reason":"query_template_invalid"}`,
		`{"schema_version":"addp.execution-failure/v1","stage":"SELECT secret","reason":"query_template_invalid"}`,
		`{"schema_version":"addp.execution-failure/v1","stage":"validation","reason":"password=secret"}`,
		`{"schema_version":"addp.execution-failure/v1","stage":"validation","reason":"query_template_invalid","message":"business-secret"}`,
		`{"schema_version":"addp.execution-failure/v1","stage":"validation"}`,
		`"arbitrary runtime body"`, `null`,
	} {
		var value interface{}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		observed := execution.Observe(&execution.TaskExecution{Status: "failed", ErrorDetails: models.JSONMap{"diagnostic": value}})
		if observed.ErrorDetails["diagnostic"] != nil {
			t.Fatalf("accepted unsafe contract: %s", raw)
		}
	}
}
