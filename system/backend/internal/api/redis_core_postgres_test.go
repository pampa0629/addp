package api

import (
	"github.com/addp/common/infra/redistest"
	"gorm.io/gorm"
	"net"
	"net/http/httptest"
	"testing"
)

// Runs inside the registered OAuth fixture and its disposable PostgreSQL lock.
func testSystemRedisBoundaryAgainstPostgres(t *testing.T, db *gorm.DB, runtime *IAMRuntime, secret string, tenantID int64) {
	cfg := redistest.Config(t)
	proxy := redistest.NewProxy(t, net.JoinHostPort(cfg.Host, cfg.Port))
	client := redistest.Open(t, proxy.Config(cfg))
	router := SetupRouter(db, testIAMRuntimeConfig(), client)
	token := decodeIAMAccessToken(t, performIAMOAuthClientCredentialsRequest(t, router, secret, tenantID))
	count := func(table string) int64 {
		t.Helper()
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	beforeTokens, beforeAssignments, beforeGrants := count("system.access_tokens"), count("system.role_assignments"), count("system.engine_access_grants")
	get := func(path string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	get("/health/ready", 200)
	proxy.SetAvailable(false)
	get("/health/live", 200)
	get("/health/ready", 503)
	failed := performIAMOAuthClientCredentialsRequest(t, router, secret, tenantID)
	if failed.Code != 503 {
		t.Fatal("unavailable System issued token", failed.Code)
	}
	if count("system.access_tokens") != beforeTokens || count("system.role_assignments") != beforeAssignments || count("system.engine_access_grants") != beforeGrants {
		t.Fatal("Redis outage changed authoritative grants/tokens")
	}
	// An outage does not revoke the pre-existing database-backed Token.
	if _, err := runtime.AuthContextService.ResolveAccessToken(t.Context(), token); err != nil {
		t.Fatal("Redis outage revoked existing Token", err)
	}
	proxy.SetAvailable(true)
	get("/health/ready", 200)
	if _, err := runtime.AuthContextService.ResolveAccessToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	decodeIAMAccessToken(t, performIAMOAuthClientCredentialsRequest(t, router, secret, tenantID))
	t.Log("System real Redis/PG: no Token/Grant/Assignment side effect on outage; same Token valid after recovery")
}
