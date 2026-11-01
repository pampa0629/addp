package redissource

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSourceHTTPFixedProjectionAndCurrentFailure(t *testing.T) {
	snapshot, err := Parse(fixture(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fail := false
	handler := Handler(func(context.Context) (Snapshot, error) {
		if fail {
			return Snapshot{}, errors.New("private-upstream-content")
		}
		return snapshot, nil
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "addp_redis_memory_allocated_bytes 1000") || !strings.Contains(w.Body.String(), "9007199254740993") || strings.Contains(w.Body.String(), snapshot.RunID) || strings.Contains(w.Body.String(), "private") {
		t.Fatal("fixed source projection changed or leaked metadata")
	}
	fail = true
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "addp_redis_") || strings.Contains(w.Body.String(), "private") {
		t.Fatal("failure retained metrics or leaked upstream error")
	}
}

func TestSourceHTTPRejectsOtherMethodsPathsQueriesAndConcurrentReads(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	handler := Handler(func(context.Context) (Snapshot, error) {
		entered <- struct{}{}
		<-release
		return Parse(fixture(), time.Now())
	})
	for _, item := range [][2]string{{"POST", "/metrics"}, {"GET", "/metrics?target=redis"}, {"GET", "/metrics?"}, {"GET", "/health"}, {"GET", "/%6detrics"}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(item[0], item[1], nil))
		if w.Code != 404 {
			t.Fatal("source opened an additional endpoint")
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/metrics", nil))
	}()
	<-entered
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 503 {
		t.Fatal("concurrent Redis query was allowed")
	}
	close(release)
	<-done
}
