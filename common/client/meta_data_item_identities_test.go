package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
)

type identityQueryTokenSource struct {
	calls, invalidations int
}

func (s *identityQueryTokenSource) Token(context.Context, uint) (string, error) {
	s.calls++
	return "expired-service-token", nil
}

func (s *identityQueryTokenSource) InvalidateToken(uint, string) { s.invalidations++ }

func TestMetaDataItemIdentityQueryDoesNotReplayAfterUnauthorized(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error_code":"token_expired"}`))
	}))
	defer server.Close()
	source := &identityQueryTokenSource{}
	_, err := NewMetaClient(server.URL, source).QueryDataItemIdentities(context.Background(), 7,
		authorizationfacts.IdentityQuery{EngineID: 2, Page: 1, PageSize: 20})
	var failure *TenantAPIError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusUnauthorized || failure.ErrorCode != "token_expired" {
		t.Fatalf("lost owner rejection: %v", err)
	}
	if calls != 1 || source.calls != 1 || source.invalidations != 0 {
		t.Fatalf("identity POST replayed: calls=%d tokens=%d invalidations=%d", calls, source.calls, source.invalidations)
	}
}

func TestMetaDataItemIdentityClient(t *testing.T) {
	query := authorizationfacts.IdentityQuery{EngineID: 2, Page: 1, PageSize: 20}
	page := authorizationfacts.IdentityPage{Data: []authorizationfacts.DataItemIdentity{{ItemFingerprint: strings.Repeat("a", 64), EngineID: 2, Name: "people", FullName: "outdoor.people", ItemType: "table"}}, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}
	encoded, _ := json.Marshal(page)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		ok     bool
	}{
		{"valid", 200, string(encoded), true},
		{"wrong engine", 200, strings.Replace(string(encoded), `"engine_id":2`, `"engine_id":3`, 1), false},
		{"wrong count", 200, strings.Replace(string(encoded), `"total":1`, `"total":2`, 1), false},
		{"unknown column", 200, strings.Replace(string(encoded), `"name":"people"`, `"name":"people","secret":"must-not-leak"`, 1), false},
		{"duplicate key", 200, strings.Replace(string(encoded), `"total":1`, `"total":1,"total":1`, 1), false},
		{"null data", 200, `{"data":null,"total":0,"page":1,"page_size":20,"total_pages":0}`, false},
		{"trailing", 200, string(encoded) + `{}`, false},
		{"oversize", 200, strings.Repeat(" ", authorizationfacts.MaxIdentityQueryBytes) + string(encoded), false},
		{"failure", 503, `{"error":"must-not-leak"}`, false},
		{"redirect", 302, `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/meta/runtime/data-item-identities/query" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Tenant-ID") != "" || r.URL.RawQuery != "" {
					t.Error("wrong owner request")
				}
				var got authorizationfacts.IdentityQuery
				if json.NewDecoder(r.Body).Decode(&got) != nil || got.Validate() != nil || got.EngineID != query.EngineID {
					t.Error("wrong query body")
				}
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := newTestMetaClient(server.URL).QueryDataItemIdentities(context.Background(), 7, query)
			if (err == nil) != tc.ok || calls != 1 {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
			}
			if err != nil && strings.Contains(err.Error(), "must-not-leak") {
				t.Fatal("upstream body leaked")
			}
		})
	}
}
