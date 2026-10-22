// Package tracetest enables the real optional trace runtime against an owned
// ephemeral TLS collector. It is shared by owner transaction tests only.
package tracetest

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/addp/common/httptrace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type Recorder struct {
	mu       sync.Mutex
	spans    []*tracepb.Span
	shutdown func()
}

// Finish flushes ended spans and closes the owned runtime. Repeated calls are safe.
func (r *Recorder) Finish() []*tracepb.Span {
	r.shutdown()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*tracepb.Span(nil), r.spans...)
}

// Start uses the public deployment and TLS path, without a production test hook.
// The caller must not run in parallel because the process has one trace runtime.
func Start(t testing.TB, module string) *Recorder {
	t.Helper()
	recorder := &Recorder{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		var request collector.ExportTraceServiceRequest
		if proto.Unmarshal(body, &request) != nil {
			http.Error(w, "invalid protobuf", http.StatusBadRequest)
			return
		}
		recorder.mu.Lock()
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				if len(recorder.spans)+len(scope.Spans) <= 4096 {
					recorder.spans = append(recorder.spans, scope.Spans...)
				}
			}
		}
		recorder.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	certificate := server.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	for name, data := range map[string][]byte{
		"ca.crt": cert, "client.crt": cert,
		"client.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	deployment, err := json.Marshal([]httptrace.Deployment{{Module: module, Role: "backend", Endpoint: server.URL + "/v1/traces", TLSDir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADDP_OBSERVABILITY_TRACES_ENABLED", "true")
	t.Setenv("ADDP_TRACE_DEPLOYMENTS", string(deployment))
	recorder.shutdown = httptrace.StartOptional(module, "backend")
	t.Cleanup(recorder.shutdown)
	return recorder
}
