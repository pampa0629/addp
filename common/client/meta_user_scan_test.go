package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetaUserScanUsesOnlyCurrentRequestCredential(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		want := []string{"Bearer first-user", "Bearer second-user"}[calls-1]
		if r.Method != "POST" || r.URL.Path != "/api/v1/meta/scan/run/manual" || r.Header.Get("Authorization") != want || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("X-Internal-API-Key") != "" {
			t.Errorf("unexpected scan identity or path")
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil || payload["engine_id"] != float64(9) || payload["source"] != "manager" || payload["scan_depth"] != "deep" || payload["force"] != true {
			t.Errorf("invalid request %s", body)
		}
		for _, forbidden := range []string{"first-user", "second-user", "tenant_id", "user_id", "triggered_by", "actor_principal_id", "catalog_paths"} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("unexpected identity or widened scope %s", forbidden)
			}
		}
		w.WriteHeader(201)
		io.WriteString(w, `{"execution_id":"fef33226-d498-4e22-8aca-4002c42c6014","status":"pending","execution_config":{"private":"not-for-upload-result"}}`)
	}))
	defer server.Close()
	client := NewMetaScanUserClient(server.URL)
	opts := MetaScanOptions{EngineID: 9, RefGroups: []MetaScanRefGroup{{Primary: "bucket/raw/a.parquet"}}, Source: "manager", ScanDepth: "deep", Force: true}
	for _, token := range []string{"first-user", "second-user"} {
		result, err := client.CreateManualScanRun(context.Background(), token, opts)
		if err != nil || result.ExecutionID != "fef33226-d498-4e22-8aca-4002c42c6014" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "execution_config") {
			t.Fatal("internal execution data escaped minimal result")
		}
	}
}

func TestMetaUserScanRejectsDependencyFailureWithoutRetryOrRedirect(t *testing.T) {
	for _, status := range []int{401, 403, 409, 500, 503, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls, redirects := 0, 0
			sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; w.WriteHeader(201) }))
			defer sink.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", sink.URL)
				w.WriteHeader(status)
				io.WriteString(w, "private-user-token-or-connection-info")
			}))
			defer server.Close()
			_, err := NewMetaScanUserClient(server.URL).CreateManualScanRun(context.Background(), "current-user", MetaScanOptions{EngineID: 9})
			var submission *MetaScanSubmissionError
			if !errors.As(err, &submission) || submission.StatusCode != status || calls != 1 || redirects != 0 || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe failure: err=%v calls=%d redirects=%d", err, calls, redirects)
			}
		})
	}
}

func TestMetaUserScanRejectsInvalidInputsAndResponses(t *testing.T) {
	calls := 0
	payload := `{}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(201); io.WriteString(w, payload) }))
	defer server.Close()
	client := NewMetaScanUserClient(server.URL)
	for _, token := range []string{"", "user secret", "user\nsecret"} {
		if _, err := client.CreateManualScanRun(context.Background(), token, MetaScanOptions{}); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached HTTP")
	}
	for _, invalid := range []string{`{}`, `{"execution_id":"not-a-uuid","status":"pending"}`, `{"execution_id":"00000000-0000-0000-0000-000000000000","status":"pending"}`, `{"execution_id":"fef33226-d498-4e22-8aca-4002c42c6014","status":"unknown"}`, `{` + strings.Repeat(" ", 1<<20) + `}`} {
		payload = invalid
		if _, err := client.CreateManualScanRun(context.Background(), "user", MetaScanOptions{EngineID: 9}); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls
	if _, err := client.CreateManualScanRun(ctx, "user", MetaScanOptions{EngineID: 9}); err == nil || calls != before {
		t.Fatal("cancelled request continued")
	}
}

func TestMetaUserRefreshNeverRetriesOrRedirectsCredentials(t *testing.T) {
	for _, status := range []int{401, 403, 409, 500, 503, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls, redirects := 0, 0
			sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++ }))
			defer sink.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer current-user" {
					t.Error("wrong credential")
				}
				w.Header().Set("Location", sink.URL)
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"private-secret","error_code":"scan_scope_active"}`)
			}))
			defer server.Close()
			_, err := NewMetaScanUserClient(server.URL).RefreshItem(t.Context(), "current-user", 8, MetaScanOptions{Encoding: "gbk"})
			got, ok := TenantAPIStatusCode(err)
			if !ok || got != status || calls != 1 || redirects != 0 || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("unsafe refresh error: %v calls=%d redirects=%d", err, calls, redirects)
			}
		})
	}
}

func TestMetaUserRefreshUsesCallCredentialAndContext(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != []string{"Bearer first-user", "Bearer second-user"}[calls-1] {
			t.Error("credential persisted between calls")
		}
		io.WriteString(w, `{"status":"success","items_scanned":1}`)
	}))
	defer server.Close()
	client := NewMetaScanUserClient(server.URL)
	for _, token := range []string{"", "user secret", "user\nsecret"} {
		if _, err := client.RefreshItem(t.Context(), token, 8, MetaScanOptions{}); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid credential reached server")
	}
	for _, token := range []string{"first-user", "second-user"} {
		if _, err := client.RefreshItem(t.Context(), token, 8, MetaScanOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.RefreshItem(ctx, "third-user", 8, MetaScanOptions{}); err == nil || calls != 2 {
		t.Fatal("cancelled refresh continued")
	}
}
