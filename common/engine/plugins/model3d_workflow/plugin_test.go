package model3d_workflow

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestModel3DWorkflowTestConnectionRequiresBoundConverter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"degraded",
			"service":"model3d-workflow-engine",
			"conversion_ready":false,
			"dependencies":{"converter":{"path":"/engine/bin/_3dtile","details":"/engine/bin/_3dtile was not found or is not executable"}}
		}`))
	}))
	defer server.Close()

	connInfo := model3DWorkflowTestConnectionInfo(t, server.URL)
	err := (&Model3DWorkflowPlugin{}).TestConnection(context.Background(), connInfo)
	if err == nil {
		t.Fatal("TestConnection() error = nil, want converter binding error")
	}
	if !strings.Contains(err.Error(), "model3d converter is not bound") {
		t.Fatalf("TestConnection() error = %v, want converter binding error", err)
	}
}

func TestModel3DWorkflowTestConnectionAcceptsBoundConverter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"healthy",
			"service":"model3d-workflow-engine",
			"conversion_ready":true,
			"dependencies":{"converter":{"path":"/engine/bin/_3dtile","available":true}}
		}`))
	}))
	defer server.Close()

	connInfo := model3DWorkflowTestConnectionInfo(t, server.URL)
	if err := (&Model3DWorkflowPlugin{}).TestConnection(context.Background(), connInfo); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
}

func model3DWorkflowTestConnectionInfo(t *testing.T, rawURL string) plugin.ConnectionInfo {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	host := parsed.Hostname()
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse test port: %v", err)
	}
	return plugin.ConnectionInfo{
		"protocol": parsed.Scheme,
		"host":     host,
		"port":     port,
		"name":     fmt.Sprintf("%s:%d", host, port),
	}
}
