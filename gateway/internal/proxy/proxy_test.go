package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
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
	router := gin.New()
	router.Any("/*path", NewServiceProxy(upstream.URL).Handle)
	gateway := httptest.NewServer(router)
	defer gateway.Close()

	request, err := http.NewRequest(http.MethodGet, gateway.URL+"/tiles/example/layer/0/0/0.mvt", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept-Encoding", "gzip")
	result, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
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

func TestServiceProxyForwardsWebSocketUpgrade(t *testing.T) {
	upstream := httptest.NewServer(websocket.Handler(func(connection *websocket.Conn) {
		defer connection.Close()
		var message string
		if err := websocket.Message.Receive(connection, &message); err != nil {
			return
		}
		_ = websocket.Message.Send(connection, "echo:"+message)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Any("/*path", NewServiceProxy(upstream.URL).Handle)
	gateway := httptest.NewServer(router)
	defer gateway.Close()

	websocketURL := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/api/kernels/1/channels"
	connection, err := websocket.Dial(websocketURL, "", gateway.URL)
	if err != nil {
		t.Fatalf("dial proxied websocket: %v", err)
	}
	defer connection.Close()
	if err := websocket.Message.Send(connection, "ping"); err != nil {
		t.Fatalf("send websocket message: %v", err)
	}
	var got string
	if err := websocket.Message.Receive(connection, &got); err != nil {
		t.Fatalf("receive websocket message: %v", err)
	}
	if got != "echo:ping" {
		t.Fatalf("websocket response = %q, want echo:ping", got)
	}
}
