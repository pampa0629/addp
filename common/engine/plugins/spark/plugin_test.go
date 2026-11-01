package spark

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/selection"
	"github.com/addp/common/models"
)

const readyState = `{"url":"spark://internal-master:7077","status":"ALIVE","aliveworkers":1,"workers":[{"id":"worker-1","state":"ALIVE"}],"activeapps":[]}`

func TestClusterHasFederatedQueryTargetButNoStorageProvider(t *testing.T) {
	p := &Plugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	if err := p.ConnectionSpec().Validate(); err != nil {
		t.Fatal(err)
	}
	capabilities := p.Capabilities()
	encoded, err := plugin.MarshalEngineCapabilities(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	raw := models.JSONString(encoded)
	engine := &models.Engine{Capabilities: &raw}
	if selection.HasStorageCapability(engine) || capabilities.Storage != nil {
		t.Fatal("compute target must not claim storage")
	}
	if capabilities.Compute.Query == nil || !capabilities.Compute.Query.Supported ||
		capabilities.Compute.Query.Federation.RuntimeEngineType != "spark_workflow" ||
		capabilities.Compute.Query.Federation.ExecutionAudience != "spark" || capabilities.Compute.Query.SupportsCancel {
		t.Fatal("invalid federated compute target binding")
	}
	var provider any = p
	for name, implements := range map[string]bool{
		"catalog":       func() bool { _, ok := provider.(plugin.EngineCatalogProvider); return ok }(),
		"facts":         func() bool { _, ok := provider.(plugin.EngineCatalogFactsProvider); return ok }(),
		"batch":         func() bool { _, ok := provider.(plugin.BatchReadableProvider); return ok }(),
		"table session": func() bool { _, ok := provider.(plugin.TableReadSessionProvider); return ok }(),
		"SQL":           func() bool { _, ok := provider.(plugin.SQLQueryRuntimeProvider); return ok }(),
		"pool":          func() bool { _, ok := provider.(plugin.ConnectionPoolPlugin); return ok }(),
	} {
		if implements {
			t.Fatalf("old %s interface survived", name)
		}
	}
	if !reflect.DeepEqual(p.ConnectionIdentityFields(), []string{"host", "port"}) || p.DefaultPort() != 7077 || len(p.SensitiveFields()) != 0 {
		t.Fatal("invalid cluster identity")
	}
}

func TestConnectionRejectsLegacyAndMalformedConfiguration(t *testing.T) {
	base := func() plugin.ConnectionInfo {
		return plugin.ConnectionInfo{"host": "127.0.0.1", "port": 7077, "web_ui_url": "http://localhost:18088"}
	}
	for _, key := range []string{"database", "username", "password", "master_port", "user"} {
		info := base()
		info[key] = "secret-sentinel"
		if err := (&Plugin{}).ValidateConnectionInfo(info); err == nil || strings.Contains(err.Error(), "secret-sentinel") {
			t.Fatal("legacy field accepted or leaked")
		}
	}
	for key, values := range map[string][]any{
		"host":       {"", " bad ", "host:7077", "spark://host", 123},
		"port":       {nil, 0, 65536, 7077.5, "7077", true},
		"web_ui_url": {"", "http://", "http://user:secret@host", "http://host?query=1", "http://host#fragment", "ftp://host", "http://host:0", "http://host:65536", "http://host:", " http://host"},
	} {
		for _, value := range values {
			info := base()
			info[key] = value
			if (&Plugin{}).ValidateConnectionInfo(info) == nil {
				t.Fatalf("accepted invalid %s", key)
			}
		}
	}
	for _, host := range []string{"localhost", "host.docker.internal", "master.example", "::1"} {
		info := base()
		info["host"] = host
		info["port"] = float64(7077)
		if err := (&Plugin{}).ValidateConnectionInfo(info); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProbeReadsNativeStateAndConnectsConfiguredRPCWithoutSQL(t *testing.T) {
	rpc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	seen := make(chan struct{}, 1)
	go func() {
		conn, err := rpc.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		seen <- struct{}{}
	}()
	ui := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/proxy/json/" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected probe request")
		}
		_, _ = w.Write([]byte(readyState))
	}))
	defer ui.Close()
	info := plugin.ConnectionInfo{"host": "127.0.0.1", "port": rpc.Addr().(*net.TCPAddr).Port, "web_ui_url": ui.URL + "/proxy/"}
	if err := (&Plugin{}).TestConnection(context.Background(), info); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("configured RPC endpoint was not checked")
	}
}

func TestNativeStateRejectsUnavailableAndInconsistentWorkers(t *testing.T) {
	for _, body := range []string{`<html>Master</html>`, `{}`, `[]`, `null`,
		strings.Replace(readyState, `"ALIVE"`, `"STANDBY"`, 1),
		strings.Replace(readyState, `"aliveworkers":1`, `"aliveworkers":0`, 1),
		strings.Replace(readyState, `"aliveworkers":1`, `"aliveworkers":2`, 1),
		strings.Replace(readyState, `"aliveworkers":1`, `"aliveworkers":true`, 1),
		strings.Replace(readyState, `"id":"worker-1"`, `"id":""`, 1),
		strings.Replace(readyState, `"spark://internal-master:7077"`, `"http://internal-master:7077"`, 1),
		strings.Replace(readyState, `"spark://internal-master:7077"`, `"spark://user:secret@internal-master:7077"`, 1),
	} {
		if err := validateMasterState([]byte(body)); err == nil {
			t.Fatalf("invalid native state accepted: %s", body)
		}
	}
	var state map[string]any
	_ = json.Unmarshal([]byte(readyState), &state)
	state["workers"] = []any{map[string]any{"id": "same", "state": "ALIVE"}, map[string]any{"id": "same", "state": "DEAD"}}
	b, _ := json.Marshal(state)
	if validateMasterState(b) == nil {
		t.Fatal("duplicate worker inventory accepted")
	}
}

func TestProbeRejectsRedirectLargeBodyTLSFailureAndUnreachableRPC(t *testing.T) {
	rpc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := rpc.Addr().(*net.TCPAddr).Port
	_ = rpc.Close()
	for _, scenario := range []string{"redirect", "large", "http-error", "valid-but-no-rpc"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "redirect":
					http.Redirect(w, r, "/another", 302)
				case "large":
					_, _ = w.Write([]byte(strings.Repeat(" ", maxStateBytes+1)))
				case "http-error":
					w.WriteHeader(403)
					_, _ = w.Write([]byte("secret-sentinel"))
				default:
					_, _ = w.Write([]byte(readyState))
				}
			}))
			defer server.Close()
			err := (&Plugin{}).TestConnection(context.Background(), plugin.ConnectionInfo{"host": "127.0.0.1", "port": closedPort, "web_ui_url": server.URL})
			if err == nil || strings.Contains(err.Error(), "secret-sentinel") {
				t.Fatal("invalid probe accepted or leaked body")
			}
		})
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(readyState)) }))
	defer tlsServer.Close()
	if err := (&Plugin{}).TestConnection(context.Background(), plugin.ConnectionInfo{"host": "127.0.0.1", "port": closedPort, "web_ui_url": tlsServer.URL}); err == nil {
		t.Fatal("untrusted TLS was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := probeMaster(ctx, "127.0.0.1:1", tlsServer.URL); err == nil {
		t.Fatal("cancelled probe accepted")
	}
}

// This test runs only inside the gate-owned Standalone container, using the
// production Go probe against an actual Master and Worker, with no host ports.
func TestNativeMasterProbeAgainstStandalone(t *testing.T) {
	if os.Getenv("ADDP_SPARK_NATIVE_PROBE") != "1" {
		t.Skip("requires the owned native Spark gate")
	}
	// Fixture-only diagnostics distinguish an HTTP protocol response from a
	// DNS/transport error without exposing arbitrary upstream response bodies.
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://spark-master:8080/json/")
	if err != nil {
		t.Logf("fixture native transport: %T", err)
	} else {
		t.Logf("fixture native HTTP status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
		_ = response.Body.Close()
	}

	if err := (&Plugin{}).TestConnection(context.Background(), plugin.ConnectionInfo{
		"host": "spark-master", "port": 7077, "web_ui_url": "http://spark-master:8080",
	}); err != nil {
		t.Fatal(err)
	}
}
