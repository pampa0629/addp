package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/google/uuid"
)

func TestSystemInitialProductionStrictTransport(t *testing.T) {
	id := uuid.New()
	result := authorization.InitialProductionResult{SchemaVersion: authorization.InitialProductionResultSchema, TenantID: 7, ProducerOwner: "transfer", ExecutionID: id, Outcome: "denied", RecordedAt: time.Now().UTC(), DenialCode: "target_history_exists"}
	encoded, _ := json.Marshal(result)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"terminal denial", string(encoded), 200, true},
		{"cross tenant", strings.Replace(string(encoded), `"tenant_id":"7"`, `"tenant_id":"8"`, 1), 200, false},
		{"wrong execution", strings.Replace(string(encoded), id.String(), uuid.NewString(), 1), 200, false},
		{"wrong owner", strings.Replace(string(encoded), `"transfer"`, `"develop"`, 1), 200, false},
		{"unknown field", strings.TrimSuffix(string(encoded), "}") + `,"token":"secret"}`, 200, false},
		{"no result", `null`, 200, false},
		{"pending not terminal", strings.Replace(string(encoded), `"denied"`, `"pending"`, 1), 200, false},
		{"trailing", string(encoded) + `{}`, 200, false},
		{"oversized", strings.Repeat(" ", 64<<10) + string(encoded), 200, false},
		{"forbidden", `{"error":"secret","error_code":"permission_denied"}`, 403, false},
		{"pending", `{"error":"secret","error_code":"initial_production_pending"}`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/runtime/initial_production_grants/transfer/"+id.String() || r.URL.RawQuery != "" || len(body) != 0 || r.Header.Get("Authorization") != "Bearer tenant-transfer" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("producer supplied identity or evidence in transport")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("tenant-transfer"), server.Client())
			if _, err := client.IssueInitialTransferGrant(t.Context(), id.String()); err == nil || calls != 0 {
				t.Fatal("missing Tenant reached network")
			}
			value, err := client.WithTenantID(7).IssueInitialTransferGrant(t.Context(), id.String())
			if (err == nil) != tc.ok || err != nil && strings.Contains(err.Error(), "secret") || calls != 1 {
				t.Fatalf("value=%+v error=%v calls=%d", value, err, calls)
			}
		})
	}
}

func TestSystemInitialProductionLookupNeverTreatsErrorsAsMiss(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		ok     bool
	}{
		{`{"found":false}`, 200, true}, {`{}`, 200, false}, {`null`, 200, false}, {`{"found":true}`, 200, false},
		{`{"found":false}`, 404, false}, {`{"found":false}`, 503, false},
	} {
		t.Run(tc.body+http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("lookup wrote")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			lookup, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("tenant-transfer"), server.Client()).WithTenantID(7).ResolveInitialTransferGrant(t.Context(), uuid.NewString())
			if (err == nil) != tc.ok {
				t.Fatalf("lookup=%+v error=%v", lookup, err)
			}
		})
	}
}

type productionTokenFixture struct{ invalidated bool }

func (*productionTokenFixture) Token(context.Context, uint) (string, error) {
	return "rejected-token", nil
}
func (*productionTokenFixture) PlatformToken(context.Context) (string, error) {
	return "platform-unused", nil
}
func (f *productionTokenFixture) InvalidateToken(tenant uint, token string) {
	f.invalidated = tenant == 7 && token == "rejected-token"
}

func TestSystemInitialProductionDoesNotRedirectOrRetryMutation(t *testing.T) {
	for _, status := range []int{302, 401} {
		tokens := &productionTokenFixture{}
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Location", "/leak")
			w.WriteHeader(status)
		}))
		_, err := NewSystemServiceClient(server.URL, tokens, server.Client()).WithTenantID(7).IssueInitialTransferGrant(t.Context(), uuid.NewString())
		server.Close()
		if err == nil || calls != 1 || tokens.invalidated != (status == 401) {
			t.Fatalf("status=%d calls=%d invalidated=%v error=%v", status, calls, tokens.invalidated, err)
		}
	}
}
