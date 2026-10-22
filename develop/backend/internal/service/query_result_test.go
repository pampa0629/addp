package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/develop/backend/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/protection"
)

type resultReadGate struct {
	allowDevelopProtectionGate
	reads    int
	expired  bool
	retained *dataprotection.AuthorizedQueryResult
}

func (g *resultReadGate) ReadQueryResult(_ context.Context, tenantID int64, result *dataprotection.AuthorizedQueryResult, subject dataprotection.SubjectReference, _ *dataprotection.QueryRoleSelection) (*dataprotection.AuthorizedQueryReadResult, error) {
	g.reads++
	if tenantID != 7 || subject.Type != "user" || subject.ID != "9" {
		return nil, protection.ErrResultRequired
	}
	if g.retained != nil {
		until := time.Now().Add(time.Hour)
		return &dataprotection.AuthorizedQueryReadResult{QueryResult: g.retained.Result, PlaintextExpiresAt: &until, Retained: g.retained}, nil
	}
	phone := "13812345678"
	var expires *time.Time
	if g.expired {
		phone = "138****5678"
	} else {
		until := time.Now().Add(time.Hour)
		expires = &until
	}
	return &dataprotection.AuthorizedQueryReadResult{QueryResult: plugin.QueryResult{Columns: []string{"phone"}, Rows: []map[string]interface{}{{"phone": phone}}}, PlaintextExpiresAt: expires}, nil
}
func resultFixture() *commonExecution.TaskExecution {
	actor := int64(9)
	return &commonExecution.TaskExecution{ExecutionID: "current", TenantID: 7, Module: "develop", TaskType: "query", Status: "success", Attempt: 1, ActorPrincipalID: &actor, Metadata: commonModels.JSONMap{"result": map[string]interface{}{"result_kind": "table", "columns": []string{"phone"}, "summary": map[string]interface{}{"preview_rows": []map[string]interface{}{{"phone": "138****5678"}}}}}}
}
func authorizedResultFixture() *dataprotection.AuthorizedQueryResult {
	return &dataprotection.AuthorizedQueryResult{TenantID: 7, Subject: dataprotection.SubjectReference{Type: "user", ID: "9"}, Result: plugin.QueryResult{Rows: []map[string]interface{}{{"phone": "13812345678"}}}}
}
func TestCurrentQueryResultNeverChangesPersistedOrHistoricalPreview(t *testing.T) {
	gate := &resultReadGate{}
	cache := newQueryResultCache()
	executor := &DevExecutor{sqlEngine: &SQLEngineService{protectionGate: gate}, queryResults: cache}
	if err := cache.put("current", 1, "generation", authorizedResultFixture(), nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.remove("current", "generation") })
	ctx := WithExecutionAuthContext(t.Context(), authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute"}))
	item := resultFixture()
	for _, expired := range []bool{false, true} {
		gate.expired = expired
		response, err := executor.queryResultResponse(ctx, professionalExecutionResponse(item), 7, "")
		if err != nil {
			t.Fatal(err)
		}
		encoded := string(mustJSON(t, response))
		if strings.Contains(encoded, "13812345678") == expired {
			t.Fatal("current result failed authorization recheck")
		}
		if strings.Contains(encoded, "plaintext_expires_at") == expired {
			t.Fatal("deadline missing or exposed after expiry")
		}
		if strings.Contains(string(mustJSON(t, professionalExecutionResponse(item))), "13812345678") {
			t.Fatal("historical preview contains plaintext")
		}
		if strings.Contains(string(mustJSON(t, item)), "13812345678") {
			t.Fatal("execution facts mutated by current result delivery")
		}
	}
	gate.expired = false
	responseAfterRenewal, err := executor.queryResultResponse(ctx, professionalExecutionResponse(item), 7, "")
	if err != nil || strings.Contains(string(mustJSON(t, responseAfterRenewal)), "13812345678") {
		t.Fatal("a renewed grant restored an expired result without a new query")
	}
	if gate.reads != 2 {
		t.Fatal("current result not rechecked")
	}
	if _, err := executor.queryResultResponse(t.Context(), professionalExecutionResponse(item), 7, ""); err == nil {
		t.Fatal("unverified caller accepted")
	}
	other := WithExecutionAuthContext(t.Context(), authtest.NewTenantUserAuthContext("7", "10", []string{"develop.task.read", "develop.data_read.execute"}))
	response, err := executor.queryResultResponse(other, professionalExecutionResponse(item), 7, "")
	if err != nil || strings.Contains(string(mustJSON(t, response)), "13812345678") {
		t.Fatal("other subject received current plaintext")
	}
	// Restart or expiry never reconstructs raw results from the persisted execution.
	executor.queryResults = newQueryResultCache()
	response, err = executor.queryResultResponse(ctx, professionalExecutionResponse(item), 7, "")
	if err != nil || strings.Contains(string(mustJSON(t, response)), "13812345678") {
		t.Fatal("restart restored plaintext")
	}
}
func TestCurrentQueryCacheIsolationExpiryBudgetAndFencing(t *testing.T) {
	cache := newQueryResultCache()
	subject := dataprotection.SubjectReference{Type: "user", ID: "9"}
	if err := cache.put("id", 1, "old", authorizedResultFixture(), nil); err != nil {
		t.Fatal(err)
	}
	value, ok := cache.get("id", 1, 7, subject)
	if !ok {
		t.Fatal("result missing")
	}
	if time.Until(value.expires) > queryResultRetention {
		t.Fatal("retention unbounded")
	}
	for _, test := range []struct {
		attempt int
		tenant  int64
		subject dataprotection.SubjectReference
	}{{2, 7, subject}, {1, 8, subject}, {1, 7, dataprotection.SubjectReference{Type: "user", ID: "10"}}} {
		if _, ok := cache.get("id", test.attempt, test.tenant, test.subject); ok {
			t.Fatal("identity mismatch accepted")
		}
	}
	if err := cache.put("id", 2, "new", authorizedResultFixture(), nil); err != nil {
		t.Fatal(err)
	}
	cache.remove("id", "old")
	if _, ok := cache.get("id", 2, 7, subject); !ok {
		t.Fatal("stale lease removed newer result")
	}
	cache.mu.Lock()
	v := cache.entries["id"]
	v.expires = time.Now().Add(-time.Second)
	cache.entries["id"] = v
	cache.mu.Unlock()
	if _, ok := cache.get("id", 2, 7, subject); ok || cache.bytes != 0 {
		t.Fatal("expired result retained")
	}
	deadline := time.Now().UTC().Add(time.Minute)
	if err := cache.put("limited", 1, "limited", authorizedResultFixture(), &deadline); err != nil {
		t.Fatal(err)
	}
	limited, ok := cache.get("limited", 1, 7, subject)
	if !ok || !limited.expires.Equal(deadline) {
		t.Fatal("initial authorization deadline was not captured")
	}
	cache.remove("limited", "limited")
	huge := authorizedResultFixture()
	huge.Result.Rows[0]["phone"] = strings.Repeat("x", queryResultMaxEntryBytes)
	if err := cache.put("huge", 1, "huge", huge, nil); err == nil || cache.bytes != 0 {
		t.Fatal("oversized result accepted")
	}
	for i := 0; i <= queryResultMaxEntries; i++ {
		id := strings.Repeat("x", i+1)
		if err := cache.put(id, 1, id, authorizedResultFixture(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) != queryResultMaxEntries || cache.bytes > queryResultMaxBytes {
		t.Fatal("cache exceeds budget")
	}
	if _, ok := cache.get("x", 1, 7, subject); ok {
		t.Fatal("oldest result not evicted")
	}
	cache.mu.Lock()
	for id := range cache.entries {
		cache.removeLocked(id)
	}
	cache.mu.Unlock()
}
func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type choiceResultReadGate struct{ resultReadGate }

func (g *choiceResultReadGate) ReadQueryResult(_ context.Context, _ int64, snapshot *dataprotection.AuthorizedQueryResult, _ dataprotection.SubjectReference, roles *dataprotection.QueryRoleSelection) (*dataprotection.AuthorizedQueryReadResult, error) {
	if roles == nil || len(roles.RoleKeys) == 0 || roles.RoleKeys[0] != "custom.analytics" {
		return nil, dataprotection.ErrRoleOptionIneligible
	}
	return &dataprotection.AuthorizedQueryReadResult{QueryResult: snapshot.Result, Retained: snapshot}, nil
}
func TestSelectedQueryDeliveryRechecksCurrentRolesDoesNotExtendCacheAndRequiresRequeryAfterEviction(t *testing.T) {
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute"})
	roles := []string{"custom.analytics"}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/system/auth/subject-roles" || r.Header.Get("Authorization") != "Bearer addp_at_current" {
			t.Error("live result used a different identity")
		}
		json.NewEncoder(w).Encode(authorization.CurrentSubjectRoles{TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, AuthorizationVersion: facts.Authorization.AuthorizationVersion, RoleKeys: roles, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)})
	}))
	defer server.Close()
	executor := &DevExecutor{queryResults: newQueryResultCache(), sqlEngine: &SQLEngineService{systemService: commonClient.NewSystemServiceClient(server.URL, nil, server.Client()), protectionGate: &choiceResultReadGate{}}}
	snapshot := authorizedResultFixture()
	snapshot.Result.Rows[0]["phone"] = "selected-mask"
	snapshot.RoleChoices = []dataprotection.FieldRoleChoice{{Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"}, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: "policy", AssessmentRevision: 1, PolicyRevision: 1, OptionID: "hash"}}}
	if err := executor.queryResults.put("current", 1, "generation", snapshot, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { executor.queryResults.remove("current", "generation") })
	original, _ := executor.queryResults.get("current", 1, 7, snapshot.Subject)
	response := func() *models.ExecutionWithDevTask {
		item := professionalExecutionResponse(resultFixture())
		result, _ := mapValue(item.Metadata["result"])
		result["role_choice_delivery"] = true
		return item
	}
	ctx := WithExecutionAuthContext(t.Context(), facts)
	for range 2 {
		out, err := executor.queryResultResponse(ctx, response(), 7, "addp_at_current")
		if err != nil || !strings.Contains(string(mustJSON(t, out)), "selected-mask") {
			t.Fatal("current selected result missing", err)
		}
	}
	current, _ := executor.queryResults.get("current", 1, 7, snapshot.Subject)
	if !current.expires.Equal(original.expires) {
		t.Fatal("reading a choice extended retention")
	}
	roles = []string{"custom.reader"}
	if _, err := executor.queryResultResponse(ctx, response(), 7, "addp_at_current"); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatal("withdrawn role did not reject delivery", err)
	}
	executor.queryResults.remove("current", "generation")
	if _, err := executor.queryResultResponse(ctx, response(), 7, "addp_at_current"); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatal("missing choice cache silently returned a different mask", err)
	}
	if calls != 3 {
		t.Fatal("current roles were cached or a query was rerun")
	}
}

func TestQueryCacheReplacementCannotResurrectAnEarlierObservation(t *testing.T) {
	cache := newQueryResultCache()
	raw := authorizedResultFixture()
	if err := cache.put("cas", 1, "lease", raw, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.remove("cas", "lease") })
	original, ok := cache.get("cas", 1, 7, raw.Subject)
	if !ok {
		t.Fatal("snapshot missing")
	}
	masked := *raw
	masked.Result = *mustCloneQueryResult(t, &raw.Result)
	masked.Result.Rows[0]["phone"] = "default-mask"
	if err := cache.replace("cas", original, &masked, &original.expires); err != nil {
		t.Fatal(err)
	}
	if err := cache.replace("cas", original, raw, &original.expires); err == nil {
		t.Fatal("an old observation restored a removed original")
	}
	current, ok := cache.get("cas", 1, 7, raw.Subject)
	if !ok || current.value.Result.Rows[0]["phone"] != "default-mask" || current.revision == original.revision || !current.expires.Equal(original.expires) {
		t.Fatal("replacement lost fencing or retention")
	}
}
func mustCloneQueryResult(t *testing.T, result *plugin.QueryResult) *plugin.QueryResult {
	t.Helper()
	copy, err := dataprotection.CloneQueryResult(result)
	if err != nil {
		t.Fatal(err)
	}
	return copy
}
