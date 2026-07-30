package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testServiceClientSecret = "0123456789abcdef0123456789abcdef"

func TestOAuthServiceTokenSourceUsesClientCredentialsAndCachesByTenant(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestsByTenant := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/oauth/token" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		clientID, secret, ok := r.BasicAuth()
		if !ok || clientID != "addp-manager" || secret != testServiceClientSecret {
			t.Fatalf("basic auth = client:%q secret-match:%t ok:%t", clientID, secret == testServiceClientSecret, ok)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("audience") != "addp.api" || r.Form.Get("scope") != "addp.api" {
			t.Fatalf("oauth form = %#v", r.Form)
		}
		tenantID := r.Form.Get("tenant_id")
		mu.Lock()
		requestsByTenant[tenantID]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "addp_at_tenant_" + tenantID,
			"token_type":   "bearer",
			"expires_in":   300,
			"scope":        "addp.api",
		})
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-manager", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatalf("NewOAuthServiceTokenSource() error = %v", err)
	}
	for _, tenantID := range []uint{7, 7, 11, 11} {
		got, err := source.Token(context.Background(), tenantID)
		if err != nil {
			t.Fatalf("Token(%d) error = %v", tenantID, err)
		}
		if want := fmt.Sprintf("addp_at_tenant_%d", tenantID); got != want {
			t.Fatalf("Token(%d) = %q, want %q", tenantID, got, want)
		}
	}
	if requestsByTenant["7"] != 1 || requestsByTenant["11"] != 1 || len(requestsByTenant) != 2 {
		t.Fatalf("token requests by tenant = %#v", requestsByTenant)
	}
}

func TestOAuthServiceTokenSourceUsesExplicitPlatformContextAndCachesSeparately(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		contextKey := r.Form.Get("context_type") + ":" + r.Form.Get("tenant_id")
		mu.Lock()
		requests[contextKey]++
		mu.Unlock()
		if r.Form.Get("context_type") == "platform" && r.Form.Get("tenant_id") != "" {
			t.Fatal("platform token request included tenant_id")
		}
		_, _ = w.Write([]byte(`{"access_token":"addp_at_service_token","token_type":"bearer","expires_in":300,"scope":"addp.api"}`))
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-meta", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatalf("NewOAuthServiceTokenSource() error = %v", err)
	}
	if _, err := source.PlatformToken(context.Background()); err != nil {
		t.Fatalf("PlatformToken() error = %v", err)
	}
	if _, err := source.PlatformToken(context.Background()); err != nil {
		t.Fatalf("cached PlatformToken() error = %v", err)
	}
	if _, err := source.Token(context.Background(), 7); err != nil {
		t.Fatalf("Token(7) error = %v", err)
	}
	if requests["platform:"] != 1 || requests[":7"] != 1 || len(requests) != 2 {
		t.Fatalf("token requests = %#v", requests)
	}
}

func TestOAuthServiceTokenSourceInvalidatesOnlyTheRejectedCachedToken(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokenRequests++
		sequence := tokenRequests
		mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"access_token":"addp_at_token_%d","token_type":"bearer","expires_in":300,"scope":"addp.api"}`, sequence)
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-manager", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	first, err := source.Token(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	source.InvalidateToken(7, "addp_at_an_older_token")
	stillCached, err := source.Token(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if stillCached != first || tokenRequests != 1 {
		t.Fatalf("non-matching invalidation evicted token: token=%q requests=%d", stillCached, tokenRequests)
	}
	source.InvalidateToken(7, first)
	refreshed, err := source.Token(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed == first || tokenRequests != 2 {
		t.Fatalf("matching invalidation did not refresh token: first=%q refreshed=%q requests=%d", first, refreshed, tokenRequests)
	}
}

func TestMetaClientWithServiceTokenSendsOnlyBearerToMeta(t *testing.T) {
	t.Parallel()

	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token form: %v", err)
		}
		if got := r.Form.Get("tenant_id"); got != "7" {
			t.Fatalf("token tenant_id = %q, want 7", got)
		}
		_, _ = w.Write([]byte(`{"access_token":"addp_at_service_token","token_type":"bearer","expires_in":300,"scope":"addp.api"}`))
	}))
	defer systemServer.Close()

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_service_token" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("Meta request must not send legacy internal authentication headers")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 12})
	}))
	defer metaServer.Close()

	source, err := NewOAuthServiceTokenSource(systemServer.URL, "addp-manager", testServiceClientSecret, systemServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthServiceTokenSource() error = %v", err)
	}
	metaClient := NewMetaClient(metaServer.URL, source).WithTenantID(7)
	if _, err := metaClient.GetItemByID(12); err != nil {
		t.Fatalf("GetItemByID() error = %v", err)
	}
}

func TestMetaClientRefreshesRejectedServiceTokenOnce(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	tokenRequests := 0
	metaRequests := 0
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokenRequests++
		sequence := tokenRequests
		mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"access_token":"addp_at_service_%d","token_type":"bearer","expires_in":300,"scope":"addp.api"}`, sequence)
	}))
	defer systemServer.Close()

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		metaRequests++
		mu.Unlock()
		switch r.Header.Get("Authorization") {
		case "Bearer addp_at_service_1":
			http.Error(w, "authorization version changed", http.StatusUnauthorized)
		case "Bearer addp_at_service_2":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 12})
		default:
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
	}))
	defer metaServer.Close()

	source, err := NewOAuthServiceTokenSource(systemServer.URL, "addp-manager", testServiceClientSecret, systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	metaClient := NewMetaClient(metaServer.URL, source).WithTenantID(7)
	item, err := metaClient.GetItemByID(12)
	if err != nil {
		t.Fatalf("GetItemByID() error = %v", err)
	}
	if item.ID != 12 || tokenRequests != 2 || metaRequests != 2 {
		t.Fatalf("item=%#v token requests=%d Meta requests=%d", item, tokenRequests, metaRequests)
	}
}

func TestMetaClientReplaysRequestBodyAfterServiceTokenRefresh(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	tokenRequests := 0
	requestBodies := make([]string, 0, 2)
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokenRequests++
		sequence := tokenRequests
		mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"access_token":"addp_at_service_%d","token_type":"bearer","expires_in":300,"scope":"addp.api"}`, sequence)
	}))
	defer systemServer.Close()

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		requestBodies = append(requestBodies, string(body))
		sequence := len(requestBodies)
		mu.Unlock()
		if sequence == 1 {
			http.Error(w, "authorization version changed", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed"})
	}))
	defer metaServer.Close()

	source, err := NewOAuthServiceTokenSource(systemServer.URL, "addp-manager", testServiceClientSecret, systemServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	metaClient := NewMetaClient(metaServer.URL, source).WithTenantID(7)
	result, err := metaClient.RefreshItem(12, MetaScanOptions{EngineID: 5, Force: true})
	if err != nil {
		t.Fatalf("RefreshItem() error = %v", err)
	}
	if result.Status != "completed" || len(requestBodies) != 2 || requestBodies[0] == "" || requestBodies[0] != requestBodies[1] {
		t.Fatalf("result=%#v request bodies=%q", result, requestBodies)
	}
}

func TestOAuthServiceTokenSourceRejectsErrorWithoutLeakingSecret(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid client "+testServiceClientSecret, http.StatusUnauthorized)
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-manager", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatalf("NewOAuthServiceTokenSource() error = %v", err)
	}
	_, err = source.Token(context.Background(), 7)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("Token() error = %v", err)
	}
	if strings.Contains(err.Error(), testServiceClientSecret) {
		t.Fatal("Token() error leaked the client secret")
	}
}

func TestOAuthServiceTokenSourceRejectsUnexpectedScope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"addp_at_wrong_scope","token_type":"bearer","expires_in":300,"scope":"openid"}`))
	}))
	defer server.Close()

	source, err := NewOAuthServiceTokenSource(server.URL, "addp-manager", testServiceClientSecret, server.Client())
	if err != nil {
		t.Fatalf("NewOAuthServiceTokenSource() error = %v", err)
	}
	if _, err := source.Token(context.Background(), 7); err == nil || !strings.Contains(err.Error(), "invalid token response") {
		t.Fatalf("Token() error = %v", err)
	}
}
