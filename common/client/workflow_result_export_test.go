package client

import (
	"encoding/json"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestWorkflowResultExportRequiresCompleteScalarBoundedTable(t *testing.T) {
	table := &plugin.WorkflowTableResult{SchemaVersion: "addp.workflow.table-result/v1", Columns: []string{"instance_id", "surface"}, Rows: []map[string]interface{}{{"instance_id": json.Number("1"), "surface": json.Number("819.0184107347761")}}, RowCount: 1}
	data, err := WorkflowResultExportRows(table)
	if err != nil {
		t.Fatal(err)
	}
	binding := &WorkflowResultExportSource{ExecutionID: uuid.NewString(), Attempt: 1, ExpiresAt: time.Now().Add(time.Minute), SHA256: WorkflowResultRowsDigest(data), Columns: table.Columns, RowCount: 1, Targets: []dataprotection.ResourceReference{{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "source"}}}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"digest", "column", "row_count", "target", "attempt"} {
		next := *binding
		switch bad {
		case "digest":
			next.SHA256 = "bad"
		case "column":
			next.Columns = []string{"x", "x"}
		case "row_count":
			next.RowCount = 1001
		case "target":
			next.Targets = nil
		case "attempt":
			next.Attempt = 0
		}
		if next.Validate() == nil {
			t.Fatal("accepted", bad)
		}
	}
	table.RowCount = 2
	if _, err := WorkflowResultExportRows(table); err == nil {
		t.Fatal("accepted truncated table")
	}
	table.RowCount = 1
	table.Rows[0]["surface"] = map[string]interface{}{"geometry": "private"}
	if _, err := WorkflowResultExportRows(table); err == nil {
		t.Fatal("accepted nested output")
	}
}
