package supermap_workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestSuperMapWorkflowTestConnectionRequiresObjectsJava(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "degraded",
			"dependencies": {
				"objectsjava": {"available": false, "path": "/opt/supermap/objectsjava/bin_linux_arm64"},
				"gpa_libs": {"available": true, "path": "/opt/supermap/gpa/libs"}
			}
		}`))
	}))
	defer server.Close()

	err := (&SuperMapWorkflowPlugin{}).TestConnection(context.Background(), connInfoForTestServer(t, server.URL))
	if err == nil {
		t.Fatal("TestConnection succeeded without objectsjava binding, want error")
	}
	if !strings.Contains(err.Error(), "objectsjava runtime is not bound") {
		t.Fatalf("error = %q, want objectsjava binding failure", err)
	}
}

func TestSuperMapWorkflowTestConnectionRequiresGPALibs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "degraded",
			"dependencies": {
				"objectsjava": {"available": true, "path": "/opt/supermap/objectsjava/bin_linux_arm64"},
				"gpa_libs": {"available": false, "details": "missing gpa-sps-core jar"}
			}
		}`))
	}))
	defer server.Close()

	err := (&SuperMapWorkflowPlugin{}).TestConnection(context.Background(), connInfoForTestServer(t, server.URL))
	if err == nil {
		t.Fatal("TestConnection succeeded without GPA/SPS libs binding, want error")
	}
	if !strings.Contains(err.Error(), "GPA/SPS libs are not bound") {
		t.Fatalf("error = %q, want GPA/SPS binding failure", err)
	}
}

func TestSuperMapWorkflowTestConnectionAcceptsBoundRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "healthy",
			"dependencies": {
				"objectsjava": {"available": true, "path": "/opt/supermap/objectsjava/bin_linux_arm64"},
				"gpa_libs": {"available": true, "path": "/opt/supermap/gpa/libs"}
			}
		}`))
	}))
	defer server.Close()

	if err := (&SuperMapWorkflowPlugin{}).TestConnection(context.Background(), connInfoForTestServer(t, server.URL)); err != nil {
		t.Fatalf("TestConnection failed: %v", err)
	}
}

func connInfoForTestServer(t *testing.T, rawURL string) plugin.ConnectionInfo {
	t.Helper()
	host, port, ok := strings.Cut(strings.TrimPrefix(rawURL, "http://"), ":")
	if !ok {
		t.Fatalf("unexpected test server URL %s", rawURL)
	}
	return plugin.ConnectionInfo{
		"host": host,
		"port": port,
	}
}
