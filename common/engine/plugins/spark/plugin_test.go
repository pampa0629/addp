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
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/models"
	"golang.org/x/sys/unix"
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
	defer rpc.Close()
	rpcPort := rpc.Addr().(*net.TCPAddr).Port
	checkFailure := func(t *testing.T, ctx context.Context, port int, uiURL, want string) {
		t.Helper()
		err := (&Plugin{}).TestConnection(ctx, plugin.ConnectionInfo{"host": "127.0.0.1", "port": port, "web_ui_url": uiURL})
		if err == nil {
			t.Fatal("invalid probe accepted")
		}
		if strings.Contains(err.Error(), "secret-sentinel") {
			t.Fatal("probe leaked the upstream response body")
		}
		if !reflect.DeepEqual(err, commoni18n.NewMessageError(want)) {
			t.Fatalf("probe did not reject with %s (error type %T)", want, err)
		}
	}

	for _, scenario := range []struct{ name, want string }{
		{"redirect", "engine.spark.state_unavailable"},
		{"large", "engine.spark.state_invalid"},
		{"http-error", "engine.spark.state_unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch scenario.name {
				case "redirect":
					if r.URL.Path == "/json/" {
						http.Redirect(w, r, "/another", http.StatusFound)
						return
					}
					_, _ = w.Write([]byte(readyState))
				case "large":
					_, _ = w.Write([]byte(readyState + strings.Repeat(" ", maxStateBytes+1-len(readyState))))
				case "http-error":
					w.WriteHeader(403)
					_, _ = w.Write([]byte("secret-sentinel"))
				}
			}))
			defer server.Close()
			checkFailure(t, context.Background(), rpcPort, server.URL, scenario.want)
		})
	}
	t.Run("valid-but-no-rpc", func(t *testing.T) {
		port := reserveNonListeningTCPPort(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(readyState)) }))
		defer server.Close()
		// Some kernels leave the dial pending for a bound, non-listening port.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		checkFailure(t, ctx, port, server.URL, "engine.spark.rpc_unavailable")
	})

	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(readyState)) }))
	defer tlsServer.Close()
	checkFailure(t, context.Background(), rpcPort, tlsServer.URL, "engine.spark.state_unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := probeMaster(ctx, "127.0.0.1:1", tlsServer.URL); err == nil {
		t.Fatal("cancelled probe accepted")
	}
}

func reserveNonListeningTCPPort(t *testing.T) int {
	t.Helper()
	// Keep the TCP port bound for the whole test without accepting connections.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, unix.IPPROTO_TCP)
	if err != nil {
		t.Fatal(err)
	}
	unix.CloseOnExec(fd)
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	address, err := unix.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	return address.(*unix.SockaddrInet4).Port
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
