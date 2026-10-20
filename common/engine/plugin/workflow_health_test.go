package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func healthConnection(t *testing.T, endpoint string) ConnectionInfo {
	t.Helper()
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return ConnectionInfo{"protocol": u.Scheme, "host": u.Hostname(), "port": port}
}

func TestWorkflowHealthContract(t *testing.T) {
	const healthy = `{"status":"healthy","service":"test","version":"1"}`
	for _, test := range []struct {
		name string
		code int
		body string
		ok   bool
	}{
		{"minimal", 200, healthy, true},
		{"optional unavailable", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"optional":{"available":false,"required":false}}}`, true},
		{"required available", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":{"available":true,"required":true,"version":"2","details":"bound"}}}`, true},
		{"empty body", 200, "", false},
		{"invalid JSON", 200, "<html>private response</html>", false},
		{"missing identity", 200, `{"status":"healthy"}`, false},
		{"missing status", 200, `{"service":"test","version":"1"}`, false},
		{"degraded removed", 200, `{"status":"degraded","service":"test","version":"1"}`, false},
		{"unhealthy", 503, `{"status":"unhealthy","service":"test","version":"1"}`, false},
		{"200 unhealthy", 200, `{"status":"unhealthy","service":"test","version":"1"}`, false},
		{"503 healthy", 503, healthy, false},
		{"redirect", 302, healthy, false},
		{"required unavailable", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":{"available":false,"required":true}}}`, false},
		{"version strings removed", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":"2"}}`, false},
		{"null dependencies", 200, `{"status":"healthy","service":"test","version":"1","dependencies":null}`, false},
		{"missing required false", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":{"available":false}}}`, false},
		{"missing available", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":{"required":false}}}`, false},
		{"nonboolean availability", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":{"required":true,"available":"true"}}}`, false},
		{"unknown dependency field", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":{"required":true,"available":true,"path":"/private/path"}}}`, false},
		{"healthy with check error", 200, `{"status":"healthy","service":"test","version":"1","error_code":"health_check_failed"}`, false},
		{"negative uptime", 200, `{"status":"healthy","service":"test","version":"1","uptime":-1}`, false},
		{"null uptime", 200, `{"status":"healthy","service":"test","version":"1","uptime":null}`, false},
		{"null version fact", 200, `{"status":"healthy","service":"test","version":"1","dependencies":{"sdk":{"available":true,"required":true,"version":null}}}`, false},
		{"empty error code", 200, `{"status":"healthy","service":"test","version":"1","error_code":""}`, false},
		{"trailing JSON", 200, healthy + `{}`, false},
		{"oversized", 200, healthy + strings.Repeat(" ", 64*1024), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				w.WriteHeader(test.code)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			provider := NewHTTPWorkflowRuntimeProvider("test", "Test")
			err := provider.TestConnection(context.Background(), healthConnection(t, server.URL))
			if (err == nil) != test.ok {
				t.Fatalf("TestConnection() = %v, success = %v", err, test.ok)
			}
			if err != nil && (strings.Contains(err.Error(), "private response") || strings.Contains(err.Error(), "/private/path")) {
				t.Fatal("probe error leaked remote response")
			}
		})
	}
}

func TestWorkflowHealthPreservesCancellationAndRejectsRedirect(t *testing.T) {
	redirected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.Redirect(w, r, "/other", http.StatusFound)
			return
		}
		redirected = true
	}))
	defer server.Close()
	if _, err := HTTPWorkflowHealth(context.Background(), healthConnection(t, server.URL)); err == nil || redirected {
		t.Fatalf("redirect accepted: err=%v redirected=%v", err, redirected)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if _, err := HTTPWorkflowHealth(ctx, healthConnection(t, server.URL)); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
