package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func metaAuthorizationTestFacts(t *testing.T) authorizationfacts.AuthorizationFacts {
	t.Helper()
	f := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 2, ItemType: "table", FullName: "outdoor.persons", BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
		Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: plugin.TabularItemPath(2, "schema", "outdoor", "persons")}}
	f.ItemFingerprint = models.GenerateItemFingerprint(2, f.FullName)
	hash, err := f.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	f.BoundaryHash = &hash
	return f
}

func TestMetaAuthorizationFactsStrictRuntimeClient(t *testing.T) {
	facts := metaAuthorizationTestFacts(t)
	encoded, _ := json.Marshal(authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{{ItemFingerprint: facts.ItemFingerprint, Facts: &facts}}})
	for _, test := range []struct {
		name    string
		status  int
		body    string
		success bool
	}{
		{"complete", 200, string(encoded), true},
		{"unknown field", 200, strings.Replace(string(encoded), `"layout":"single"`, `"layout":"single","token":"secret"`, 1), false},
		{"trailing JSON", 200, string(encoded) + `{}`, false},
		{"duplicate field", 200, strings.Replace(string(encoded), `"layout":"single"`, `"layout":"single","layout":"single"`, 1), false},
		{"bounded larger envelope", 200, strings.Repeat(" ", 64<<10) + string(encoded), true},
		{"oversized", 200, strings.Repeat(" ", authorizationfacts.MaxAuthorizationFactsBatchBytes) + string(encoded), false},
		{"not found", 404, `{"error":"secret","error_code":"meta_authorization_target_not_found"}`, false},
		{"error", 500, `{"error":"secret"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/meta/runtime/data-items/authorization-facts/query" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Tenant-ID") != "" || r.URL.RawQuery != "" {
					t.Error("unexpected runtime request")
				}
				var query authorizationfacts.AuthorizationFactsQuery
				if json.NewDecoder(r.Body).Decode(&query) != nil || len(query.Fingerprints) != 1 || query.Fingerprints[0] != facts.ItemFingerprint {
					t.Error("unbound owner query")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			value, err := newTestMetaClient(server.URL).ReadAuthorizationFacts(context.Background(), 7, []string{facts.ItemFingerprint})
			if (err == nil) != test.success || calls != 1 {
				t.Fatalf("value=%+v err=%v calls=%d", value, err, calls)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("upstream response leaked")
			}
			if test.status == 404 {
				var apiError *TenantAPIError
				if !errors.As(err, &apiError) || apiError.StatusCode != 404 {
					t.Fatal("missing target confused with transport failure")
				}
			}
		})
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/other", 302) }))
	defer redirect.Close()
	_, err := newTestMetaClient(redirect.URL).ReadAuthorizationFacts(context.Background(), 7, []string{facts.ItemFingerprint})
	var apiError *TenantAPIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 302 {
		t.Fatal("runtime redirect followed", err)
	}
}

func TestMetaFactsQueryDoesNotReplayAfterUnauthorized(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	source := &identityQueryTokenSource{}
	_, err := NewMetaClient(server.URL, source).ReadAuthorizationFacts(t.Context(), 7, []string{strings.Repeat("a", 64)})
	if err == nil || calls != 1 || source.calls != 1 || source.invalidations != 0 {
		t.Fatal("owner query replayed after rejected credential", err)
	}
}
