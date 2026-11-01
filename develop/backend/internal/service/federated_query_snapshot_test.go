package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
)

func testFrozenFederation() *models.FederatedQueryBinding {
	return &models.FederatedQueryBinding{SchemaVersion: federatedBindingSchema, SelectedEngineID: 90, RuntimeEngineID: 99, Audience: "spark", SourceEngineIDs: []uint{12, 13}}
}

func TestFederatedBindingSurvivesDurableSnapshotWithoutLeakingIntoTask(t *testing.T) {
	task := &models.DevTask{DevType: "query", ExecutionConfig: models.DevTaskContent{"engine_id": 90}, Content: models.DevTaskContent{"query_type": "sql", "query": "SELECT * FROM source.public.orders"}, Timeout: 30, QueryResultLimit: 10, FederatedBinding: testFrozenFederation()}
	definition, err := json.Marshal(task)
	if err != nil || strings.Contains(string(definition), "runtime_engine_id") {
		t.Fatalf("execution binding leaked into task: %s %v", definition, err)
	}
	config := devTaskExecutionRecordConfig(task, nil)
	task.FederatedBinding.SourceEngineIDs[0] = 77
	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var durable commonModels.JSONMap
	if err := json.Unmarshal(payload, &durable); err != nil {
		t.Fatal(err)
	}
	restored, err := devQueryTaskFromExecution(&commonExecution.TaskExecution{TenantID: 7, ExecutionConfig: durable})
	if err != nil || !reflect.DeepEqual(restored.FederatedBinding, testFrozenFederation()) {
		t.Fatalf("restored=%+v error=%v", restored, err)
	}
	if strings.Contains(string(payload), "connection_info") || strings.Contains(string(payload), "password") {
		t.Fatal("snapshot persisted credentials")
	}
}

func TestFederatedBindingRejectsIncompleteAndMixedIdentities(t *testing.T) {
	for _, change := range []func(*models.FederatedQueryBinding){
		func(b *models.FederatedQueryBinding) { b.SchemaVersion = "" },
		func(b *models.FederatedQueryBinding) { b.SelectedEngineID = 91 },
		func(b *models.FederatedQueryBinding) { b.RuntimeEngineID = 0 },
		func(b *models.FederatedQueryBinding) { b.Audience = " spark" },
		func(b *models.FederatedQueryBinding) { b.SourceEngineIDs = nil },
		func(b *models.FederatedQueryBinding) { b.SourceEngineIDs = []uint{90} },
		func(b *models.FederatedQueryBinding) { b.SourceEngineIDs = []uint{99} },
		func(b *models.FederatedQueryBinding) { b.SourceEngineIDs = []uint{12, 12} },
		func(b *models.FederatedQueryBinding) { b.SourceEngineIDs = []uint{0} },
	} {
		b := testFrozenFederation()
		change(b)
		if validateFederatedBinding(b, 90) == nil {
			t.Fatalf("accepted invalid binding: %+v", b)
		}
	}
	if _, err := decodeFederatedBinding(map[string]interface{}{"schema_version": federatedBindingSchema, "selected_engine_id": 90, "runtime_engine_id": 99, "audience": "spark", "source_engine_ids": []uint{12}, "connection_info": map[string]string{"password": "private"}}, 90); err == nil {
		t.Fatal("accepted caller connection in binding")
	}
}

func TestFederatedAuthorizationNeverReturnsTargetAsDataSource(t *testing.T) {
	binding := testFrozenFederation()
	ids, err := federatedAuthorizationEngineIDs(binding)
	if err != nil || !reflect.DeepEqual(ids, []uint{12, 13, 90}) || !reflect.DeepEqual(binding.SourceEngineIDs, []uint{12, 13}) {
		t.Fatalf("scopes=%v binding=%+v error=%v", ids, binding, err)
	}
	binding.RuntimeEngineID = binding.SelectedEngineID
	ids, err = federatedAuthorizationEngineIDs(binding)
	if err != nil || !reflect.DeepEqual(ids, []uint{12, 13}) {
		t.Fatalf("self runtime scopes=%v error=%v", ids, err)
	}
}

func TestPersistedFederationCannotRediscoverSourcesOrAcceptMissingBinding(t *testing.T) {
	provider := &traceQueryProvider{t: t}
	executor := &DevExecutor{federatedQuery: provider}
	task := &models.DevTask{DevType: "query", Content: models.DevTaskContent{"query_type": "sql", "query": "SELECT * FROM renamed_source.public.orders"}, ExecutionConfig: models.DevTaskContent{"engine_id": 90}, FederatedBinding: testFrozenFederation()}
	ids, err := executor.federatedReadEngineIDs(context.Background(), task, 7)
	if err != nil || !reflect.DeepEqual(ids, []uint{12, 13}) {
		t.Fatalf("fixed sources=%v error=%v", ids, err)
	}
	ids[0] = 77
	if task.FederatedBinding.SourceEngineIDs[0] != 12 {
		t.Fatal("mutated frozen source set")
	}
	task.FederatedBinding = nil
	if _, err := executor.federatedReadEngineIDs(context.Background(), task, 7); err == nil {
		t.Fatal("missing snapshot rediscovered sources")
	}
}

func TestFederatedBindingFailureUsesExecutionLanguage(t *testing.T) {
	for _, lang := range []string{"zh-cn", "en"} {
		ctx, err := commoni18n.WithLanguage(context.Background(), lang)
		if err != nil {
			t.Fatal(err)
		}
		message := federatedBindingError(ctx).Error()
		if lang == "en" && !strings.Contains(message, "Start a new execution") || lang == "zh-cn" && !strings.Contains(message, "重新发起执行") {
			t.Fatalf("language=%s message=%s", lang, message)
		}
	}
}
