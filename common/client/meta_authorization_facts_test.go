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
			var observed []MetaAuthorizationFactsTiming
			ctx := WithMetaAuthorizationFactsTiming(t.Context(), func(value MetaAuthorizationFactsTiming) { observed = append(observed, value) })
			value, err := newTestMetaClient(server.URL).ReadAuthorizationFacts(ctx, 7, []string{facts.ItemFingerprint})
			if (err == nil) != test.success || calls != 1 {
				t.Fatalf("value=%+v err=%v calls=%d", value, err, calls)
			}
			if test.success {
				if len(observed) != 1 {
					t.Fatalf("successful query observations: %d", len(observed))
				}
				timing := observed[0]
				var sum time.Duration
				for _, stage := range []time.Duration{timing.Preparation, timing.HTTP, timing.ResponseRead, timing.StrictJSON, timing.Decode, timing.Validation} {
					if stage < 0 {
						t.Fatal("negative client timing")
					}
					sum += stage
				}
				if timing.Total < sum || sum <= 0 || timing.HTTP <= 0 || timing.StrictJSON <= 0 || timing.Validation <= 0 {
					t.Fatalf("invalid non-overlapping timing boundaries: %+v", timing)
				}
			} else if len(observed) != 0 {
				t.Fatal("failed owner response reported a successful observation")
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

func TestMetaFactsQueryTimingIsContextLocal(t *testing.T) {
	facts := metaAuthorizationTestFacts(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{{ItemFingerprint: facts.ItemFingerprint, Facts: &facts}}})
	}))
	defer server.Close()
	client := newTestMetaClient(server.URL)
	counts := [2]int{}
	contexts := []context.Context{
		WithMetaAuthorizationFactsTiming(t.Context(), func(MetaAuthorizationFactsTiming) { counts[0]++ }),
		t.Context(),
		WithMetaAuthorizationFactsTiming(t.Context(), func(MetaAuthorizationFactsTiming) { counts[1]++ }),
	}
	for _, ctx := range contexts {
		value, err := client.ReadAuthorizationFacts(ctx, 7, []string{facts.ItemFingerprint})
		if err != nil || len(value.Data) != 1 || value.Data[0].ItemFingerprint != facts.ItemFingerprint {
			t.Fatalf("timing changed owner response: %+v %v", value, err)
		}
	}
	if counts != [2]int{1, 1} {
		t.Fatalf("client timing leaked across contexts: %v", counts)
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
