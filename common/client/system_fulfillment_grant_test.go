package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func fulfillmentGrantClientBinding() shared.SharingFulfillmentBinding {
	expires := time.Date(2030, 1, 2, 3, 4, 5, 123456000, time.UTC)
	return shared.SharingFulfillmentBinding{CallerPrincipalID: 9007199254740993,
		Operator: shared.SharingFulfillmentOperator{PrincipalID: 9007199254740995, MembershipID: 5, AuthorizationVersion: 9007199254740997},
		Path:     plugin.TabularItemPath(12, "schema", "原.模式", " 源/表 "), DecisionID: uuid.New(), RequirementVersion: 9007199254740999,
		RecipientType: "project_group", RecipientID: 9007199254741001, Action: "read", ExpiryMode: shared.SharingExpiryAtTime, ExpiresAt: &expires}
}

func TestSystemFulfillmentGrantClientIdentityAndExactHistory(t *testing.T) {
	id := uuid.New()
	binding := fulfillmentGrantClientBinding()
	issued := time.Date(2020, 1, 2, 3, 4, 5, 123456000, time.UTC)
	grant := shared.SharingFulfillmentGrant{RequestID: id, GrantedAt: issued}
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-service-token" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("grant request must use Tenant Service Bearer only")
		}
		var actual shared.SharingFulfillmentBinding
		if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || !reflect.DeepEqual(actual, binding) {
			t.Errorf("original binding changed: %+v %v", actual, err)
		}
		switch r.URL.Path {
		case "/api/v1/system/runtime/engine-access-fulfillments/" + id.String() + "/grant":
			_ = json.NewEncoder(w).Encode(grant)
		case "/api/v1/system/runtime/engine-access-fulfillments/" + id.String() + "/grant/resolve":
			_ = json.NewEncoder(w).Encode(shared.SharingFulfillmentGrantLookup{Found: true, Grant: &grant})
		default:
			t.Errorf("unexpected additional operation: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := NewSystemFulfillmentClient(server.URL, fulfillmentTestTokens{}, server.Client()).WithTenantID(7)
	for i := 0; i < 2; i++ {
		result, err := c.IssueGrant(context.Background(), id, binding)
		if err != nil || result == nil || *result != grant {
			t.Fatalf("same-parameter issuance history=%+v %v", result, err)
		}
	}
	lookup, err := c.ResolveGrant(context.Background(), id, binding)
	if err != nil || lookup == nil || !lookup.Found || lookup.Grant == nil || *lookup.Grant != grant || len(calls) != 3 {
		t.Fatalf("pure history lookup=%+v calls=%v %v", lookup, calls, err)
	}
}

func TestSystemFulfillmentGrantClientRejectsInvalidHistoryAndFailures(t *testing.T) {
	id := uuid.New()
	binding := fulfillmentGrantClientBinding()
	validGrant := `{"request_id":"` + id.String() + `","granted_at":"2020-01-02T03:04:05.123456Z"}`
	wrongGrant := `{"request_id":"` + uuid.NewString() + `","granted_at":"2020-01-02T03:04:05Z"}`
	for _, test := range []struct {
		name, operation, body, code string
		status                      int
		valid                       bool
	}{
		{"issued", "issue", validGrant, "", 200, true},
		{"miss", "resolve", `{"found":false}`, "", 200, true},
		{"found", "resolve", `{"found":true,"grant":` + validGrant + `}`, "", 200, true},
		{"empty_issue", "issue", `{}`, "", 200, false},
		{"wrong_issue_id", "issue", wrongGrant, "", 200, false},
		{"zero_issue_time", "issue", `{"request_id":"` + id.String() + `","granted_at":"0001-01-01T00:00:00Z"}`, "", 200, false},
		{"invalid_time", "issue", `{"request_id":"` + id.String() + `","granted_at":"bad"}`, "", 200, false},
		{"empty_lookup", "resolve", `{}`, "", 200, false},
		{"null_found", "resolve", `{"found":null}`, "", 200, false},
		{"miss_with_null_grant", "resolve", `{"found":false,"grant":null}`, "", 200, false},
		{"miss_with_grant", "resolve", `{"found":false,"grant":` + validGrant + `}`, "", 200, false},
		{"found_without_grant", "resolve", `{"found":true}`, "", 200, false},
		{"found_with_null", "resolve", `{"found":true,"grant":null}`, "", 200, false},
		{"found_wrong_id", "resolve", `{"found":true,"grant":` + wrongGrant + `}`, "", 200, false},
		{"found_empty_grant", "resolve", `{"found":true,"grant":{}}`, "", 200, false},
		{"invalid_json", "resolve", `invalid`, "", 200, false},
		{"no_content_issue", "issue", ``, "", 204, false},
		{"no_content_lookup", "resolve", ``, "", 204, false},
		{"unauthenticated", "resolve", `{}`, "", 401, false},
		{"forbidden", "resolve", `{}`, "", 403, false},
		{"not_found_is_not_miss", "resolve", `{}`, "", 404, false},
		{"window_expired", "issue", `{"error_code":"engine_access_grant_window_expired"}`, "engine_access_grant_window_expired", 409, false},
		{"closed", "issue", `{"error_code":"engine_access_fulfillment_closed"}`, "engine_access_fulfillment_closed", 409, false},
		{"binding_conflict", "resolve", `{"error_code":"engine_access_fulfillment_binding_conflict"}`, "engine_access_fulfillment_binding_conflict", 409, false},
		{"dependency_error", "resolve", `{}`, "", 503, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			c := NewSystemFulfillmentClient(server.URL, fulfillmentTestTokens{}, server.Client()).WithTenantID(7)
			var err error
			if test.operation == "issue" {
				result, e := c.IssueGrant(context.Background(), id, binding)
				err = e
				if !test.valid && result != nil {
					t.Fatal("invalid response exposed as issuance history")
				}
			} else {
				result, e := c.ResolveGrant(context.Background(), id, binding)
				err = e
				if !test.valid && result != nil {
					t.Fatal("failure exposed as a missing or valid Grant")
				}
			}
			if (err == nil) != test.valid || calls != 1 {
				t.Fatalf("valid=%v calls=%d err=%v", test.valid, calls, err)
			}
			if test.status >= 400 {
				if status, ok := TenantAPIStatusCode(err); !ok || status != test.status {
					t.Fatalf("lost HTTP status: %v", err)
				}
			}
			if test.code != "" {
				if code, ok := TenantAPIErrorCode(err); !ok || code != test.code {
					t.Fatalf("lost stable error code: %v", err)
				}
			}
		})
	}
}

func TestSystemFulfillmentGrantClientRejectsInputBeforeSending(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
	}))
	defer server.Close()
	c := NewSystemFulfillmentClient(server.URL, fulfillmentTestTokens{}, server.Client()).WithTenantID(7)
	binding := fulfillmentGrantClientBinding()
	invalid := binding
	invalid.RecipientID = 0
	for _, input := range []struct {
		id      uuid.UUID
		binding shared.SharingFulfillmentBinding
	}{{uuid.Nil, binding}, {uuid.New(), invalid}} {
		if result, err := c.IssueGrant(context.Background(), input.id, input.binding); result != nil || err == nil {
			t.Fatalf("invalid issuance accepted: %+v %v", result, err)
		}
		if result, err := c.ResolveGrant(context.Background(), input.id, input.binding); result != nil || err == nil {
			t.Fatalf("invalid lookup accepted: %+v %v", result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := c.ResolveGrant(ctx, uuid.New(), binding); result != nil || err == nil {
		t.Fatalf("cancellation treated as a miss: %+v %v", result, err)
	}
	if calls != 0 {
		t.Fatalf("invalid input/cancellation sent %d requests", calls)
	}
}
