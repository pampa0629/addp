package frontendprobe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFrontendProbeDistinguishesEntryAndScript(t *testing.T) {
	for _, tc := range []struct {
		name, entry, script, assetType string
		wantEntry, wantAsset           bool
		assetRequested                 bool
	}{
		{"production build", "/module-ui/monitor/", "/module-ui/monitor/assets/index-A1.js", "application/javascript", true, true, true},
		{"vite", "/", "/src/main.js", "text/javascript; charset=utf-8", true, true, true},
		{"vite HMR timestamp", "/module-ui/monitor/", "/module-ui/monitor/src/main.js?t=1795070243878", "text/javascript", true, true, true},
		{"HTML fallback", "/", "/assets/index-A1.js", "text/html", true, false, true},
		{"other origin rejected", "/", "https://example.invalid/src/main.js", "text/javascript", true, false, false},
		{"unrelated module rejected", "/module-ui/monitor/", "/module-ui/system/assets/index-A1.js", "text/javascript", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.Path
				if r.URL.Path == tc.entry {
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(`<div id="app"></div><script type="module" src="` + tc.script + `"></script>`))
					return
				}
				w.Header().Set("Content-Type", tc.assetType)
				_, _ = w.Write([]byte("export default 1"))
			}))
			defer server.Close()
			origin, _ := url.Parse(server.URL)
			got := New(origin).Check(context.Background(), tc.entry)
			if got.Entry != tc.wantEntry || got.Asset == nil || *got.Asset != tc.wantAsset || (len(requests) == 2) != tc.assetRequested {
				t.Fatalf("unexpected result %+v, requests %d", got, len(requests))
			}
		})
	}
}

func TestFailedEntryNeverPretendsToHaveAnAssetOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	origin, _ := url.Parse(server.URL)
	got := New(origin).Check(context.Background(), "/")
	if got.Entry || got.Asset != nil {
		t.Fatal(got)
	}
}
