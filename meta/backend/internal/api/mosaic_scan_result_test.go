package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Called by the shared direct-HTTP SQLite and PostgreSQL authorization matrix.
func testManagerMosaicResultRead(t *testing.T, db *gorm.DB, read func(string, string, int) map[string]interface{}, parent, child execution.TaskExecution) {
	t.Helper()
	path := "/runtime/mosaic-scans/" + child.ExecutionID + "/result?parent_execution_id=" + parent.ExecutionID
	set := func(item *execution.TaskExecution) {
		t.Helper()
		if err := db.Save(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	parent.Metadata["meta_scan"] = commonmodels.JSONMap{"execution_id": child.ExecutionID}
	set(&parent)
	for _, credential := range []string{"owner", "peer", "oauth", "machine", "wrong-machine", "develop-machine", "manager-machine-denied"} {
		read(credential, path, http.StatusForbidden)
	}
	read("manager-machine-foreign", path, http.StatusNotFound)
	read("manager-machine", "/runtime/mosaic-scans/invalid/result?parent_execution_id="+parent.ExecutionID, http.StatusBadRequest)
	read("manager-machine", "/runtime/mosaic-scans/"+uuid.NewString()+"/result?parent_execution_id="+parent.ExecutionID, http.StatusNotFound)
	read("manager-machine", "/runtime/mosaic-scans/"+child.ExecutionID+"/result?parent_execution_id="+uuid.NewString(), http.StatusNotFound)
	for _, status := range []string{execution.ExecutionStatusPending, execution.ExecutionStatusRunning, execution.ExecutionStatusFailed, execution.ExecutionStatusTimeout, execution.ExecutionStatusCancelled} {
		child.Status = status
		child.Metadata["raw_runtime"] = "secret-runtime-and-token"
		child.ErrorDetails = commonmodels.JSONMap{"message": "secret-runtime-and-token"}
		set(&child)
		result := read("manager-machine", path, http.StatusOK)
		encoded, _ := json.Marshal(result)
		if len(result) != 3 || result["status"] != status || strings.Contains(string(encoded), "secret") {
			t.Fatalf("unsafe result: %s", encoded)
		}
	}
	child.Status = execution.ExecutionStatusSuccess
	set(&child)
	read("manager-machine", path, http.StatusNotFound) // success without a formal receipt is never fabricated
	child.Metadata["mosaic_output"] = commonclient.MetaMosaicOutput{Locator: "addp://engine/9/path/results/mosaic?type=object&item_id=77", ItemID: 77, Fingerprint: "whole-fingerprint"}
	set(&child)
	result := read("manager-machine", path, http.StatusOK)
	if len(result) != 4 || len(result["output"].(map[string]interface{})) != 3 {
		t.Fatalf("result is not closed: %+v", result)
	}
	for _, test := range []struct {
		name   string
		mutate func(*execution.TaskExecution)
	}{
		{"tenant", func(p *execution.TaskExecution) { p.TenantID++ }},
		{"module", func(p *execution.TaskExecution) { p.Module = execution.ModuleDevelop }},
		{"type", func(p *execution.TaskExecution) { p.TaskType = execution.TaskTypeRasterCOGGeneration }},
		{"terminal", func(p *execution.TaskExecution) { p.Status = execution.ExecutionStatusSuccess }},
		{"expired", func(p *execution.TaskExecution) { at := time.Now().Add(-time.Minute); p.LeaseExpiresAt = &at }},
		{"attempt", func(p *execution.TaskExecution) { p.Attempt++ }},
		{"scan_binding", func(p *execution.TaskExecution) {
			p.Metadata["meta_scan"] = commonmodels.JSONMap{"execution_id": uuid.NewString()}
		}},
		{"target", func(p *execution.TaskExecution) {
			p.Metadata["produced_target"] = commonclient.MetaScanProducedTarget{EngineID: 9, CatalogPath: "results/sibling", ManifestRef: "mosaic.addp.json", Attempt: 1}
		}},
	} {
		t.Run("mosaic_result_parent_"+test.name, func(t *testing.T) {
			changed := parent
			changed.Metadata = parent.Metadata.Clone()
			test.mutate(&changed)
			set(&changed)
			read("manager-machine", path, http.StatusNotFound)
			set(&parent)
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(*execution.TaskExecution)
	}{
		{"module", func(c *execution.TaskExecution) { c.Module = execution.ModuleManager }},
		{"type", func(c *execution.TaskExecution) { c.TaskType = execution.TaskTypeWorkflow }},
		{"source", func(c *execution.TaskExecution) { c.Source = execution.ModuleDevelop }},
		{"attempt_receipt", func(c *execution.TaskExecution) {
			c.Metadata["produced_target"] = commonclient.MetaScanProducedTarget{EngineID: 9, CatalogPath: "results/mosaic", ManifestRef: "mosaic.addp.json", Attempt: 2}
		}},
		{"manifest_is_not_whole", func(c *execution.TaskExecution) {
			c.Metadata["mosaic_output"] = commonclient.MetaMosaicOutput{Locator: "addp://engine/9/path/results/mosaic/mosaic.addp.json?type=object&item_id=77", ItemID: 77, Fingerprint: "whole-fingerprint"}
		}},
	} {
		t.Run("mosaic_result_child_"+test.name, func(t *testing.T) {
			changed := child
			changed.Metadata = child.Metadata.Clone()
			test.mutate(&changed)
			set(&changed)
			read("manager-machine", path, http.StatusNotFound)
			set(&child)
		})
	}
}
