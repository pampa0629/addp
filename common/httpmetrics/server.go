// Package httpmetrics owns HTTP server measurements shared by ADDP Go services.
// It records transport facts only; interpreting them as service quality and
// authorizing queries remain Monitor responsibilities.
package httpmetrics

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

// Server has a private collector set. It neither registers global metrics nor
// opens a listener. Deployment adapters must use the existing protected source.
type Server struct {
	duration     *prometheus.HistogramVec
	aggregate    prometheus.Histogram
	active       *prometheus.GaugeVec
	requests     prometheus.Counter
	failures     prometheus.Counter
	clientErrors prometheus.Counter
	methods      map[string]bool
	started      prometheus.Gauge
}

var process = sync.OnceValue(func() *Server { return New(nil) })

const panicKey = "addp.httpmetrics.recovered_panic"

// Process is the sole collector for the actual Go process. System's immutable
// runtime declaration supplies its identity through the existing metrics source.
func Process() *Server { return process() }

// Recovery preserves response status/write semantics while recording that a
// response interrupted after its headers were sent was not a successful request.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				c.Set(panicKey, true)
				// Shared request logging emits only bounded transport facts.
				// Gin's default recovery dumps raw request headers and panic text.
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

// New uses OpenTelemetry HTTP server metric names, seconds, explicit duration
// boundaries and finite method/error attributes. knownMethods is a full override
// of the standard known method set; nil selects the standard methods.
func New(knownMethods []string) *Server {
	if knownMethods == nil {
		knownMethods = []string{"CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE", "QUERY"}
	}
	methods := make(map[string]bool, len(knownMethods))
	for _, method := range knownMethods {
		methods[method] = true
	}
	server := &Server{
		methods: methods,
		started: prometheus.NewGauge(prometheus.GaugeOpts{Name: "addp_http_server_instrumentation_start_time_seconds", Help: "Start of HTTP request instrumentation in Unix seconds; zero means no HTTP router is instrumented."}),
		aggregate: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "addp_http_server_duration_seconds", Help: "All completed HTTP request durations for this process, initialized before its first request.",
			Buckets: durationBuckets(),
		}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_server_request_duration_seconds", Help: "Duration of completed HTTP server requests in seconds.",
			Buckets: durationBuckets(),
		}, []string{"http_request_method", "url_scheme", "http_route", "http_response_status_code", "error_type"}),
		active: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "http_server_active_requests", Help: "Number of HTTP server requests currently being processed.",
		}, []string{"http_request_method", "url_scheme"}),
		requests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "addp_http_server_requests_total", Help: "All HTTP server requests completed since instrumentation started, including unmatched routes.",
		}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "addp_http_server_failures_total", Help: "HTTP 5xx responses and requests interrupted by timeout, cancellation, a failed response write or an unrecovered panic; each request counts once.",
		}),
		clientErrors: prometheus.NewCounter(prometheus.CounterOpts{Name: "addp_http_server_client_errors_total", Help: "HTTP requests with a 4xx response status, counted separately from server failures."}),
	}
	for method := range methods {
		server.active.WithLabelValues(method, "http")
		server.active.WithLabelValues(method, "https")
	}
	server.active.WithLabelValues("_OTHER", "http")
	server.active.WithLabelValues("_OTHER", "https")
	return server
}

func durationBuckets() []float64 {
	return []float64{.005, .01, .025, .05, .075, .1, .25, .5, .75, 1, 2.5, 5, 7.5, 10}
}

func (s *Server) Describe(ch chan<- *prometheus.Desc) {
	s.duration.Describe(ch)
	s.aggregate.Describe(ch)
	s.active.Describe(ch)
	s.requests.Describe(ch)
	s.failures.Describe(ch)
	s.clientErrors.Describe(ch)
	s.started.Describe(ch)
}

func (s *Server) Collect(ch chan<- prometheus.Metric) {
	s.duration.Collect(ch)
	s.aggregate.Collect(ch)
	s.active.Collect(ch)
	s.requests.Collect(ch)
	s.failures.Collect(ch)
	s.clientErrors.Collect(ch)
	s.started.Collect(ch)
}

// CanonicalMethod is shared by process metrics, traces and request logs.
func (s *Server) CanonicalMethod(value string) string {
	if s.methods[value] {
		return value
	}
	return "_OTHER"
}

// FailureType exposes the existing transport fact without adding a collector
// or interpreting a response body. A 4xx status alone is not a server failure.
func FailureType(c *gin.Context) string {
	failed := false
	if w, ok := c.Writer.(interface{ WriteFailed() bool }); ok {
		failed = w.WriteFailed()
	}
	return failureType(c, false, failed)
}
func failureType(c *gin.Context, panicked, failed bool) string {
	switch {
	case panicked || c.GetBool(panicKey):
		return "_OTHER"
	case errors.Is(c.Request.Context().Err(), context.DeadlineExceeded):
		return "timeout"
	case errors.Is(c.Request.Context().Err(), context.Canceled):
		return "canceled"
	case failed:
		return "_OTHER"
	case c.Writer.Status() >= 500 && c.Writer.Status() <= 599:
		return strconv.Itoa(c.Writer.Status())
	}
	return ""
}

// Middleware must enclose recovery and every request handler, including health,
// readiness, authentication rejection and NoRoute. Gin's matched template is the
// only route source. Neither URL paths nor forwarded/request identity headers
// enter the collector. A Gateway wildcard remains a wildcard, not a backend API.
func (s *Server) Middleware() gin.HandlerFunc {
	s.started.Set(float64(time.Now().UnixNano()) / 1e9)
	return func(c *gin.Context) {
		method := s.CanonicalMethod(c.Request.Method)
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		active := s.active.WithLabelValues(method, scheme)
		active.Inc()
		writer := &observedWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		started := time.Now()
		defer func() {
			panicValue := recover()
			status := ""
			// An unwritten response on panic has no sent HTTP status. Outer
			// recovery can still produce one, so instrumentation belongs outside it.
			if panicValue == nil || c.Writer.Written() {
				if code := c.Writer.Status(); code >= 100 && code <= 599 {
					status = strconv.Itoa(code)
				}
			}
			errorType := failureType(c, panicValue != nil, writer.failed)
			elapsed := time.Since(started).Seconds()
			s.duration.WithLabelValues(method, scheme, c.FullPath(), status, errorType).Observe(elapsed)
			s.aggregate.Observe(elapsed)
			s.requests.Inc()
			if errorType != "" {
				s.failures.Inc()
			}
			if c.Writer.Status() >= 400 && c.Writer.Status() <= 499 {
				s.clientErrors.Inc()
			}
			active.Dec()
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		c.Next()
	}
}

// Preserve Gin's flushing/hijacking interface and propagate every original write
// result. A failed body write is an unfinished transport, even with a 2xx header.
type observedWriter struct {
	gin.ResponseWriter
	failed bool
}

func (w *observedWriter) WriteFailed() bool { return w.failed }

func (w *observedWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.failed = w.failed || err != nil || n != len(data)
	return n, err
}

func (w *observedWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	w.failed = w.failed || err != nil || n != len(data)
	return n, err
}
