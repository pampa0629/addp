package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/protection"
)

func TestObjectExecutionFreezesExactMetadataWithoutChangingDefinition(t *testing.T) {
	const source = "/api/v1/system/runtime/engine-descriptors/4"
	const target = "/api/v1/system/runtime/engine-descriptors/5"
	const meta = "/api/v1/meta/items/21"
	for _, tc := range []struct {
		name  string
		reads []string
	}{
		{"exact", []string{source, meta, target}},
		{"other object engine", []string{source}},
		{"source denied", []string{source}},
		{"source offline", []string{source}},
		{"invalid source version", []string{source}},
		{"different Meta path", []string{source, meta}},
		{"different Meta item id", []string{source, meta}},
		{"wrong format", []string{source, meta}},
		{"multi layout", []string{source, meta}},
		{"scheduled", []string{source}},
		{"enabled", []string{source}},
		{"parent execution", []string{source}},
		{"target denied", []string{source, meta, target}},
		{"target offline", []string{source, meta, target}},
		{"wrong target type", []string{source, meta, target}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := commonmodels.JSONMap{"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"},
				"source": map[string]interface{}{"locator": "addp://engine/4/path/demo/tables/orders.parquet?type=object&item_id=21", "data_type": "table", "representation": "encoded", "format": "parquet", "options": map[string]interface{}{}},
				"target": map[string]interface{}{"parent_locator": "addp://engine/5/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native"}}
			item := commonmodels.MetaItem{ID: 21, EngineID: 4, ItemType: "object", FullName: "demo/tables/orders.parquet", Fingerprint: commonmodels.GenerateItemFingerprint(4, "demo/tables/orders.parquet"), Attributes: map[string]interface{}{"item": map[string]interface{}{"layout": "single", "data_type": "table", "format": "parquet"}}}
			task := &models.TransferTask{TenantID: 7, Config: config}
			record := &execution.TaskExecution{TenantID: 7, Source: "transfer", TriggerType: "manual", ExecutionBoundary: "bounded", ExecutionConfig: config}
			src := commonmodels.EngineRuntimeDescriptor{ID: 4, EngineType: "rustfs", Version: 8, LifecycleState: "active", ConnectionStatus: "online"}
			dst := commonmodels.EngineRuntimeDescriptor{ID: 5, EngineType: "postgresql", Version: 23, LifecycleState: "active", ConnectionStatus: "online"}
			var deniedPath string
			switch tc.name {
			case "other object engine":
				src.EngineType = "s3"
			case "invalid source version":
				src.Version = 0
			case "source offline":
				src.ConnectionStatus = "offline"
			case "target offline":
				dst.ConnectionStatus = "offline"
			case "source denied":
				deniedPath = source
			case "target denied":
				deniedPath = target
			case "wrong target type":
				dst.EngineType = "mysql"
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
			var reads []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads = append(reads, r.URL.Path)
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tenant-7" {
					t.Error("expected GET with tenant 7 service identity")
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == deniedPath {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				switch r.URL.Path {
				case source:
					_ = json.NewEncoder(w).Encode(src)
				case target:
					_ = json.NewEncoder(w).Encode(dst)
				case meta:
					_ = json.NewEncoder(w).Encode(item)
				default:
					t.Errorf("unexpected preparation route %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			tokens := recommendationTokenSource{}
			service := &TaskService{
				engineResolver:      planner.NewSystemEngineResolver(client.NewSystemClient(server.URL, tokens)),
				engineDescriptor:    protection.EngineDescriptorLookup(client.NewSystemServiceClient(server.URL, tokens, server.Client())),
				metaClient:          client.NewMetaClient(server.URL, tokens),
				objectAuthorization: client.NewSystemExecutionAuthorizationClient(server.URL, server.Client()),
			}
			prepared, err := service.prepareObjectExecution(t.Context(), task, record)
			if !slices.Equal(reads, tc.reads) {
				t.Fatalf("requests = %v, want %v", reads, tc.reads)
			}
			if tc.name != "exact" {
				wantError := tc.name != "other object engine"
				if prepared || (err != nil) != wantError || record.ExecutionConfig["object_source"] != nil || record.MaxAttempts != 0 {
					t.Fatalf("unexpected preparation result: prepared=%t attempts=%d err=%v", prepared, record.MaxAttempts, err)
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

func TestStartObjectImportStopsBeforeAuthorizationOnDescriptorDenial(t *testing.T) {
	db := newTransferTaskServiceTestDB(t)
	config := validOverridableTargetTransferTaskConfig()
	config["source"] = map[string]interface{}{
		"locator":   "addp://engine/4/path/demo/tables/orders.parquet?type=object&item_id=21",
		"data_type": "table", "representation": "encoded", "format": "parquet",
	}
	task := &models.TransferTask{TenantID: 7, Name: "object descriptor refusal", TaskType: "sync", Status: models.TaskStatusIdle, Config: config, BatchSize: 100}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()
	svc := NewTaskService(db, nil, nil)
	tokens := recommendationTokenSource{}
	svc.SetEngineDescriptorLookup(protection.EngineDescriptorLookup(client.NewSystemServiceClient(server.URL, tokens, server.Client())))
	svc.SetObjectAuthorizationIssuer(client.NewSystemExecutionAuthorizationClient(server.URL, server.Client()))
	ctx := WithTaskAuthorizationToken(transferTestUserOrigin(), "addp_at_fixture")
	started, err := svc.StartTask(ctx, task.ID, 7, 9)
	if status, ok := client.SystemAPIStatusCode(err); !ok || status != http.StatusForbidden || started != nil {
		t.Fatalf("start = %v, error = %v, want descriptor refusal", started, err)
	}
	if !slices.Equal(paths, []string{"/api/v1/system/runtime/engine-descriptors/4"}) {
		t.Fatalf("unexpected start requests: %v", paths)
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Where("source_task_id = ?", task.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("executions after refusal = %d, error = %v", count, err)
	}
}
