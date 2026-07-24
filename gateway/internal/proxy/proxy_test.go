package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServiceProxyPreservesGzipEncodedResponse(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("gzip mvt payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	want := compressed.Bytes()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(want)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/tiles/example/layer/0/0/0.mvt", nil)

	NewServiceProxy(upstream.URL).Handle(ctx)

	result := recorder.Result()
	defer result.Body.Close()
	got, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", result.Header.Get("Content-Encoding"))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("proxied gzip bytes changed: got %d bytes, want %d", len(got), len(want))
	}
}
