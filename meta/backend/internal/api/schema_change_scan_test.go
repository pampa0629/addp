package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func testAppliedSchemaChangeScan(t *testing.T, db *gorm.DB, router http.Handler, read func(string, string, int) map[string]interface{}) {
	t.Helper()
	id, token, task := uuid.NewString(), uuid.NewString(), "57"
	locator := "addp://engine/9/path/public/changed?type=table"
	origin := map[string]interface{}{"request_id": 61, "task_id": 57, "generation": 1, "from_revision": 1, "to_revision": 2, "target_locator": locator, "applied_at": time.Now().Add(-time.Minute), "triggered_by": 31, "actor_principal_id": 31, "actor_tenant_membership_id": 51, "issued_authorization_version": 6, "claim_token": token, "attempt": 1, "lease_until": time.Now().Add(time.Hour)}
	historicalBy := 32
	record := execution.TaskExecution{TenantID: scanReadTenant, ExecutionID: id, Module: "transfer", TaskType: "sync", Source: "transfer", SourceTaskID: &task, Status: "failed", ExecutionBoundary: "continuous", TriggerType: "manual", TriggeredBy: &historicalBy, Metadata: commonmodels.JSONMap{"stop_reason": "schema_change_blocked", "schema_change_scan_origin": origin, "continuous": map[string]interface{}{"schema_change": map[string]interface{}{"request_id": 61, "status": "applied", "generation": 1, "from_revision": 1, "to_revision": 2, "metadata_scan": map[string]interface{}{"status": "running", "attempt": 1}}}}}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	request := map[string]interface{}{"source": "transfer", "engine_id": 9, "targets": []string{locator}, "scan_depth": "deep", "force": true, "schema_change_source": map[string]interface{}{"execution_id": id, "request_id": 61, "claim_token": token}}
	post := func(auth string, body map[string]interface{}, want int) string {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/v1/meta/scan/run/manual", strings.NewReader(string(data)))
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("schema origin: status=%d want=%d body=%s", w.Code, want, w.Body)
		}
		var out map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		value, _ := out["execution_id"].(string)
		return value
	}
	// The complete applied fact is accepted even though the old execution failed.
	childID := post("transfer-machine", request, 201)
	var child execution.TaskExecution
	if err := db.Where("execution_id = ?", childID).First(&child).Error; err != nil {
		t.Fatal(err)
	}
	if child.ParentExecutionID != nil || child.TriggeredBy == nil || *child.TriggeredBy != 31 || child.ActorPrincipalID == nil || *child.ActorPrincipalID != 31 || child.ActorTenantMembershipID == nil || *child.ActorTenantMembershipID != 51 || child.IssuedAuthorizationVersion == nil || *child.IssuedAuthorizationVersion != 6 || child.ExecutionAuthorizationID != nil || child.LeaseToken != nil {
		t.Fatal("approval scan did not retain only approval origin")
	}
	var unchanged execution.TaskExecution
	if err := db.Where("execution_id = ?", id).First(&unchanged).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.TriggeredBy == nil || *unchanged.TriggeredBy != 32 || unchanged.ActorPrincipalID != nil {
		t.Fatal("approval replaced historical execution actor")
	}
	childJSON, _ := json.Marshal(child)
	if strings.Contains(string(childJSON), token) || child.AuthorizationExpiresAt != nil || child.SourceTaskID != nil {
		t.Fatal("approval scan retained a claim or execution authorization")
	}
	read("owner", "/executions/"+childID, 200)
	read("peer", "/executions/"+childID, 404)
	read("foreign", "/executions/"+childID, 404)
	for _, auth := range []string{"owner", "develop-machine", "manager-machine", "wrong-machine"} {
		post(auth, request, 403)
	}
	for _, source := range []map[string]interface{}{{"execution_id": "invalid", "request_id": 61, "claim_token": token}, {"execution_id": id, "request_id": 0, "claim_token": token}, {"execution_id": id, "request_id": 61, "claim_token": ""}} {
		changed := map[string]interface{}{}
		for k, v := range request {
			changed[k] = v
		}
		changed["schema_change_source"] = source
		post("transfer-machine", changed, 400)
	}
	for _, change := range []map[string]interface{}{{"claim_token": uuid.NewString()}, {"request_id": 62}, {"execution_id": uuid.NewString()}} {
		changed := map[string]interface{}{}
		for k, v := range request {
			changed[k] = v
		}
		source := map[string]interface{}{"execution_id": id, "request_id": 61, "claim_token": token}
		for k, v := range change {
			source[k] = v
		}
		changed["schema_change_source"] = source
		post("transfer-machine", changed, 404)
	}
	for _, change := range []map[string]interface{}{{"parent_execution_id": id}, {"catalog_paths": []string{"public"}}, {"targets": []string{"addp://engine/9/path/public/other?type=table"}}, {"targets": []string{locator, "addp://engine/9/path/public/other?type=table"}}} {
		changed := map[string]interface{}{}
		for k, v := range request {
			changed[k] = v
		}
		for k, v := range change {
			changed[k] = v
		}
		post("transfer-machine", changed, 400)
	}
	for _, fields := range []map[string]interface{}{{"tenant_id": scanReadTenant + 1}, {"status": "running"}, {"execution_boundary": "bounded"}, {"module": "meta"}} {
		original := record
		if err := db.Model(&execution.TaskExecution{}).Where("id = ?", record.ID).Updates(fields).Error; err != nil {
			t.Fatal(err)
		}
		post("transfer-machine", request, 404)
		if err := db.Save(&original).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []map[string]interface{}{{"actor_principal_id": 0}, {"triggered_by": 32}, {"to_revision": 3}, {"lease_until": time.Now().Add(-time.Second)}} {
		stored := map[string]interface{}{}
		for k, v := range origin {
			stored[k] = v
		}
		for k, v := range change {
			stored[k] = v
		}
		metadata := commonmodels.JSONMap{}
		for k, v := range record.Metadata {
			metadata[k] = v
		}
		metadata["schema_change_scan_origin"] = stored
		if err := db.Model(&record).Update("metadata", metadata).Error; err != nil {
			t.Fatal(err)
		}
		post("transfer-machine", request, 404)
	}
}
