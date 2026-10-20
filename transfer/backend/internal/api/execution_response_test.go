package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/gin-gonic/gin"
)

func transferResponseFixture() *models.TaskExecution {
	return &models.TaskExecution{
		ID: 3, ExecutionID: "execution-3", TaskID: 7, Status: models.ExecutionStatusFailed,
		RecordsRead: 11, RecordsWritten: 9, BytesRead: 120, BytesWritten: 100,
		ErrorMsg:         "private-sentinel sql password=secret",
		ErrorDetails:     execution.Observe(&execution.TaskExecution{Status: "failed", ErrorDetails: commonmodels.JSONMap{"message": "private-sentinel", "code": "record_decode"}}).ErrorDetails,
		CheckpointOffset: 42, CheckpointState: models.JSONMap{"value": "private-sentinel"},
		Metadata: models.JSONMap{
			"target_production": map[string]interface{}{"execution_id": "private-sentinel", "phase": "committed"},
			"execution_inputs":  map[string]interface{}{"sql": "private-sentinel"},
			"checkpoint_state":  map[string]interface{}{"cursor": "private-sentinel"}, "fencing_token": "private-sentinel",
			"recovery_attempt": 2, "recovery_consecutive_failures": 0, "recovery_backoff_seconds": 30,
			"recovery_not_before": time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC), "recovery_reason": "lease_expired", "recovery_circuit_state": "open", "stop_reason": "private-sentinel",
			"lineage_facts": map[string]interface{}{
				"schema_version": execution.LineageFactsSchemaVersion,
				"inputs":         []interface{}{map[string]interface{}{"port": "source", "locator": "addp://engine/1/path/public/source?type=table", "schema_snapshot": map[string]interface{}{"hash": "schema-hash", "fields": []interface{}{map[string]interface{}{"name": "id", "type": "integer", "nullable": false, "comment": "private-sentinel", "default_expression": "private-sentinel", "generation_expression": "private-sentinel", "extra": "private-sentinel"}}}, "extra": "private-sentinel"}},
				"operations":     []interface{}{map[string]interface{}{"kind": "copy", "field_lineage_status": "complete", "field_mappings": []interface{}{map[string]interface{}{"input_port": "source", "output_port": "target", "source_field": "id", "target_field": "id", "transformation": "direct", "sql": "private-sentinel"}}, "response": "private-sentinel"}},
				"meta_scan_refs": []string{"scan-execution"}, "runtime_execution_id": "runtime-execution", "private": "private-sentinel",
			},
			"continuous": map[string]interface{}{
				"partitions":    map[string]interface{}{"0": map[string]interface{}{"type": "kafka_offset", "version": "v1", "values": map[string]interface{}{"next_offset": "42", "value": "private-sentinel"}, "fencing_token": "private-sentinel"}},
				"diagnostics":   map[string]interface{}{"health": "degraded", "checkpoint_health": "healthy", "error": "private-sentinel", "partitions": map[string]interface{}{"0": map[string]interface{}{"partition": "0", "earliest_offset": 10, "latest_offset": 45, "next_offset": 42, "lag_records": 3, "recovery_headroom_records": 32, "health": "degraded", "checkpoint_health": "healthy", "private": "private-sentinel"}}, "private": "private-sentinel"},
				"capture":       map[string]interface{}{"generation": 2, "source_recovery": map[string]interface{}{"schema_version": "capture.source_recovery/v1", "provider": "oracle", "health": "healthy", "capture_position": "42", "private": "private-sentinel"}, "source_transactions": map[string]interface{}{"schema_version": "capture.source_transactions/v1", "provider": "oracle", "status": "available", "active_count": 1, "private": "private-sentinel"}},
				"schema_change": map[string]interface{}{"request_id": 9, "status": "pending", "missing_fields": []string{"removed"}, "unexpected_fields": []string{"added"}, "metadata_scan": map[string]interface{}{"status": "succeeded", "execution_id": "scan-execution", "lease_until": time.Now(), "claim_token": "private-sentinel"}},
			},
			"target_refs": []interface{}{map[string]interface{}{"path": "tenant/export/result.csv", "primary": true, "required": true, "role": "primary", "extension": ".csv", "entry": "result.csv", "private": "private-sentinel"}},
		},
	}
}

func TestTransferExecutionResponseRetainsOnlyDeclaredProfessionalFacts(t *testing.T) {
	for _, language := range []string{"zh-cn", "en"} {
		t.Run(language, func(t *testing.T) {
			item := transferResponseFixture()
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/execution", func(c *gin.Context) { respondExecution(c, 200, item) })
			req := httptest.NewRequest("GET", "/execution", nil)
			req.Header.Set("Accept-Language", language)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != 200 {
				t.Fatalf("status=%d %s", response.Code, response.Body.String())
			}
			for _, private := range []string{"private-sentinel", "checkpoint_state", "checkpoint_offset", "fencing_token", "default_expression", "generation_expression", "comment", "claim_token", "lease_until", "target_refs", "error_msg"} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatalf("exposed %s: %s", private, response.Body.String())
				}
			}
			var body ExecutionResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.ErrorDetails["category"] != "execution_failed" || body.ErrorDetails["code"] != "record_decode" || body.RecordsWritten != 9 {
				t.Fatalf("lost overview: %#v", body)
			}
			facts := body.Metadata.LineageFacts
			if facts.Inputs[0].SchemaSnapshot.Hash != "schema-hash" || facts.Inputs[0].SchemaSnapshot.Fields[0].Name != "id" || facts.Operations[0].FieldMappings[0].Transformation != "direct" || facts.MetaScanRefs[0] != "scan-execution" {
				t.Fatalf("lost frozen lineage: %#v", facts)
			}
			continuous := body.Metadata.Continuous
			if *continuous.Partitions["0"].Values.NextOffset != 42 || *continuous.Diagnostics.Partitions["0"].LagRecords != 3 || continuous.Capture.SourceRecovery.CapturePosition != "42" || continuous.Capture.SourceTransactions.ActiveCount != 1 || continuous.SchemaChange.RequestID != 9 {
				t.Fatalf("lost continuous diagnostics: %#v", continuous)
			}
			if body.Metadata.RecoveryCircuitState != "open" || *body.Metadata.RecoveryAttempt != 2 || *body.Metadata.RecoveryConsecutiveFailures != 0 || body.Metadata.StopReason != "" {
				t.Fatalf("lost recovery: %#v", body.Metadata)
			}
			if language == "en" && !strings.Contains(continuous.Diagnostics.Error, "temporarily unavailable") {
				t.Fatalf("diagnostics not localized: %s", continuous.Diagnostics.Error)
			}
			if language == "zh-cn" && !strings.Contains(continuous.Diagnostics.Error, "暂时不可用") {
				t.Fatalf("diagnostics not localized: %s", continuous.Diagnostics.Error)
			}
			if item.Metadata["stop_reason"] != "private-sentinel" || item.ErrorMsg == "" {
				t.Fatal("response projection mutated runtime facts")
			}
		})
	}
}

func TestTransferModuleResultRetainsOnlyExportContractAndSafeFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	response, err := projectModuleExecution(c, transferResponseFixture())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-sentinel", "lineage_facts", "continuous", "checkpoint", "execution_config"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("module result exposed %s", private)
		}
	}
	if len(response.Metadata.TargetRefs) != 1 || response.Metadata.TargetRefs[0].Path != "tenant/export/result.csv" || !response.Metadata.TargetRefs[0].Primary || response.ErrorMsg == "" {
		t.Fatalf("lost export result: %#v", response)
	}
}

func TestTransferMalformedProfessionalFactsFailClosedWithoutRawDetails(t *testing.T) {
	item := transferResponseFixture()
	item.Metadata["continuous"] = map[string]interface{}{"diagnostics": map[string]interface{}{"partitions": map[string]interface{}{"0": map[string]interface{}{"next_offset": "private-sentinel"}}}}
	router := gin.New()
	router.GET("/execution", func(c *gin.Context) { respondExecution(c, 200, item) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/execution", nil))
	if response.Code != 500 || strings.Contains(response.Body.String(), "private-sentinel") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTransferCommittedOffsetUsesCanonicalDecimalString(t *testing.T) {
	for _, offset := range []string{"0", "42", "9223372036854775807"} {
		t.Run(offset, func(t *testing.T) {
			item := transferResponseFixture()
			item.Metadata["continuous"] = map[string]interface{}{"partitions": map[string]interface{}{"0": map[string]interface{}{
				"type": "kafka_offset", "version": "v1", "values": map[string]string{"next_offset": offset, "private": "private-sentinel"},
			}}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			response, err := projectExecution(c, item)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"next_offset":"`+offset+`"`) || strings.Contains(string(data), "private-sentinel") {
				t.Fatalf("committed position changed or leaked: %s", data)
			}
		})
	}
}

func TestProducedScanFeedbackExposesOnlyStableFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, code := range []string{"scan_submission_failed", "private-secret"} {
		item := transferResponseFixture()
		item.Metadata["metadata_scan"] = map[string]interface{}{"status": "failed", "error_code": code, "error": "private-secret", "target": "private-secret", "lease_token": "private-secret"}
		response, err := projectExecution(c, item)
		if err != nil {
			t.Fatal(err)
		}
		got := response.Metadata.MetadataScan
		if got == nil || got.Status != "failed" || (code == "scan_submission_failed" && got.ErrorCode != code) || (code == "private-secret" && got.ErrorCode != "") {
			t.Fatalf("scan failure feedback=%#v", got)
		}
		data, _ := json.Marshal(got)
		if strings.Contains(string(data), "private-secret") {
			t.Fatal("private scan failure leaked")
		}
	}
}

func TestInitialAuthorizationFeedbackExposesNoPrivateDeliveryOrGrantFacts(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, status := range []string{"pending", "denied", "issued"} {
		item := transferResponseFixture()
		id := "00000000-0000-4000-8000-000000000001"
		state := models.ProductionAuthorization{SchemaVersion: "transfer.initial-authorization/v1", Status: status, Attempts: 3, NextAttemptUnix: 123, Code: "scan_pending"}
		if status == "issued" {
			state.GrantRequestID = id
		}
		item.Metadata[models.ProductionAuthorizationMetadataKey] = state
		response, err := projectExecution(c, item)
		if err != nil || response.Metadata.InitialAuthorization == nil || response.Metadata.InitialAuthorization.Status != status {
			t.Fatal("lost initial authorization feedback", response, err)
		}
		body, _ := json.Marshal(response.Metadata.InitialAuthorization)
		for _, private := range []string{"attempts", "next_attempt_unix", "grant_request_id", "schema_version", id} {
			if strings.Contains(string(body), private) {
				t.Fatalf("private continuation fact leaked: %s", body)
			}
		}
	}
}
