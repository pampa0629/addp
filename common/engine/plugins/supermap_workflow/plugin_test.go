package supermap_workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestSuperMapWorkflowTestConnectionRequiresIObjectsCPP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "degraded",
			"dependencies": {
				"iobjects_cpp": {"available": false, "path": "/opt/supermap/bin/bin", "missing": ["libSuEngine.so"]},
				"freetype": {"available": true, "path": "/lib/aarch64-linux-gnu"},
				"nfs": {"available": true, "path": "/usr/sbin"}
			}
		}`))
	}))
	defer server.Close()

	err := (&SuperMapWorkflowPlugin{}).TestConnection(context.Background(), connInfoForTestServer(t, server.URL))
	if err == nil {
		t.Fatal("TestConnection succeeded without iObjects C++ runtime, want error")
	}
	if !strings.Contains(err.Error(), "iObjects C++ runtime is not available") {
		t.Fatalf("error = %q, want iObjects C++ runtime failure", err)
	}
}

func TestSuperMapWorkflowTestConnectionRequiresFreeType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "degraded",
			"dependencies": {
				"iobjects_cpp": {"available": true, "path": "/opt/supermap/bin/bin"},
				"freetype": {"available": false, "path": "/lib/aarch64-linux-gnu", "missing": ["libfreetype.so.6"]},
				"nfs": {"available": true, "path": "/usr/sbin"}
			}
		}`))
	}))
	defer server.Close()

	err := (&SuperMapWorkflowPlugin{}).TestConnection(context.Background(), connInfoForTestServer(t, server.URL))
	if err == nil {
		t.Fatal("TestConnection succeeded without FreeType runtime, want error")
	}
	if !strings.Contains(err.Error(), "FreeType runtime is not available") {
		t.Fatalf("error = %q, want FreeType runtime failure", err)
	}
}

func TestSuperMapWorkflowTestConnectionRequiresNFSClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "degraded",
			"dependencies": {
				"iobjects_cpp": {"available": true, "path": "/opt/supermap/bin/bin"},
				"freetype": {"available": true, "path": "/lib/aarch64-linux-gnu"},
				"nfs": {"available": false, "path": "/usr/sbin", "missing": ["mount.nfs"]}
			}
		}`))
	}))
	defer server.Close()

	err := (&SuperMapWorkflowPlugin{}).TestConnection(context.Background(), connInfoForTestServer(t, server.URL))
	if err == nil {
		t.Fatal("TestConnection succeeded without NFS client runtime, want error")
	}
	if !strings.Contains(err.Error(), "NFS client runtime is not available") {
		t.Fatalf("error = %q, want NFS client runtime failure", err)
	}
}

func TestSuperMapWorkflowTestConnectionAcceptsBoundRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "healthy",
			"dependencies": {
				"iobjects_cpp": {"available": true, "path": "/opt/supermap/bin/bin"},
				"freetype": {"available": true, "path": "/lib/aarch64-linux-gnu"},
				"nfs": {"available": true, "path": "/usr/sbin"}
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
