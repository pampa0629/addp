package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func TestObjectExecutionFreezesExactMetadataWithoutChangingDefinition(t *testing.T) {
	for _, kind := range []string{"exact", "different Meta path", "different Meta item id", "wrong format", "multi layout", "scheduled", "enabled", "parent execution"} {
		t.Run(kind, func(t *testing.T) {
			config := commonmodels.JSONMap{"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"},
				"source": map[string]interface{}{"locator": "addp://engine/4/path/demo/tables/orders.parquet?type=object&item_id=21", "data_type": "table", "representation": "encoded", "format": "parquet", "options": map[string]interface{}{}},
				"target": map[string]interface{}{"parent_locator": "addp://engine/5/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native"}}
			item := commonmodels.MetaItem{ID: 21, EngineID: 4, ItemType: "object", FullName: "demo/tables/orders.parquet", Fingerprint: commonmodels.GenerateItemFingerprint(4, "demo/tables/orders.parquet"), Attributes: map[string]interface{}{"item": map[string]interface{}{"layout": "single", "data_type": "table", "format": "parquet"}}}
			task := &models.TransferTask{TenantID: 7, Config: config}
			record := &execution.TaskExecution{TenantID: 7, Source: "transfer", TriggerType: "manual", ExecutionBoundary: "bounded", ExecutionConfig: config}
			switch kind {
			case "different Meta path":
				item.FullName = "demo/tables/other.parquet"
			case "different Meta item id":
				item.ID = 22
			case "wrong format":
				item.Attributes["item"].(map[string]interface{})["format"] = "csv"
			case "multi layout":
				item.Attributes["item"].(map[string]interface{})["layout"] = "multi"
			case "scheduled":
				task.Schedule = "* * * * *"
			case "enabled":
				task.Enabled = true
			case "parent execution":
				parent := "parent"
				record.ParentExecutionID = &parent
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/meta/items/21" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(item)
			}))
			defer server.Close()
			service := &TaskService{engineResolver: planner.StaticEngineResolver{4: {Type: "rustfs", EngineID: 4, EngineVersion: 8}, 5: {Type: "postgresql", EngineID: 5, EngineVersion: 1}}, metaClient: newExecutionTestMetaClient(server.URL), objectAuthorization: client.NewSystemExecutionAuthorizationClient("http://unused.invalid", nil)}
			prepared, err := service.prepareObjectExecution(t.Context(), task, record)
			if kind != "exact" {
				if prepared || err == nil {
					t.Fatal("accepted unsafe source preparation", kind, err)
				}
				return
			}
			if err != nil || !prepared || record.MaxAttempts != 1 {
				t.Fatal("exact source did not prepare", err)
			}
			if task.Config["object_source"] != nil {
				t.Fatal("execution scope leaked into saved task")
			}
			raw, _ := json.Marshal(record.ExecutionConfig)
			scope, err := execution.NewTransferObjectReadScope(raw)
			if err != nil || scope.ObjectSource.EngineVersion != "8" || scope.TargetEngineID != 5 {
				t.Fatal("incorrect frozen source", err)
			}
		})
	}
}
