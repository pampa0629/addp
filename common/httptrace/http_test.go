package httptrace

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/httpmetrics"
	"github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
	sdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestSharedModuleClientsUseTheSoleHTTPTransport(t *testing.T) {
	paths, err := filepath.Glob("../client/*.go")
	if err != nil || len(paths) == 0 {
		t.Fatal("shared clients unavailable", err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "Do" {
				owner, ok := selector.X.(*ast.Ident)
				if !ok || owner.Name != "httptrace" {
					t.Errorf("%s bypasses the shared HTTP propagation transport", path)
				}
			}
			return true
		})
	}
}

type memoryExporter struct {
	mu    sync.Mutex
	spans []sdk.ReadOnlySpan
	block <-chan struct{}
}

func (e *memoryExporter) ExportSpans(ctx context.Context, v []sdk.ReadOnlySpan) error {
	if e.block != nil {
		select {
		case <-e.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, v...)
	return nil
}
func (e *memoryExporter) Shutdown(context.Context) error { return nil }
func runtimeForTest(t *testing.T, role string) (*Runtime, *memoryExporter) {
	t.Helper()
	e := &memoryExporter{}
	r := newRuntime("monitor", role, "test-instance", e)
	old := current.Swap(r)
	oldRole, _ := processRole.Load().(string)
	processRole.Store(role)
	t.Cleanup(func() {
		current.Store(old)
		processRole.Store(oldRole)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = r.provider.Shutdown(ctx)
	})
	return r, e
}
func flush(t *testing.T, r *Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := r.provider.ForceFlush(ctx); e != nil {
		t.Fatal(e)
	}
}

func TestHTTPContextPrivacyAndStreamingPropagation(t *testing.T) {
	r, e := runtimeForTest(t, "backend")
	var parent trace.SpanContext
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		parent = trace.SpanContextFromContext(propagationW3C.Extract(req.Context(), propagationHeader(req.Header)))
		if req.Header.Get("baggage") != "" || req.Header.Get("tracestate") != "" || req.Header.Get("X-Request-ID") == "" {
			t.Error("unsafe headers or lost request correlation")
		}
		io.WriteString(w, "private response")
	}))
	defer downstream.Close()
	router := gin.New()
	router.Use(httpmetrics.Process().Middleware(), Middleware(), requestid.RequestIDMiddleware(), httpmetrics.Recovery())
	router.GET("/items/:id", func(c *gin.Context) {
		req, _ := http.NewRequestWithContext(c.Request.Context(), "GET", downstream.URL+"/private?token=private", nil)
		req.Header.Set("baggage", "secret=private")
		resp, err := Do(downstream.Client(), req, downstream.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		c.Status(204)
	})
	req := httptest.NewRequest("GET", "/items/private-user?token=private", nil)
	req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-00")
	req.Header.Set("tracestate", "private=secret")
	req.Header.Set("baggage", "token=private")
	router.ServeHTTP(httptest.NewRecorder(), req)
	flush(t, r)
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.spans) != 2 {
		t.Fatalf("spans=%d", len(e.spans))
	}
	var server, client sdk.ReadOnlySpan
	for _, s := range e.spans {
		if s.SpanKind() == trace.SpanKindServer {
			server = s
		} else {
			client = s
		}
		raw, _ := json.Marshal(s.Attributes())
		if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "token") {
			t.Fatal("private attribute", string(raw))
		}
	}
	if server == nil || client == nil || server.SpanContext().TraceID().String() != "11111111111111111111111111111111" || server.Parent().SpanID().String() != "2222222222222222" || client.Parent().SpanID() != server.SpanContext().SpanID() || parent.SpanID() != client.SpanContext().SpanID() {
		t.Fatal("broken server/client/remote parent")
	}
	if server.Name() != "GET /items/:id" {
		t.Fatal("raw path or missing template")
	}
}

// HeaderCarrier's dependency stays explicit so no baggage propagator is used.
type propagationHeader http.Header

func (h propagationHeader) Get(k string) string { return http.Header(h).Get(k) }
func (h propagationHeader) Set(k, v string)     { http.Header(h).Set(k, v) }
func (h propagationHeader) Keys() []string {
	v := []string{}
	for k := range h {
		v = append(v, k)
	}
	return v
}

func TestGatewayRestartsExternalTraceAndRejectsCorrelationPayload(t *testing.T) {
	r, e := runtimeForTest(t, "ingress")
	router := gin.New()
	router.Use(Middleware(), requestid.RequestIDMiddleware())
	router.GET("/", func(c *gin.Context) {
		if c.GetString("request_id") == "private" {
			t.Fatal("inherited request payload")
		}
		c.Status(200)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	req.Header.Set("X-Request-ID", "private")
	router.ServeHTTP(httptest.NewRecorder(), req)
	flush(t, r)
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.spans) != 1 || e.spans[0].Parent().IsValid() || e.spans[0].SpanContext().TraceID().String() == "11111111111111111111111111111111" {
		t.Fatal("external parent accepted")
	}
}

func TestRedirectDoesNotLeakTraceHeaders(t *testing.T) {
	r, _ := runtimeForTest(t, "backend")
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("traceparent") != "" || req.Header.Get("baggage") != "" || req.Header.Get("tracestate") != "" || req.Header.Get("X-Request-ID") != "" {
			t.Error("trace leaked outside registered origin")
		}
		w.WriteHeader(204)
	}))
	defer foreign.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { http.Redirect(w, req, foreign.URL, http.StatusFound) }))
	defer source.Close()
	req, _ := http.NewRequest("GET", source.URL, nil)
	resp, err := Do(source.Client(), req, source.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	flush(t, r)
}

func TestQueueOverflowAndShutdownStayBounded(t *testing.T) {
	blocked := make(chan struct{})
	e := &memoryExporter{block: blocked}
	r := newRuntime("monitor", "backend", "test", e)
	started := time.Now()
	for i := 0; i < 5000; i++ {
		_, s := r.provider.Tracer("test").Start(context.Background(), "bounded")
		s.End()
	}
	if time.Since(started) > time.Second || r.queue.dropped.Load() == 0 {
		t.Fatal("queue blocked request or no drop evidence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = r.provider.Shutdown(ctx)
	close(blocked)
	select {
	case <-r.queue.done:
	case <-time.After(time.Second):
		t.Fatal("shutdown worker leaked")
	}
}

func TestStrictDeploymentSelectionAndIDGrammar(t *testing.T) {
	for _, raw := range []string{"null", "{}", "[] {}", `[{"module_name":"monitor","role":"backend","endpoint":"http://localhost/v1/traces","tls_dir":"/tmp"}]`, `[{"module_name":"monitor","role":"backend","endpoint":"https://localhost/v1/traces?secret=x","tls_dir":"/tmp"}]`} {
		if _, e := ParseDeployment(raw, "monitor", "backend"); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	if ParseTraceID(strings.Repeat("0", 32)) || ParseTraceID(strings.Repeat("A", 32)) || !ParseTraceID(strings.Repeat("1", 32)) || !ParseSpanID(strings.Repeat("2", 16)) {
		t.Fatal("invalid correlation grammar")
	}
}

func TestDisabledIngressStillResetsExternalCorrelation(t *testing.T) {
	previous, _ := processRole.Load().(string)
	defer processRole.Store(previous)
	old := current.Swap(nil)
	defer current.Store(old)
	MarkIngressBoundary()
	router := gin.New()
	router.Use(Middleware(), requestid.RequestIDMiddleware())
	router.GET("/", func(c *gin.Context) {
		if c.GetHeader("traceparent") != "" || c.GetHeader("baggage") != "" {
			t.Error("disabled export accepted external trace context")
		}
		if requestid.FromGinContext(c) == "11111111-1111-4111-8111-111111111111" {
			t.Error("external request ID reused")
		}
		c.Status(204)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	req.Header.Set("baggage", "private=value")
	req.Header.Set("X-Request-ID", "11111111-1111-4111-8111-111111111111")
	router.ServeHTTP(httptest.NewRecorder(), req)
}

func TestServerFailureMatchesMetricsAndKeepsFourHundredsSeparate(t *testing.T) {
	r, exporter := runtimeForTest(t, "backend")
	for _, tc := range []struct {
		status   int
		canceled bool
		want     bool
	}{{200, false, false}, {404, false, false}, {503, false, true}, {200, true, true}} {
		router := gin.New()
		router.Use(Middleware(), httpmetrics.Recovery())
		router.GET("/items/:id", func(c *gin.Context) { c.Status(tc.status) })
		request := httptest.NewRequest("GET", "/items/private-value", nil)
		if tc.canceled {
			ctx, cancel := context.WithCancel(request.Context())
			cancel()
			request = request.WithContext(ctx)
		}
		router.ServeHTTP(httptest.NewRecorder(), request)
		flush(t, r)
		exporter.mu.Lock()
		last := exporter.spans[len(exporter.spans)-1]
		found := false
		for _, a := range last.Attributes() {
			if string(a.Key) == "addp.http.server_failure" {
				found = true
				if a.Value.AsBool() != tc.want {
					t.Errorf("status %d cancellation %v failed=%v", tc.status, tc.canceled, a.Value)
				}
			}
		}
		exporter.mu.Unlock()
		if !found {
			t.Fatal("missing explicit failure fact")
		}
	}
}

func TestEnabledTransportPreservesBidirectionalHTTPUpgrade(t *testing.T) {
	r, exporter := runtimeForTest(t, "ingress")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		connection, stream, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = stream.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		_ = stream.Flush()
		message := make([]byte, 4)
		if _, err := io.ReadFull(stream, message); err != nil {
			return
		}
		_, _ = stream.WriteString("echo:" + string(message))
		_ = stream.Flush()
	}))
	defer upstream.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", upstream.URL, nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "test")
	response, err := Transport(base, upstream.URL).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	stream, ok := response.Body.(io.ReadWriteCloser)
	if response.StatusCode != 101 || !ok {
		t.Fatal("trace transport removed the HTTP upgrade stream", response.StatusCode)
	}
	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	message := make([]byte, 9)
	if _, err := io.ReadFull(stream, message); err != nil || string(message) != "echo:ping" {
		t.Fatal("bidirectional upgrade failed", string(message), err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	flush(t, r)
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if len(exporter.spans) != 1 || exporter.spans[0].SpanKind() != trace.SpanKindClient {
		t.Fatal("upgrade span did not finish exactly once", len(exporter.spans))
	}
}
