package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

// Shared by the existing direct HTTP T1 and real PostgreSQL T2 entry points.
func testMetaDevelopProducedScan(t *testing.T, db *gorm.DB, router http.Handler, read func(string, string, int) map[string]interface{}) {
	t.Helper()
	principal, membership, version := int64(31), int64(51), int64(6)
	by, grantID := 31, int64(999)
	expires := time.Now().UTC().Add(time.Hour)
	const parentID = "71000009-1111-4111-8111-000000000001"
	const fileLocator = "addp://engine/9/path/results/dem.tif?type=file"
	const tableLocator = "addp://engine/9/path/public/result?type=table"
	const objectLocator = "addp://engine/9/path/results/object.tif?type=object"
	parent := execution.TaskExecution{
		TenantID: scanReadTenant, ExecutionID: parentID, Module: "develop", TaskType: "workflow", Source: "develop",
		Status: "running", TriggerType: "manual", TriggeredBy: &by,
		ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version,
		ExecutionAuthorizationID: &grantID, AuthorizationExpiresAt: &expires,
		Metadata: commonmodels.JSONMap{"outputs": map[string]interface{}{
			"file":   map[string]interface{}{"resource": map[string]interface{}{"locator": fileLocator}},
			"table":  map[string]interface{}{"resource": map[string]interface{}{"locator": tableLocator}},
			"object": map[string]interface{}{"resource": map[string]interface{}{"locator": objectLocator}},
		}},
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	request := models.ScanRequest{EngineID: 9, ScanDepth: "deep", Force: true, Source: commonclient.MetaScanSourceDevelopProducedTarget,
		ParentExecutionID: parentID, RefGroups: []models.ScanRefGroup{{Primary: "results/dem.tif"}}}
	post := func(token string, req models.ScanRequest, want int) string {
		t.Helper()
		encoded, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/v1/meta/scan/run/manual", strings.NewReader(string(encoded)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s scan create: got %d want %d body=%s", token, w.Code, want, w.Body)
		}
		for _, forbidden := range []string{"execution_config", "actor_principal_id", "execution_authorization_id", "outputs", "lease_token"} {
			if strings.Contains(w.Body.String(), forbidden) {
				t.Fatalf("creation leaked %s", forbidden)
			}
		}
		var body map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if want >= 400 {
			for key := range body {
				if key != "error" && key != "error_code" {
					t.Fatalf("denial leaked %s", key)
				}
			}
			return ""
		}
		if metadata, ok := body["metadata"].(map[string]interface{}); !ok || len(metadata) != 0 {
			t.Fatal("creation metadata exceeds the empty safe projection")
		}
		id, ok := body["execution_id"].(string)
		if !ok || id == "" {
			t.Fatal("missing created execution")
		}
		return id
	}
	// Client-declared actor or authorization fields are rejected, never consumed.
	for _, targets := range [][]string{{"invalid"}, {" "}, {tableLocator, "addp://engine/10/path/public/B?type=table"}} {
		post("owner", models.ScanRequest{EngineID: 9, Targets: targets, ScanDepth: "deep"}, 400)
	}
	for _, field := range []string{"triggered_by", "actor_principal_id", "execution_authorization_id"} {
		encoded, _ := json.Marshal(request)
		body := strings.TrimSuffix(string(encoded), "}") + ",\"" + field + "\":32}"
		r := httptest.NewRequest("POST", "/api/v1/meta/scan/run/manual", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer develop-machine")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("client-declared %s accepted: %d", field, w.Code)
		}
	}
	beforePeer := read("peer", "/scan/runs", 200)["total"]
	for _, token := range []string{"owner", "oauth", "machine", "wrong-machine", "develop-machine-denied"} {
		post(token, request, 403)
	}
	spacedSource := request
	spacedSource.Source = " " + request.Source + " "
	post("owner", spacedSource, 403)
	for _, mutate := range []func(*models.ScanRequest){
		func(r *models.ScanRequest) { r.ParentExecutionID = "" },
		func(r *models.ScanRequest) { r.ParentExecutionID = "not-a-uuid" },
		func(r *models.ScanRequest) { r.Source = "meta" },
	} {
		req := request
		mutate(&req)
		post("develop-machine", req, 400)
	}
	for _, mutate := range []func(*models.ScanRequest){
		func(r *models.ScanRequest) { r.ParentExecutionID = "71000009-9999-4999-8999-000000000001" },
		func(r *models.ScanRequest) { r.RefGroups = []models.ScanRefGroup{{Primary: "results/other.tif"}} },
		func(r *models.ScanRequest) {
			r.RefGroups = []models.ScanRefGroup{{Primary: "results/dem.tif", Refs: []models.ScanRef{{Path: "results/private.tif"}}}}
		},
		func(r *models.ScanRequest) { r.CatalogPaths = []string{"results"} },
		func(r *models.ScanRequest) { r.RefGroups = nil },
	} {
		req := request
		mutate(&req)
		post("develop-machine", req, 404)
	}
	for i, mutate := range []func(*execution.TaskExecution){
		func(p *execution.TaskExecution) { p.TenantID++ },
		func(p *execution.TaskExecution) { p.Module = "orchestrator" },
		func(p *execution.TaskExecution) { p.TaskType = "query" },
		func(p *execution.TaskExecution) { p.Status = "success" },
		func(p *execution.TaskExecution) {
			p.ActorPrincipalID = nil
			p.ActorTenantMembershipID = nil
			p.IssuedAuthorizationVersion = nil
			p.ExecutionAuthorizationID = nil
			p.AuthorizationExpiresAt = nil
		},
		func(p *execution.TaskExecution) { peer := 32; p.TriggeredBy = &peer },
		func(p *execution.TaskExecution) { p.Metadata = nil },
	} {
		p := parent
		p.ID = 0
		p.ExecutionAuthorizationID = nil
		p.AuthorizationExpiresAt = nil
		p.ExecutionID = fmt.Sprintf("71000009-2222-4222-8222-%012d", i+1)
		mutate(&p)
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		req := request
		req.ParentExecutionID = p.ExecutionID
		post("develop-machine", req, 404)
	}
	var ids []string
	ids = append(ids, post("develop-machine", request, 201))
	for _, target := range []string{tableLocator, objectLocator} {
		req := request
		req.RefGroups = nil
		req.Targets = []string{target}
		ids = append(ids, post("develop-machine", req, 201))
	}
	for _, id := range ids {
		var child execution.TaskExecution
		if err := db.Where("execution_id = ?", id).First(&child).Error; err != nil {
			t.Fatal(err)
		}
		if child.ParentExecutionID == nil || *child.ParentExecutionID != parentID || child.TriggeredBy == nil || *child.TriggeredBy != by ||
			child.ActorPrincipalID == nil || *child.ActorPrincipalID != principal || child.ActorTenantMembershipID == nil || *child.ActorTenantMembershipID != membership ||
			child.IssuedAuthorizationVersion == nil || *child.IssuedAuthorizationVersion != version || child.SourceTaskID != nil || child.ExecutionAuthorizationID != nil || child.AuthorizationExpiresAt != nil {
			t.Fatal("scan provenance / authorization boundary changed")
		}
		config := scanflow.ParseExecutionConfig(child.ExecutionConfig)
		if id != ids[0] {
			if len(config.Targets) != 1 || len(config.CatalogPaths) != 0 || len(config.RefGroups) != 0 {
				t.Fatal("produced leaf expanded before Worker dispatch")
			}
			want := tableLocator
			if id == ids[2] {
				want = objectLocator
			}
			if config.Targets[0] != want {
				t.Fatal("execution configuration lost the produced target")
			}
		}
		read("owner", "/executions/"+id, 200)
		read("peer", "/executions/"+id, 404)
		read("foreign", "/executions/"+id, 404)
		read("denied", "/executions/"+id, 403)
	}
	if read("peer", "/scan/runs", 200)["total"] != beforePeer {
		t.Fatal("peer list count leaked derived scans")
	}
	if read("owner", "/scan/runs", 200)["total"] != float64(5) {
		t.Fatal("initiator list missing derived scans")
	}
	var children int64
	if err := db.Model(&execution.TaskExecution{}).Where("parent_execution_id = ?", parentID).Count(&children).Error; err != nil || children != 3 {
		t.Fatalf("denied request persisted a child: %d %v", children, err)
	}
}
