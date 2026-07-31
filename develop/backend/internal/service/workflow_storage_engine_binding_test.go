package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/repository"
)

type workflowBindingTokenSource struct{}

func (workflowBindingTokenSource) Token(context.Context, uint) (string, error) {
	return "tenant-token", nil
}

func (workflowBindingTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-token", nil
}

func TestWorkflowStorageEngineBindingsListAndRebind(t *testing.T) {
	db := newNotebookBindingTestDB(t)
	updatedAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, dev_type, content, execution_config, editor_layout,
			timeout, tags, created_by, updated_at, status
		) VALUES (
			34, 7, 'spark_doris_customers', 'workflow',
			CAST('{"workflow_definition":{"tasks":[{"id":"load","operator":"load","params":{"locator":"addp://engine/5/path/addp_acceptance/customers?type=table&item_id=81"}},{"id":"write","operator":"write","params":{"target_parent_locator":"addp://engine/5/path/addp_acceptance?type=database&node_id=42","unrelated_locator":"addp://engine/2/path/public/source?type=table&item_id=99"}}]},"inputs":{}}' AS BLOB),
			CAST('{"engine_id":1,"engine_specific":{"spark_cluster_id":4}}' AS BLOB),
			CAST('{}' AS BLOB), 300, '{}', 1, ?, 'active'
		)
	`, updatedAt).Error; err != nil {
		t.Fatalf("seed workflow: %v", err)
	}

	systemServer := newWorkflowBindingSystemServer(t)
	defer systemServer.Close()
	systemClient := commonClient.NewSystemServiceClient(
		systemServer.URL,
		workflowBindingTokenSource{},
		systemServer.Client(),
	)
	svc := NewDevTaskService(repository.NewDevTaskRepository(db), systemClient)

	bindings, err := svc.ListWorkflowStorageEngineBindings(context.Background(), 34, 7)
	if err != nil {
		t.Fatalf("ListWorkflowStorageEngineBindings() error = %v", err)
	}
	if len(bindings.Items) != 2 {
		t.Fatalf("bindings = %#v, want 2 items", bindings.Items)
	}
	oldBinding := bindings.Items[1]
	if oldBinding.EngineID != 5 || oldBinding.Available || oldBinding.ReferenceCount != 2 {
		t.Fatalf("old binding = %#v", oldBinding)
	}
	if !reflect.DeepEqual(oldBinding.ResourceTypes, []string{"database", "table"}) {
		t.Fatalf("resource types = %#v", oldBinding.ResourceTypes)
	}
	if !reflect.DeepEqual(oldBinding.CompatibleEngineIDs, []uint{15}) {
		t.Fatalf("compatible engines = %#v, want [15]", oldBinding.CompatibleEngineIDs)
	}

	result, err := svc.RebindWorkflowStorageEngine(context.Background(), 34, 7, 3, 5, 15)
	if err != nil {
		t.Fatalf("RebindWorkflowStorageEngine() error = %v", err)
	}
	if result.ReplacedLocatorCount != 2 || result.Task.ID != 34 {
		t.Fatalf("rebind result = %#v", result)
	}
	if got := result.Task.ExecutionConfig["engine_id"]; got != float64(1) {
		t.Fatalf("workflow runtime engine changed: %#v", got)
	}
	engineSpecific, ok := result.Task.ExecutionConfig["engine_specific"].(map[string]interface{})
	if !ok || engineSpecific["spark_cluster_id"] != float64(4) {
		t.Fatalf("spark runtime changed: %#v", result.Task.ExecutionConfig)
	}

	locators := make([]*resourcetree.ResourceLocator, 0, 3)
	walkWorkflowStorageLocators(result.Task.Content, func(locator *resourcetree.ResourceLocator) {
		locators = append(locators, locator)
	})
	if len(locators) != 3 {
		t.Fatalf("locators = %#v", locators)
	}
	var reboundCount int
	for _, locator := range locators {
		if locator.EngineID == 15 {
			reboundCount++
			if locator.NodeID != nil || locator.ItemID != nil {
				t.Fatalf("rebound locator retained Meta IDs: %#v", locator)
			}
		}
		if locator.EngineID == 2 && (locator.ItemID == nil || *locator.ItemID != 99) {
			t.Fatalf("unrelated locator changed: %#v", locator)
		}
	}
	if reboundCount != 2 {
		t.Fatalf("rebound locator count = %d", reboundCount)
	}

	stored, err := svc.GetDevTask(34, 7)
	if err != nil {
		t.Fatalf("GetDevTask() after rebind error = %v", err)
	}
	if got := collectWorkflowStorageEngineBindings(stored.Content); len(got) != 2 || got[1].engineID != 15 {
		t.Fatalf("stored bindings = %#v", got)
	}
}

func TestWorkflowStorageEngineRebindRejectsIncompatibleTarget(t *testing.T) {
	db := newNotebookBindingTestDB(t)
	updatedAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, dev_type, content, execution_config, editor_layout,
			timeout, tags, updated_at, status
		) VALUES (
			35, 7, 'table_workflow', 'workflow',
			CAST('{"workflow_definition":{"tasks":[{"params":{"locator":"addp://engine/5/path/public/source?type=table"}}]}}' AS BLOB),
			CAST('{"engine_id":1}' AS BLOB), CAST('{}' AS BLOB), 300, '{}', ?, 'active'
		)
	`, updatedAt).Error; err != nil {
		t.Fatalf("seed workflow: %v", err)
	}

	systemServer := newWorkflowBindingSystemServer(t)
	defer systemServer.Close()
	svc := NewDevTaskService(
		repository.NewDevTaskRepository(db),
		commonClient.NewSystemServiceClient(systemServer.URL, workflowBindingTokenSource{}, systemServer.Client()),
	)

	_, err := svc.RebindWorkflowStorageEngine(context.Background(), 35, 7, 3, 5, 16)
	if !errors.Is(err, ErrStorageEngineIncompatible) {
		t.Fatalf("error = %v, want ErrStorageEngineIncompatible", err)
	}
	stored, getErr := svc.GetDevTask(35, 7)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got := collectWorkflowStorageEngineBindings(stored.Content); len(got) != 1 || got[0].engineID != 5 {
		t.Fatalf("stored bindings changed: %#v", got)
	}
}

func TestWorkflowStorageEngineBindingUpdateDetectsConcurrentTaskChange(t *testing.T) {
	db := newNotebookBindingTestDB(t)
	updatedAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, dev_type, content, execution_config, editor_layout,
			timeout, tags, updated_at, status
		) VALUES (
			36, 7, 'concurrent_workflow', 'workflow', CAST('{"workflow_definition":{"tasks":[]}}' AS BLOB),
			CAST('{"engine_id":1}' AS BLOB), CAST('{}' AS BLOB), 300, '{}', ?, 'active'
		)
	`, updatedAt).Error; err != nil {
		t.Fatalf("seed workflow: %v", err)
	}

	repo := repository.NewDevTaskRepository(db)
	item, err := repo.FindByID(36, 7)
	if err != nil {
		t.Fatal(err)
	}
	expectedUpdatedAt := item.UpdatedAt
	if err := db.Exec(
		"UPDATE develop.dev_tasks SET updated_at = ? WHERE id = ?",
		expectedUpdatedAt.Add(time.Second), item.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	item.Content["rebind_marker"] = true

	err = repo.UpdateWorkflowStorageEngineBindings(item, 3, expectedUpdatedAt)
	if !errors.Is(err, repository.ErrDevTaskConcurrentUpdate) {
		t.Fatalf("error = %v, want ErrDevTaskConcurrentUpdate", err)
	}
	stored, err := repo.FindByID(36, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := stored.Content["rebind_marker"]; exists {
		t.Fatalf("concurrent update overwrote stored content: %#v", stored.Content)
	}
}

func newWorkflowBindingSystemServer(t *testing.T) *httptest.Server {
	t.Helper()
	postgresCapabilities := commonModels.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"postgresql",
		"engine_family":"tabular",
		"storage":{"catalog_model":{"path_version":"catalog.path/v1","root_term":"server","levels":[{"term":"schema","kinds":["namespace"],"role":"branch"},{"term":"table","kinds":["table","view","materialized_view"],"role":"leaf"}]},"catalog":{"supported":true}}
	}`)
	dorisCapabilities := commonModels.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"doris",
		"engine_family":"tabular",
		"storage":{"catalog_model":{"path_version":"catalog.path/v1","root_term":"server","levels":[{"term":"database","kinds":["namespace"],"role":"branch"},{"term":"table","kinds":["table","view"],"role":"leaf"}]},"catalog":{"supported":true}}
	}`)
	objectCapabilities := commonModels.JSONString(`{
		"schema_version":"engine.capabilities/v1",
		"engine_type":"minio",
		"engine_family":"object",
		"storage":{"catalog_model":{"path_version":"catalog.path/v1","root_term":"service","levels":[{"term":"bucket","kinds":["bucket"],"role":"branch"},{"term":"prefix","kinds":["prefix"],"role":"branch"},{"term":"object","kinds":["object"],"role":"leaf"}]},"catalog":{"supported":true}}
	}`)
	descriptors := []commonModels.EngineRuntimeDescriptor{
		{ID: 2, Name: "Current PostgreSQL", EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive, Capabilities: &postgresCapabilities},
		{ID: 15, Name: "Replacement Doris", EngineType: "doris", LifecycleState: commonModels.EngineLifecycleActive, Capabilities: &dorisCapabilities},
		{ID: 16, Name: "Object Storage", EngineType: "minio", LifecycleState: commonModels.EngineLifecycleActive, Capabilities: &objectCapabilities},
		{ID: 17, Name: "Inactive PostgreSQL", EngineType: "postgresql", LifecycleState: "inactive", Capabilities: &postgresCapabilities},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/system/runtime/engine-descriptors" {
			t.Fatalf("System path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": descriptors, "total": len(descriptors), "page": 1, "page_size": 100,
		})
	}))
}
