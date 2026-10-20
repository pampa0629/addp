package httptrace

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var propagationW3C = propagation.TraceContext{}

func strip(h http.Header) { h.Del("traceparent"); h.Del("tracestate"); h.Del("baggage") }
func method(v string) string {
	switch v {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE", "QUERY":
		return v
	}
	return "_OTHER"
}
func routine(p string) bool {
	return p == "/health/live" || p == "/health/ready" || p == "/api/v1/system/runtime/modules/heartbeat"
}

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if role, _ := processRole.Load().(string); role == "ingress" {
			c.Set("addp.request.external", true)
			strip(c.Request.Header)
		}
		r := current.Load()
		if r == nil {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		// An external parent/flag cannot make a Gateway join an attacker chosen trace.
		if r.role != "ingress" {
			ctx = propagationW3C.Extract(ctx, propagation.HeaderCarrier(c.Request.Header))
			parent := trace.SpanContextFromContext(ctx)
			ctx = trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{TraceID: parent.TraceID(), SpanID: parent.SpanID(), TraceFlags: parent.TraceFlags(), Remote: true}))
		} else {
			strip(c.Request.Header)
			ctx = trace.ContextWithSpanContext(ctx, trace.SpanContext{})
		}
		c.Request.Header.Del("tracestate")
		c.Request.Header.Del("baggage")
		ctx, span := tracer().Start(ctx, method(c.Request.Method), trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", method(c.Request.Method))))
		c.Request = c.Request.WithContext(ctx)
		started := time.Now()
		defer func() {
			panicValue := recover()
			failure := panicValue != nil || c.GetBool("addp.httpmetrics.recovered_panic") || c.Writer.Status() >= 500 || c.Request.Context().Err() != nil
			if w, ok := c.Writer.(interface{ WriteFailed() bool }); ok {
				failure = failure || w.WriteFailed()
			}
			span.SetName(method(c.Request.Method) + " " + c.FullPath())
			span.SetAttributes(attribute.String("http.route", c.FullPath()), attribute.Int("http.response.status_code", c.Writer.Status()), attribute.Bool("addp.http.server_failure", failure), attribute.Bool("addp.http.routine", routine(c.FullPath())), attribute.Bool("addp.http.slow", time.Since(started) >= time.Second))
			if failure {
				span.SetStatus(codes.Error, "")
			}
			span.End()
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		c.Next()
	}
}

type transport struct {
	base   http.RoundTripper
	origin string
}

// Transport wraps only a caller's registered module origin. Redirects or other
// targets strip context. Original TLS, proxy, timeout and streaming are kept.
func Transport(base http.RoundTripper, origin string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base: base, origin: origin}
}
func sameOrigin(a, b *url.URL) bool {
	return a != nil && b != nil && a.Scheme == b.Scheme && a.Host == b.Host && a.User == nil && b.User == nil
}
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header = req.Header.Clone()
	strip(copy.Header)
	origin, e := url.Parse(t.origin)
	if e != nil || !sameOrigin(req.URL, origin) || current.Load() == nil {
		return t.base.RoundTrip(copy)
	}
	ctx, span := tracer().Start(req.Context(), method(req.Method), trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("http.request.method", method(req.Method)), attribute.Bool("addp.http.routine", routine(req.URL.Path))))
	copy = copy.WithContext(ctx)
	propagationW3C.Inject(ctx, propagation.HeaderCarrier(copy.Header))
	if id := requestid.FromContext(ctx); id != "" {
		copy.Header.Set(requestid.RequestIDHeader, id)
	}
	response, err := t.base.RoundTrip(copy)
	end := func(failed bool) {
		if failed {
			span.SetStatus(codes.Error, "")
			span.SetAttributes(attribute.Bool("addp.http.server_failure", true))
		}
		span.End()
	}
	if err != nil {
		end(true)
		return response, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	if response.Body == nil {
		end(response.StatusCode >= 500)
	} else {
		response.Body = &body{ReadCloser: response.Body, end: end, failed: response.StatusCode >= 500}
	}
	return response, nil
}

type body struct {
	io.ReadCloser
	once   sync.Once
	end    func(bool)
	failed bool
}

func (b *body) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e != nil {
		b.once.Do(func() { b.end(b.failed || !errors.Is(e, io.EOF)) })
	}
	return n, e
}
func (b *body) Close() error {
	e := b.ReadCloser.Close()
	b.once.Do(func() { b.end(b.failed || e != nil) })
	return e
}

func Do(client *http.Client, req *http.Request, origin string) (*http.Response, error) {
	copy := *client
	copy.Transport = Transport(client.Transport, origin)
	return copy.Do(req)
}

// IDs are correlation only. Values are canonical and never carry user payload.
func IDs(ctx context.Context) (string, string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}
func ParseTraceID(v string) bool {
	id, e := trace.TraceIDFromHex(v)
	return e == nil && id.IsValid() && strings.ToLower(v) == v
}
func ParseSpanID(v string) bool {
	id, e := trace.SpanIDFromHex(v)
	return e == nil && id.IsValid() && strings.ToLower(v) == v
}
