package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/httptrace/tracetest"
	"github.com/addp/common/logger"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/manager/internal/preview"
	"github.com/gin-gonic/gin"
)

// This component test runs the actual preview handler and resolver with owned
// dependency failures. It does not certify real tenant authorization or SQL reads.
func TestPreviewTraceContextAndCompletionLog(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	const parentID = "2222222222222222"
	for _, failure := range []string{"system", "meta"} {
		t.Run(failure, func(t *testing.T) {
			recorder := tracetest.Start(t, "manager")
			var output bytes.Buffer
			logger.Init(logger.Options{Writer: &output})
			t.Cleanup(func() { logger.Init(logger.Options{}) })
			observed := map[string]string{}
			var observations sync.Mutex
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer addp_at_trace_fixture" {
					t.Errorf("unexpected dependency request method or credential")
				}
				if r.Header.Get("baggage") != "" || r.Header.Get("tracestate") != "" {
					t.Error("unapproved propagation headers reached dependency")
				}
				observations.Lock()
				observed[r.URL.Path] = r.Header.Get("traceparent")
				observations.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/system/engines/26" && failure != "system" {
					_, _ = w.Write([]byte(`{"id":26,"tenant_id":1,"engine_type":"postgresql","connection_info":{},"lifecycle_state":"active","connection_status":"online"}`))
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"dependency unavailable"}`))
			}))
			defer upstream.Close()
			tokens := client.ServiceTokenProviderFunc(func(ctx context.Context, tenant uint) (string, error) {
				if tenant != 1 {
					t.Errorf("unexpected tenant %d", tenant)
				}
				return "addp_at_trace_fixture", nil
			})
			resolver := preview.NewPreviewResolver(nil, client.NewSystemClient(upstream.URL, tokens), client.NewMetaClient(upstream.URL, tokens))
			handler := NewExplorerHandler(nil, resolver, nil, nil, nil)
			router := modulelifecycle.NewRouter()
			router.GET("/api/v1/manager/preview", func(c *gin.Context) {
				setTenantAuthContextForTest(c, 1, 7)
				handler.Preview(c)
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/manager/preview?"+url.Values{
				"locator": {"addp://engine/26/path/public/users?type=table&item_id=1831"}, "page": {"1"}, "page_size": {"20"},
			}.Encode(), nil)
			request.Header.Set("traceparent", "00-"+traceID+"-"+parentID+"-01")
			request.Header.Set("baggage", "private=untrusted")
			request.Header.Set("tracestate", "private=untrusted")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			observations.Lock()
			defer observations.Unlock()
			if response.Code < 500 {
				t.Fatalf("dependency failure returned %d", response.Code)
			}
			spans := recorder.Finish()
			var serverID string
			clients := map[string]bool{}
			for _, span := range spans {
				if hex.EncodeToString(span.TraceId) != traceID {
					t.Fatal("preview dependency escaped the incoming trace")
				}
				if span.Kind.String() == "SPAN_KIND_SERVER" {
					serverID = hex.EncodeToString(span.SpanId)
					if span.Name != "GET /api/v1/manager/preview" || hex.EncodeToString(span.ParentSpanId) != parentID {
						t.Fatal("preview server did not preserve the real incoming parent")
					}
				} else if span.Kind.String() == "SPAN_KIND_CLIENT" {
					clients[hex.EncodeToString(span.SpanId)] = true
				}
			}
			want := 1
			if failure == "meta" {
				want = 2
			}
			if serverID == "" || len(clients) != want || len(observed) != want || len(spans) != want+1 {
				t.Fatalf("server=%q clients=%d dependency calls=%d spans=%d", serverID, len(clients), len(observed), len(spans))
			}
			for _, span := range spans {
				if clients[hex.EncodeToString(span.SpanId)] && hex.EncodeToString(span.ParentSpanId) != serverID {
					t.Fatal("outgoing request lost preview server parent")
				}
			}
			for _, header := range observed {
				parts := strings.Split(header, "-")
				if len(parts) != 4 || parts[1] != traceID || !clients[parts[2]] {
					t.Fatalf("outgoing traceparent does not match exported client span: %q", header)
				}
			}
			decoder := json.NewDecoder(&output)
			completions := 0
			for decoder.More() {
				var row map[string]any
				if err := decoder.Decode(&row); err != nil {
					t.Fatal(err)
				}
				if row["msg"] == "http.request.completed" {
					completions++
					if row["trace_id"] != traceID || row["span_id"] != serverID || row["route"] != "/api/v1/manager/preview" || row["status"] != float64(response.Code) {
						t.Fatalf("preview completion log lost association: %#v", row)
					}
					for _, forbidden := range []string{"locator", "query", "authorization", "body", "sql"} {
						if _, found := row[forbidden]; found {
							t.Fatalf("completion log leaked %s", forbidden)
						}
					}
				}
			}
			if completions != 1 {
				t.Fatalf("got %d completion logs", completions)
			}
		})
	}
}
