package sourceserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceRejectsQueriesBodiesAndParallelReadsWithoutLeakingFailedData(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	handler := Handler(func(context.Context) ([]byte, error) {
		close(entered)
		<-release
		return []byte("private_partial_sample 1\n"), errors.New("private upstream credential")
	})
	for _, path := range []string{"/health", "/metrics?", "/metrics?target=other", "/%6detrics", "/metrics/"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 404 {
			t.Fatalf("unexpected route accepted: %s", path)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodHead, http.MethodPut} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, "/metrics", nil))
		if w.Code != 404 {
			t.Fatalf("unexpected method accepted: %s", method)
		}
	}
	for _, chunked := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/metrics", strings.NewReader("target=other"))
		if chunked {
			r.ContentLength = -1
			r.TransferEncoding = []string{"chunked"}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("source accepted a request body")
		}
	}
	first, done := httptest.NewRecorder(), make(chan struct{})
	go func() { handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/metrics", nil)); close(done) }()
	<-entered
	busy := httptest.NewRecorder()
	handler.ServeHTTP(busy, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	close(release)
	<-done
	for _, w := range []*httptest.ResponseRecorder{first, busy} {
		if w.Code != 503 || strings.Contains(w.Body.String(), "private") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("failed or busy source leaked data or lost no-store")
		}
	}
}

func TestSourceSuccessfulReadDoesNotSurviveFollowingFailure(t *testing.T) {
	calls := 0
	handler := Handler(func(context.Context) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("fixed_metric 12\n"), nil
		}
		return nil, errors.New("upstream failed")
	})
	first, next := httptest.NewRecorder(), httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	handler.ServeHTTP(next, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if first.Code != 200 || first.Body.String() != "fixed_metric 12\n" || next.Code != 503 || strings.Contains(next.Body.String(), "fixed_metric") {
		t.Fatal("source returned stale data after failure")
	}
}
