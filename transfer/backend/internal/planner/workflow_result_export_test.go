package planner

import (
	"encoding/json"
	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/exportartifact"
	"github.com/google/uuid"
	"testing"
	"time"
)

func workflowExportConfig(t *testing.T) map[string]interface{} {
	t.Helper()
	parent := exportartifact.InfraPrefixLocator("manager", "tenant_7/export/develop/20261114/"+uuid.NewString())
	var config map[string]interface{}
	raw, _ := json.Marshal(client.TransferExecutionConfig{WorkflowResult: &client.WorkflowResultExportSource{ExecutionID: uuid.NewString(), Attempt: 1, ExpiresAt: time.Now().Add(time.Minute), SHA256: client.WorkflowResultRowsDigest([]byte("[]")), Columns: []string{"surface"}, RowCount: 0, Targets: []dataprotection.ResourceReference{{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "source"}}}, Runtime: client.TransferExecutionRuntime{Boundary: "bounded"}, Load: client.TransferExecutionLoad{Mode: "snapshot"}, Source: client.TransferExecutionEndpoint{Locator: parent[:len(parent)-len("?type=prefix")] + "/workflow-result.json?type=object", DataType: "table", Representation: "encoded", Format: "json"}, Target: client.TransferExecutionEndpoint{ParentLocator: parent, Name: "areas.csv", DataType: "table", Representation: "encoded", Format: "csv", Policy: map[string]interface{}{"apply_mode": "replace"}}, Transforms: []client.TransferExecutionTransform{{Type: "field_mapping", Mode: "project", Fields: []client.TransferExecutionFieldMapping{{Source: "surface", Target: "surface", TargetType: "unknown", Nullable: true}}}}})
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	return config
}
func TestWorkflowResultExportIsExactPrivateCSVSession(t *testing.T) {
	for _, kind := range []string{"valid", "other_prefix", "tenant", "query", "format", "changed_columns", "missing_deadline"} {
		t.Run(kind, func(t *testing.T) {
			config := workflowExportConfig(t)
			spec, err := ParseTableExportTaskSpec(config, 1000)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "other_prefix":
				spec.Source.Locator = exportartifact.InfraObjectLocator("manager", "tenant_7/elsewhere/workflow-result.json")
			case "tenant":
				spec.Target.ParentLocator = exportartifact.InfraPrefixLocator("manager", "tenant_8/export/develop/20261114/"+uuid.NewString())
			case "query":
				spec.Source.Query = &QuerySourceSpec{Language: "sql", Statement: "select 1"}
			case "format":
				spec.Target.Format = "json"
			case "changed_columns":
				spec.Transforms[0].Fields[0].Target = "other"
			case "missing_deadline":
				spec.WorkflowResult.ExpiresAt = time.Time{}
			}
			if err := ValidateWorkflowResultExport(spec, 7); (err == nil) != (kind == "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
