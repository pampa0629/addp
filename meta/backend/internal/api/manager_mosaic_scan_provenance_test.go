package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Both direct HTTP T1 and the existing PostgreSQL read gate call this contract.
func testMetaManagerMosaicProducedScan(t *testing.T, db *gorm.DB, router http.Handler, read func(string, string, int) map[string]interface{}) {
	t.Helper()
	principal, membership, version, authorizationID, by := int64(31), int64(51), int64(6), time.Now().UnixNano(), 31
	expires, owner, token := time.Now().UTC().Add(time.Hour), "manager-worker", uuid.NewString()
	parent := execution.TaskExecution{TenantID: scanReadTenant, ExecutionID: uuid.NewString(), Module: execution.ModuleManager,
		TaskType: execution.TaskTypeRasterMosaicGeneration, Source: execution.ModuleManager, Status: execution.ExecutionStatusRunning,
		ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, TriggeredBy: &by,
		ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version,
		ExecutionAuthorizationID: &authorizationID, AuthorizationExpiresAt: &expires, Attempt: 1, LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &expires,
		Metadata: commonmodels.JSONMap{"manifest_locator": "/vsis3/results/mosaic/mosaic.addp.json", "manifest_ref": "mosaic.addp.json",
			"produced_target": commonclient.MetaScanProducedTarget{EngineID: 9, CatalogPath: "results/mosaic", ManifestRef: "mosaic.addp.json", Attempt: 1}}}
	req := models.ScanRequest{EngineID: 9, CatalogPaths: []string{"results/mosaic"}, ScanDepth: "deep", Force: true,
		Source: commonclient.MetaScanSourceManagerMosaicProducedTarget, ParentExecutionID: parent.ExecutionID}
	post := func(credential string, request models.ScanRequest, want int) string {
		t.Helper()
		encoded, _ := json.Marshal(request)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/meta/scan/run/manual", strings.NewReader(string(encoded)))
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s mosaic scan: status=%d want=%d body=%s", credential, w.Code, want, w.Body)
		}
		for _, field := range []string{"execution_config", "actor_principal_id", "execution_authorization_id", "lease_token", token, `"produced_target":`} {
			if strings.Contains(w.Body.String(), field) {
				t.Fatalf("mosaic scan response leaked %s", field)
			}
		}
		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if want >= 400 {
			return ""
		}
		id, ok := response["execution_id"].(string)
		if !ok || id == "" {
			t.Fatal("mosaic scan response has no execution UUID")
		}
		return id
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{"owner", "oauth", "machine", "wrong-machine", "develop-machine", "manager-machine-denied"} {
		post(credential, req, http.StatusForbidden)
	}
	beforePeer := read("peer", "/scan/runs", http.StatusOK)["total"]
	id := post("manager-machine", req, http.StatusCreated)
	read("owner", "/executions/"+id, http.StatusOK)
	var child execution.TaskExecution
	if err := db.Where("execution_id = ?", id).First(&child).Error; err != nil {
		t.Fatal(err)
	}
	if child.TriggeredBy == nil || *child.TriggeredBy != by || child.ParentExecutionID == nil || *child.ParentExecutionID != parent.ExecutionID || child.ActorPrincipalID == nil || *child.ActorPrincipalID != principal || child.ActorTenantMembershipID == nil || *child.ActorTenantMembershipID != membership || child.IssuedAuthorizationVersion == nil || *child.IssuedAuthorizationVersion != version || child.ExecutionAuthorizationID != nil || child.AuthorizationExpiresAt != nil || child.SourceTaskID != nil || child.LeaseToken != nil {
		t.Fatal("mosaic child did not inherit only the verified origin")
	}
	read("peer", "/executions/"+id, http.StatusNotFound)
	read("foreign", "/executions/"+id, http.StatusNotFound)
	read("denied", "/executions/"+id, http.StatusForbidden)
	if read("peer", "/scan/runs", http.StatusOK)["total"] != beforePeer {
		t.Fatal("mosaic scan leaked into peer list total")
	}
	for _, paths := range [][]string{{"results"}, {"results/sibling"}, {"results/mosaic", "results/sibling"}} {
		changed := req
		changed.CatalogPaths = paths
		post("manager-machine", changed, http.StatusNotFound)
	}
	for _, mutate := range []func(*models.ScanRequest){
		func(r *models.ScanRequest) { r.ParentExecutionID = "" },
		func(r *models.ScanRequest) { r.ParentExecutionID = "not-a-uuid" },
	} {
		changed := req
		mutate(&changed)
		post("manager-machine", changed, http.StatusBadRequest)
	}
	for _, test := range []struct {
		name   string
		mutate func(*execution.TaskExecution)
	}{
		{"foreign_tenant", func(p *execution.TaskExecution) { p.TenantID++ }},
		{"foreign_module", func(p *execution.TaskExecution) { p.Module = execution.ModuleDevelop }},
		{"other_task_type", func(p *execution.TaskExecution) { p.TaskType = execution.TaskTypeRasterCOGGeneration }},
		{"completed_parent", func(p *execution.TaskExecution) { p.Status = execution.ExecutionStatusSuccess }},
		{"missing_actor", func(p *execution.TaskExecution) { p.ActorPrincipalID = nil }},
		{"missing_membership", func(p *execution.TaskExecution) { p.ActorTenantMembershipID = nil }},
		{"missing_version", func(p *execution.TaskExecution) { p.IssuedAuthorizationVersion = nil }},
		{"wrong_triggered_by", func(p *execution.TaskExecution) { other := 32; p.TriggeredBy = &other }},
		{"missing_lease", func(p *execution.TaskExecution) { p.LeaseToken = nil }},
		{"missing_owner", func(p *execution.TaskExecution) { p.LeaseOwner = nil }},
		{"expired_lease", func(p *execution.TaskExecution) { expired := time.Now().Add(-time.Minute); p.LeaseExpiresAt = &expired }},
		{"stale_output_attempt", func(p *execution.TaskExecution) { p.Attempt = 2 }},
		{"unconfirmed_output", func(p *execution.TaskExecution) { p.Metadata = commonmodels.JSONMap{} }},
		{"mismatched_output_engine", func(p *execution.TaskExecution) {
			p.Metadata["produced_target"] = commonclient.MetaScanProducedTarget{EngineID: 10, CatalogPath: "results/mosaic", ManifestRef: "mosaic.addp.json", Attempt: 1}
		}},
	} {
		changed := parent
		changed.ID, changed.ExecutionID = 0, uuid.NewString()
		changed.ExecutionAuthorizationID, changed.AuthorizationExpiresAt = nil, nil
		leaseToken := uuid.NewString()
		changed.LeaseToken = &leaseToken
		changed.Metadata = parent.Metadata.Clone()
		test.mutate(&changed)
		if err := db.Create(&changed).Error; err != nil {
			t.Fatalf("%s parent fixture: %v", test.name, err)
		}
		request := req
		request.ParentExecutionID = changed.ExecutionID
		post("manager-machine", request, http.StatusNotFound)
		var count int64
		if err := db.Model(&execution.TaskExecution{}).Where("parent_execution_id = ?", changed.ExecutionID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s created an untrusted child: %d err=%v", test.name, count, err)
		}
	}
}
