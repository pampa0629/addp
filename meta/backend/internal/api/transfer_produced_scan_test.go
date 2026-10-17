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

// Runs through the same HTTP contracts on SQLite T1 and PostgreSQL T2.
func testTransferAndVectorProducedScans(t *testing.T, db *gorm.DB, router http.Handler, read func(string, string, int) map[string]interface{}) {
	t.Helper()
	for _, kind := range []string{"bounded", "scheduled", "continuous", "vector"} {
		t.Run("produced_"+kind, func(t *testing.T) {
			by, principal, membership, version := 31, int64(31), int64(51), int64(6)
			id, owner, token := uuid.NewString(), "producer", uuid.NewString()
			expires := time.Now().Add(time.Hour)
			parent := execution.TaskExecution{TenantID: scanReadTenant, ExecutionID: id, Module: "transfer", TaskType: "sync", Source: "transfer", Status: "running", TriggerType: "manual", ExecutionBoundary: kind, TriggeredBy: &by, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version}
			if kind == "scheduled" {
				parent.ExecutionBoundary = "bounded"
				parent.TriggerType = "scheduled"
			}
			locator := "addp://engine/9/path/public/produced?type=table"
			parent.Metadata = commonmodels.JSONMap{"outputs": commonmodels.JSONMap{"execution_id": id, "target_locator": locator, "continuous_prepared": kind == "continuous"}}
			req := models.ScanRequest{Source: "transfer", ParentExecutionID: id, EngineID: 9, Targets: []string{locator}, ScanDepth: "deep", Force: true}
			credential := "transfer-machine"
			if kind == "vector" {
				parent.Module, parent.TaskType, parent.Source, parent.ExecutionBoundary = "manager", execution.TaskTypeVectorTileSetGeneration, "manager", "bounded"
				parent.Attempt, parent.LeaseOwner, parent.LeaseToken, parent.LeaseExpiresAt = 1, &owner, &token, &expires
				parent.Metadata = commonmodels.JSONMap{"catalog_path": "results/roads.pmtiles", "produced_target": commonclient.MetaScanProducedTarget{EngineID: 9, CatalogPath: "results/roads.pmtiles", Attempt: 1}}
				req.Source, req.Targets, req.RefGroups = "manager", nil, []models.ScanRefGroup{{Primary: "results/roads.pmtiles"}}
				credential = "manager-machine"
			}
			if err := db.Create(&parent).Error; err != nil {
				t.Fatal(err)
			}
			post := func(auth string, request models.ScanRequest, want int) string {
				t.Helper()
				body, _ := json.Marshal(request)
				r := httptest.NewRequest("POST", "/api/v1/meta/scan/run/manual", strings.NewReader(string(body)))
				r.Header.Set("Authorization", "Bearer "+auth)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != want {
					t.Fatalf("%s status=%d want=%d body=%s", auth, w.Code, want, w.Body)
				}
				var out map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &out)
				id, _ := out["execution_id"].(string)
				return id
			}
			for _, auth := range []string{"owner", "develop-machine", "wrong-machine"} {
				post(auth, req, 403)
			}
			missing := req
			missing.ParentExecutionID = ""
			post(credential, missing, 400)
			changed := req
			changed.Targets = []string{"addp://engine/9/path/public/other?type=table"}
			changed.RefGroups = nil
			post(credential, changed, 404)
			changed = req
			changed.CatalogPaths = []string{"public"}
			if kind == "vector" {
				post(credential, changed, 404)
			} else {
				post(credential, changed, 400)
			}
			for _, fields := range []map[string]interface{}{{"status": "success"}, {"triggered_by": 32}, {"tenant_id": scanReadTenant + 1}, {"metadata": commonmodels.JSONMap{}}} {
				if err := db.Model(&execution.TaskExecution{}).Where("id = ?", parent.ID).Updates(fields).Error; err != nil {
					t.Fatal(err)
				}
				post(credential, req, 404)
				if err := db.Save(&parent).Error; err != nil {
					t.Fatal(err)
				}
			}
			childID := post(credential, req, 201)
			var child execution.TaskExecution
			if err := db.Where("execution_id = ?", childID).First(&child).Error; err != nil {
				t.Fatal(err)
			}
			if child.ParentExecutionID == nil || *child.ParentExecutionID != id || child.ActorPrincipalID == nil || *child.ActorPrincipalID != principal || child.ActorTenantMembershipID == nil || *child.ActorTenantMembershipID != membership || child.IssuedAuthorizationVersion == nil || *child.IssuedAuthorizationVersion != version || child.TriggeredBy == nil || *child.TriggeredBy != by || child.ExecutionAuthorizationID != nil || child.AuthorizationExpiresAt != nil || child.LeaseToken != nil {
				t.Fatal("scan did not inherit only verified identity and causal parent")
			}
			read("owner", "/executions/"+childID, 200)
			read("peer", "/executions/"+childID, 404)
			read("foreign", "/executions/"+childID, 404)
		})
	}
	t.Run("applied_schema_change", func(t *testing.T) { testAppliedSchemaChangeScan(t, db, router, read) })
}
