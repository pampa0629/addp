package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	authmiddleware "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestOntologyRuleFactRejectionPrecedesServiceLookup(t *testing.T) {
	call := authorization.OntologyRuleCall{AgentRunID: "run", ToolCallID: "call", PrincipalID: "3",
		Evaluation: authorization.OntologyRuleEvaluation{OntologyID: "outdoor", RuleID: "valid_activity", Revision: 2,
			Generation: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", ActivationVersion: 3, ObjectID: "test-object"}}
	for _, status := range []int{http.StatusForbidden, http.StatusConflict} {
		system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var got authorization.OntologyRuleCall
			if r.URL.Path != "/api/v1/system/runtime/ontology-rule-evaluations/consume" || r.Header.Get("Authorization") != "Bearer addp_at_owner" ||
				json.NewDecoder(r.Body).Decode(&got) != nil || got != call {
				t.Error("Service lost the exact rule claim or machine identity")
			}
			w.WriteHeader(status)
		}))
		h := ontologyRuleFactsHandler{system: client.NewSystemServiceClient(system.URL, apiRoleTestTokens{}, system.Client())}
		router := gin.New()
		router.POST("/facts/:service_name", func(c *gin.Context) {
			if err := authmiddleware.SetAuthContextForGin(c, testTenantUserAuthContext(t, 7)); err != nil {
				t.Fatal(err)
			}
		}, h.ReadOntologyRuleFacts)
		body, err := json.Marshal(authorization.OntologyRuleFactRequest{Call: call, ServiceID: 16,
			ServiceVersion: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IDParameter:    "activity_id", Fields: []string{"activity_id", "activity_date"}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/facts/private-activity", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		system.Close()
		if response.Code != http.StatusForbidden {
			t.Fatalf("upstream %d: returned %d, want rejection before service lookup", status, response.Code)
		}
	}
}
