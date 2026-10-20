package httpmetrics

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func gather(t *testing.T, server *Server) map[string]*dto.MetricFamily {
	t.Helper()
	registry := prometheus.NewRegistry()
	registry.MustRegister(server)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]*dto.MetricFamily{}
	for _, family := range families {
		result[family.GetName()] = family
	}
	return result
}

func labels(metric *dto.Metric) map[string]string {
	result := map[string]string{}
	for _, label := range metric.Label {
		result[label.GetName()] = label.GetValue()
	}
	return result
}

func activeValue(family *dto.MetricFamily) float64 {
	value := 0.0
	for _, metric := range family.Metric {
		value += metric.Gauge.GetValue()
	}
	return value
}

func serve(router http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", "Bearer private-token")
	request.Header.Set("X-Request-ID", "private-request")
	request.Header.Set("X-Forwarded-Proto", "untrusted-scheme")
	router.ServeHTTP(response, request)
	return response
}

func TestTemplatesAndUnmatchedRoutesDoNotExposeRequestData(t *testing.T) {
	server := New(nil)
	router := gin.New()
	router.Use(server.Middleware())
	router.GET("/api/v1/example/items/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, path := range []string{"/api/v1/example/items/private-a?sql=private-query", "/api/v1/example/items/private-b"} {
		if response := serve(router, "GET", path); response.Code != 204 {
			t.Fatal(response.Code)
		}
	}
	for i := 0; i < 100; i++ {
		serve(router, "CUSTOM", "/private-unmatched/"+strings.Repeat("x", i))
	}
	families := gather(t, server)
	if got := families["addp_http_server_requests_total"].Metric[0].Counter.GetValue(); got != 102 {
		t.Fatal(got)
	}
	histograms := families["http_server_request_duration_seconds"].Metric
	if len(histograms) != 2 {
		t.Fatalf("routes must remain bounded: %d", len(histograms))
	}
	for _, metric := range histograms {
		l := labels(metric)
		if len(l) != 5 || l["url_scheme"] != "http" || l["error_type"] != "" {
			t.Fatal(l)
		}
		switch l["http_request_method"] {
		case "GET":
			if l["http_route"] != "/api/v1/example/items/:id" || l["http_response_status_code"] != "204" || metric.Histogram.GetSampleCount() != 2 {
				t.Fatal(metric)
			}
		case "_OTHER":
			if l["http_route"] != "" || l["http_response_status_code"] != "404" || metric.Histogram.GetSampleCount() != 100 {
				t.Fatal(metric)
			}
		default:
			t.Fatal(l)
		}
		if len(metric.Histogram.Bucket) != 14 || metric.Histogram.GetSampleSum() < 0 || metric.Histogram.Bucket[13].GetUpperBound() != 10 {
			t.Fatal("duration must use the OTel seconds boundaries")
		}
	}
	if strings.Contains(families["http_server_request_duration_seconds"].String(), "private-") {
		t.Fatal("sensitive request data entered metrics")
	}
}

func TestTransportFailureClassificationAndRecovery(t *testing.T) {
	server := New(nil)
	router := gin.New()
	router.Use(server.Middleware(), gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) { c.AbortWithStatus(500) }))
	for path, status := range map[string]int{"/ok": 200, "/business-refusal": 200, "/denied": 403, "/limited": 429, "/not-ready": 503} {
		router.GET(path, func(c *gin.Context) { c.Status(status) })
		serve(router, "GET", path)
	}
	router.GET("/panic", func(*gin.Context) { panic("private-error-body") })
	if serve(router, "GET", "/panic").Code != 500 {
		t.Fatal("normal recovery behavior changed")
	}
	for path, err := range map[string]error{"/cancel": context.Canceled, "/timeout": context.DeadlineExceeded} {
		router.GET(path, func(c *gin.Context) {
			c.Request = c.Request.WithContext(finishedContext{Context: c.Request.Context(), err: err})
			c.Status(504)
		})
		serve(router, "GET", path)
	}
	families := gather(t, server)
	if families["addp_http_server_requests_total"].Metric[0].Counter.GetValue() != 8 || families["addp_http_server_failures_total"].Metric[0].Counter.GetValue() != 4 {
		t.Fatal("5xx, timeout and cancellation must count once each; 4xx and business bodies do not imply service failure")
	}
	want := map[string]string{"/ok": "", "/business-refusal": "", "/denied": "", "/limited": "", "/not-ready": "503", "/panic": "500", "/cancel": "canceled", "/timeout": "timeout"}
	for _, metric := range families["http_server_request_duration_seconds"].Metric {
		l := labels(metric)
		if expected, ok := want[l["http_route"]]; !ok || l["error_type"] != expected {
			t.Fatal(l)
		}
	}
}

type finishedContext struct {
	context.Context
	err error
}

func (c finishedContext) Err() error { return c.err }

func TestActiveRequestsAreIndependentOfCompletedRequestCounts(t *testing.T) {
	server := New(nil)
	router := gin.New()
	router.Use(server.Middleware())
	entered, release := make(chan struct{}, 8), make(chan struct{})
	router.GET("/wait/:id", func(c *gin.Context) { entered <- struct{}{}; <-release; c.Status(200) })
	var done sync.WaitGroup
	for i := 0; i < 8; i++ {
		done.Add(1)
		go func() { defer done.Done(); serve(router, "GET", "/wait/private-id") }()
	}
	for i := 0; i < 8; i++ {
		<-entered
	}
	families := gather(t, server)
	if activeValue(families["http_server_active_requests"]) != 8 || families["addp_http_server_requests_total"].Metric[0].Counter.GetValue() != 0 {
		t.Fatal("in-flight requests must not be interpreted as completed QPS")
	}
	close(release)
	done.Wait()
	families = gather(t, server)
	if activeValue(families["http_server_active_requests"]) != 0 || families["addp_http_server_requests_total"].Metric[0].Counter.GetValue() != 8 {
		t.Fatal("completion must release all active requests")
	}
}

func TestKnownMethodOverrideSchemeAndPrivateRegistries(t *testing.T) {
	server := New([]string{"CUSTOM"})
	router := gin.New()
	router.Use(server.Middleware())
	router.Handle("CUSTOM", "/api/*path", func(c *gin.Context) { c.Status(200) })
	router.GET("/api/*path", func(c *gin.Context) { c.Status(200) })
	request := httptest.NewRequest("CUSTOM", "https://example.test/api/private-id", nil)
	request.TLS = &tls.ConnectionState{}
	router.ServeHTTP(httptest.NewRecorder(), request)
	serve(router, "GET", "/api/private-id")
	families := gather(t, server)
	for _, metric := range families["http_server_request_duration_seconds"].Metric {
		l := labels(metric)
		if l["http_route"] != "/api/*path" || (l["http_request_method"] == "CUSTOM" && l["url_scheme"] != "https") || l["http_request_method"] == "GET" {
			t.Fatal(l)
		}
	}
	if gather(t, New(nil))["addp_http_server_requests_total"].Metric[0].Counter.GetValue() != 0 {
		t.Fatal("collectors from different running instances must be independent")
	}
}

func TestUnrecoveredPanicStillReleasesActiveRequest(t *testing.T) {
	server := New(nil)
	router := gin.New()
	router.Use(server.Middleware())
	router.GET("/panic", func(*gin.Context) { panic("private-panic") })
	func() {
		defer func() {
			if value := recover(); value != "private-panic" {
				t.Fatal("instrumentation changed application panic behavior")
			}
		}()
		serve(router, "GET", "/panic")
	}()
	families := gather(t, server)
	metric := families["http_server_request_duration_seconds"].Metric[0]
	if labels(metric)["http_response_status_code"] != "" || labels(metric)["error_type"] != "_OTHER" || families["http_server_active_requests"].Metric[0].Gauge.GetValue() != 0 || families["addp_http_server_failures_total"].Metric[0].Counter.GetValue() != 1 {
		t.Fatal("unwritten panic must not manufacture a successful status or retain an active request")
	}
}

func TestFailedBodyWritesRemainFailuresAfterSuccessfulHeaders(t *testing.T) {
	for _, writeString := range []bool{false, true} {
		server := New(nil)
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Writer = failingWriter{ResponseWriter: c.Writer} }, server.Middleware())
		router.GET("/body", func(c *gin.Context) {
			c.Writer.WriteHeaderNow()
			var n int
			var err error
			if writeString {
				n, err = c.Writer.WriteString("private-body")
			} else {
				n, err = c.Writer.Write([]byte("private-body"))
			}
			if n != 0 || !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal("instrumentation must preserve the original write result")
			}
		})
		if serve(router, "GET", "/body").Code != 200 {
			t.Fatal("already sent response status must remain unchanged")
		}
		families := gather(t, server)
		l := labels(families["http_server_request_duration_seconds"].Metric[0])
		if l["http_response_status_code"] != "200" || l["error_type"] != "_OTHER" || families["addp_http_server_failures_total"].Metric[0].Counter.GetValue() != 1 {
			t.Fatal(l)
		}
	}
}

type failingWriter struct{ gin.ResponseWriter }

func (failingWriter) Write([]byte) (int, error)       { return 0, io.ErrClosedPipe }
func (failingWriter) WriteString(string) (int, error) { return 0, io.ErrClosedPipe }
