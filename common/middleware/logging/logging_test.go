package logging_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/logging"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/common/runtimelog"
	"github.com/gin-gonic/gin"
)

func TestCompletionDiagnosticsSurviveRuntimeCaptureBurst(t *testing.T) {
	const (
		requests = 990
		start    = "1793948995000000001"
		finish   = "1793948995000000002"
	)
	var output bytes.Buffer
	logger.Init(logger.Options{Writer: &output})
	t.Cleanup(func() { logger.Init(logger.Options{}) })
	router := modulelifecycle.NewRouter()
	router.POST("/preview", func(c *gin.Context) {
		logging.SetCompletionDiagnostics(c, slog.Group("source_rule_query",
			slog.Int("backend_pid", 23), slog.String("started_unix_ns", start),
			slog.String("finished_unix_ns", finish)),
			slog.Group("auth_context_ns", slog.Group("route", slog.Int64("session_snapshot", 1234567))))
		c.Status(http.StatusOK)
	})
	router.GET("/other", func(c *gin.Context) { c.Status(http.StatusOK) })

	var requestIDs [requests]string
	var workers sync.WaitGroup
	for worker := range 10 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := worker; index < requests; index += 10 {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/preview", nil))
				requestIDs[index] = w.Header().Get("X-Request-ID")
			}
		}()
	}
	workers.Wait()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/other", nil))

	root := t.TempDir()
	options := runtimelog.Options{Root: root, Module: "system", Role: "backend", InstanceID: "burst",
		SegmentBytes: 4 << 20, InstanceBytes: 8 << 20, NodeBytes: 16 << 20, SourceAge: time.Hour}
	if err := runtimelog.Capture(options, &output, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "system", "burst", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("capture files=%v err=%v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	pending := make(map[string]bool, requests)
	for _, id := range requestIDs {
		if id == "" || pending[id] {
			t.Fatalf("invalid request ID %q", id)
		}
		pending[id] = true
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var ordinary int
	for scanner.Scan() {
		var entry runtimelog.Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		var event struct {
			Message     string                                `json:"msg"`
			RequestID   string                                `json:"request_id"`
			Route       string                                `json:"route"`
			Diagnostics map[string]map[string]json.RawMessage `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(entry.Message), &event); err != nil {
			t.Fatal(err)
		}
		if event.Message != "http.request.completed" || event.RequestID != entry.RequestID {
			t.Fatalf("unexpected record: %s", entry.Message)
		}
		if event.Route == "/other" && event.Diagnostics == nil {
			ordinary++
			continue
		}
		if event.Route != "/preview" || !pending[entry.RequestID] {
			t.Fatalf("unexpected or duplicate completion for %q", entry.RequestID)
		}
		timing := event.Diagnostics["source_rule_query"]
		if len(timing) != 3 || string(timing["backend_pid"]) != "23" ||
			string(timing["started_unix_ns"]) != `"`+start+`"` || string(timing["finished_unix_ns"]) != `"`+finish+`"` {
			t.Fatalf("missing or altered timing: %s", entry.Message)
		}
		if string(event.Diagnostics["auth_context_ns"]["route"]) != `{"session_snapshot":1234567}` {
			t.Fatalf("missing or redacted session snapshot duration: %s", entry.Message)
		}
		delete(pending, entry.RequestID)
	}
	if err := scanner.Err(); err != nil || len(pending) != 0 || ordinary != 1 {
		t.Fatalf("missing=%d ordinary=%d scan error=%v", len(pending), ordinary, err)
	}
}
