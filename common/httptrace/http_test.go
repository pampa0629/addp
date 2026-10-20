package httptrace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		if req.Header.Get("traceparent") != "" || req.Header.Get("baggage") != "" || req.Header.Get("tracestate") != "" {
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
