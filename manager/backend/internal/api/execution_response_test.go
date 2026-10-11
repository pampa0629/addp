package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func managerResponseFixture(t *testing.T) *execution.TaskExecution {
	t.Helper()
	metadata := commonModels.JSONMap{}
	if err := json.Unmarshal([]byte(`{
		"total":0,"generated":2,"rebuilt":0,"ready_skipped":0,"failed":0,"unsupported":0,"missing_source":0,
		"tiles_processed":2,"generated_tiles":2,"total_size_bytes":1024,
		"private":"private-sentinel","access_plan":{"token":"private-sentinel"},
		"workflow_runtime":{"engine_type":"model3d_workflow","operator":"max_to_glb","mode":"direct","execution_time_ms":15,"execution_id":"private-sentinel","raw_response":"private-sentinel"},
		"glb_facts":{"size_bytes":1024,"source_format":"max","stdout":"private-sentinel","stderr":"private-sentinel","command":"private-sentinel","publish":{"token":"private-sentinel"},
			"conversion":{"source_unit":"mm","unit_source":"user","scale_to_meters":0.001,"importer_version":"1.9.2","texture_refs":["horse.dds"],"source_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","meshes":5,"scope":"static","private":"private-sentinel"}},
		"lineage_facts":{"schema_version":"addp.lineage-facts/v1",
			"inputs":[{"port":"source","locator":"addp://engine/1/path/a.max?type=object","item_id":9,"item_fingerprint":"source-fingerprint","schema_snapshot":{"sample":"private-sentinel"},"private":"private-sentinel"}],
			"outputs":[{"port":"result","locator":"addp-infra://minio/manager/tenant_1/model_3d_glb/a.glb?type=object","write_mode":"replace"}],
			"operations":[{"kind":"derive","operator":"model_3d_glb_generation","input_ports":["source"],"output_ports":["result"],"field_mappings":[{"transformation":"private-sentinel"}],"private":"private-sentinel"}]}
	}`), &metadata); err != nil {
		t.Fatal(err)
	}
	history := "deleted-definition"
	authorizationID := int64(99)
	version := int64(1)
	expiresAt := time.Now().Add(time.Hour)
	return &execution.TaskExecution{
		TenantID: 1, ExecutionID: uuid.NewString(), Module: "manager", TaskType: execution.TaskTypeModel3DGLBGeneration,
		Source: "manager", SourceTaskID: &history, Status: "failed", TriggerType: "manual",
		ExecutionConfig: commonModels.JSONMap{"password": "private-sentinel"}, Metadata: metadata,
		ExecutionAuthorizationID: &authorizationID, ActorPrincipalID: &authorizationID,
		ActorTenantMembershipID: &authorizationID, IssuedAuthorizationVersion: &version, AuthorizationExpiresAt: &expiresAt,
		ErrorDetails: commonModels.JSONMap{"code": "conversion_failed", "message": "private-sentinel", "raw": "private-sentinel"},
	}
}

func assertManagerResponseProjection(t *testing.T, raw []byte) {
	t.Helper()
	for _, forbidden := range []string{"private-sentinel", "execution_config", "execution_authorization_id", "actor_principal_id", "actor_tenant_membership_id", "issued_authorization_version", "authorization_expires_at", "access_plan", "schema_snapshot", "field_mappings", "stdout", "stderr", "command", "publish", "raw_response"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("response contains forbidden field or value %q: %s", forbidden, raw)
		}
	}
	var payload ManagerExecutionResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	metadata := payload.Metadata
	if metadata.Total == nil || *metadata.Total != 0 || metadata.Generated == nil || *metadata.Generated != 2 || metadata.Failed == nil || *metadata.Failed != 0 {
		t.Fatalf("embedding counters lost: %s", raw)
	}
	if facts := metadata.LineageFacts; facts == nil || len(facts.Inputs) != 1 || len(facts.Outputs) != 1 || len(facts.Operations) != 1 || facts.Inputs[0].ItemID == nil || *facts.Inputs[0].ItemID != 9 || facts.Operations[0].Operator != execution.TaskTypeModel3DGLBGeneration {
		t.Fatalf("professional lineage lost: %s", raw)
	}
	if facts := metadata.GLBFacts; facts == nil || facts.Conversion == nil || facts.Conversion.ScaleToMeters == nil || *facts.Conversion.ScaleToMeters != .001 || facts.Conversion.Meshes == nil || *facts.Conversion.Meshes != 5 || len(facts.Conversion.TextureRefs) != 1 {
		t.Fatalf("professional conversion lost: %s", raw)
	}
	if metadata.WorkflowRuntime == nil || metadata.WorkflowRuntime.Operator != "max_to_glb" || payload.ErrorDetails["code"] != "conversion_failed" || payload.ErrorDetails["category"] == nil {
		t.Fatalf("runtime summary or safe failure lost: %s", raw)
	}
}

func TestManagerExecutionResponsePreservesFactsWithoutMutatingHistory(t *testing.T) {
	item := managerResponseFixture(t)
	before, _ := json.Marshal(item)
	response, err := newManagerExecutionResponse(item)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	assertManagerResponseProjection(t, raw)
	after, _ := json.Marshal(item)
	if string(before) != string(after) {
		t.Fatal("read projection mutated persisted execution facts")
	}
}

func TestManagerExecutionHTTPUsesExplicitResponse(t *testing.T) {
	db := newTaskProviderHandlerTestDB(t)
	item := managerResponseFixture(t)
	if err := db.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	handler := NewTaskProviderHandler(nil, nil, nil, nil, execution.NewTaskExecutionRepository(db))
	router := gin.New()
	router.Use(func(c *gin.Context) { setManagerExecutionReadTestUser(c, 1, 1) })
	router.GET("/executions/:execution_id", handler.ExecutionStatus)
	router.GET("/provider/executions/:execution_id", handler.ProviderExecutionStatus)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/executions/"+item.ExecutionID, nil))
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	assertManagerResponseProjection(t, response.Body.Bytes())
	for _, invalid := range []commonModels.JSONMap{
		{"total": "private-sentinel"},
		{"glb_facts": commonModels.JSONMap{"conversion": commonModels.JSONMap{"meshes": "private-sentinel"}}},
		{"lineage_facts": commonModels.JSONMap{"schema_version": "private-sentinel"}},
	} {
		if err := db.Model(item).Update("metadata", invalid).Error; err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/executions/"+item.ExecutionID, nil))
		if response.Code != 500 || strings.Contains(response.Body.String(), "private-sentinel") {
			t.Fatalf("malformed facts exposed or accepted: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("projection-test-failure", func(tx *gorm.DB) {
		tx.AddError(errors.New("private-sentinel"))
	}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/executions/", "/provider/executions/"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", prefix+item.ExecutionID, nil))
		if response.Code != 500 || strings.Contains(response.Body.String(), "private-sentinel") {
			t.Fatalf("repository error exposed: status=%d body=%s", response.Code, response.Body.String())
		}
	}
}
