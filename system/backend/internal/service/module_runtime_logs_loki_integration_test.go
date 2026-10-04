package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/runtimelog"
)

// Runs only inside the owner-created disposable Compose gate, never against
// the developer's shared Loki. The gate owns all storage and its teardown.
func TestRuntimeLogsAgainstLoki(t *testing.T) {
	if os.Getenv("ADDP_RUNTIME_LOG_INTEGRATION") != "1" {
		t.Skip("requires make test-system-runtime-log")
	}
	endpoint := os.Getenv("LOKI_TEST_URL")
	project := os.Getenv("LOKI_TEST_PROJECT")
	if endpoint == "" || project == "" || os.Getenv("LOKI_READ_TOKEN") == "" || os.Getenv("LOKI_WRITE_TOKEN") == "" {
		t.Fatal("missing owned Loki fixture")
	}
	s := NewRuntimeLogService(endpoint, os.Getenv("LOKI_READ_TOKEN"), logTestKey)
	if phase := os.Getenv("ADDP_RUNTIME_LOG_PHASE"); strings.HasPrefix(phase, "crash-") {
		verifyCrashedProducerLogs(t, s, phase)
		return
	}
	stamp := time.Now().UTC().Add(-5 * time.Minute)
	q := RuntimeLogQuery{From: stamp.Add(-10 * time.Nanosecond), To: stamp.Add(10 * time.Nanosecond), Limit: 200, UserID: 1}
	if os.Getenv("ADDP_RUNTIME_LOG_PHASE") == "outage" {
		if result, err := s.Query(context.Background(), "manager", "owned-outage", q); err == nil || result != nil {
			t.Fatal("real disconnected query became empty success")
		}
		return
	}
	instance := fmt.Sprintf("owned-%s-%d", project, time.Now().UnixNano())
	makeEntry := func(seq uint64, ts time.Time) runtimelog.Entry {
		e := testLog(seq, ts)
		e.InstanceID = instance
		e.ID = fmt.Sprintf("%s:%d", instance, seq)
		e.Role = "backend"
		e.Node = "trusted-test-node"
		return e
	}
	entries := []runtimelog.Entry{}
	for i := 1; i <= 1000; i++ {
		entries = append(entries, makeEntry(uint64(i), stamp))
	}
	entries = append(entries, makeEntry(1001, stamp.Add(-time.Nanosecond)), makeEntry(1002, stamp.Add(-2*time.Nanosecond)), makeEntry(1003, q.From.Add(-time.Nanosecond)), makeEntry(1004, q.To), makeEntry(1005, q.From), makeEntry(1006, q.To.Add(-time.Nanosecond)))
	push := func(entries []runtimelog.Entry) {
		t.Helper()
		streams := []map[string]any{}
		for roleIndex := 0; roleIndex < 2; roleIndex++ {
			values := [][]any{}
			for i, e := range entries {
				if i%2 != roleIndex {
					continue
				}
				ts, err := time.Parse(time.RFC3339Nano, e.Timestamp)
				if err != nil {
					t.Fatal(err)
				}
				body, err := json.Marshal(e)
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, []any{strconv.FormatInt(ts.UnixNano(), 10), string(body), map[string]string{"instance_id": e.InstanceID, "level": e.Level, "entry_id": e.ID, "node_name": e.Node}})
			}
			streams = append(streams, map[string]any{"stream": map[string]string{"deployment": "addp", "module_name": "manager", "role": []string{"backend", "worker"}[roleIndex]}, "values": values})
		}
		body, err := json.Marshal(map[string]any{"streams": streams})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, endpoint+"/loki/api/v1/push", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+os.Getenv("LOKI_WRITE_TOKEN"))
		client := http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatalf("fixture push status=%d", resp.StatusCode)
		}
	}
	push(entries)
	deadline := time.Now().Add(15 * time.Second)
	var first *RuntimeLogResult
	for {
		var err error
		first, err = s.Query(context.Background(), "manager", instance, q)
		if err == nil && first.Returned == 200 && first.Entries[199].ID == fmt.Sprintf("%s:1006", instance) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned storage did not become visible: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	seen := map[string]bool{}
	cursor := ""
	batches := 0
	for {
		q.Cursor = cursor
		result, err := s.Query(context.Background(), "manager", instance, q)
		if err != nil {
			t.Fatal(err)
		}
		batches++
		if batches > 10 || result.Returned == 0 {
			t.Fatal("pagination failed to progress")
		}
		for _, e := range result.Entries {
			if seen[e.ID] {
				t.Fatalf("duplicate across batches %s", e.ID)
			}
			seen[e.ID] = true
		}
		if !result.From.Equal(q.From) || !result.To.Equal(q.To) {
			t.Fatal("window drifted")
		}
		if !result.HasMore {
			break
		}
		cursor = result.NextCursor
	}
	if len(seen) != 1004 || seen[fmt.Sprintf("%s:1003", instance)] || seen[fmt.Sprintf("%s:1004", instance)] || !seen[fmt.Sprintf("%s:1005", instance)] || !seen[fmt.Sprintf("%s:1006", instance)] {
		t.Fatalf("real Loki lost boundary records: %d", len(seen))
	}
	q.Cursor = first.NextCursor
	// A late record newer than the already-read boundary is discovered by refresh.
	push([]runtimelog.Entry{makeEntry(6000, q.To.Add(-time.Nanosecond))})
	second, err := s.Query(context.Background(), "manager", instance, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range second.Entries {
		if entry.ID == fmt.Sprintf("%s:6000", instance) {
			t.Fatal("late record entered an older cursor batch")
		}
	}
	q.Cursor = ""
	latest, err := s.Query(context.Background(), "manager", instance, q)
	if err != nil || latest.Entries[len(latest.Entries)-1].ID != fmt.Sprintf("%s:6000", instance) {
		t.Fatalf("refresh did not recover late record: %v", err)
	}
	q.Node = "foreign-node"
	wrongNode, err := s.Query(context.Background(), "manager", instance, q)
	if err != nil || len(wrongNode.Entries) != 0 {
		t.Fatal("foreign node matched trusted source", err)
	}
	q.Node = "trusted-test-node"
	boundNode, err := s.Query(context.Background(), "manager", instance, q)
	if err != nil || len(boundNode.Entries) == 0 {
		t.Fatal("trusted node logs unavailable", err)
	}
	q.Node = ""
	// Exercise real LogQL literal escaping and structured metadata filtering.
	literal := `"} |~ ".*`
	filtered := makeEntry(6001, stamp.Add(time.Nanosecond))
	filtered.Message = literal
	push([]runtimelog.Entry{filtered})
	q.Keyword, q.Level = literal, "unknown"
	// Keyword matching uses the canonical JSON line, as in the existing API.
	// Plain quotes must not accidentally become an executable LogQL expression.
	noInjection, err := s.Query(context.Background(), "manager", instance, q)
	if err != nil || noInjection.Returned != 0 {
		t.Fatalf("literal became expression: %v", err)
	}
	q.Keyword = strings.ReplaceAll(literal, `"`, `\"`)
	match, err := s.Query(context.Background(), "manager", instance, q)
	if err != nil || match.Returned != 1 || match.Entries[0].ID != filtered.ID || match.HasMore {
		t.Fatalf("real literal/level filter failed: %v", err)
	}
	q.Keyword, q.Level = "", ""
	dense := []runtimelog.Entry{}
	instance += "-dense"
	for i := 1; i <= 1001; i++ {
		dense = append(dense, makeEntry(uint64(i), stamp))
	}
	push(dense)
	if result, err := s.Query(context.Background(), "manager", instance, q); result != nil || !errors.Is(err, ErrRuntimeLogsBoundary) {
		t.Fatalf("dense real Loki timestamp falsely completed: %v", err)
	}
	t.Logf("Real Loki multi-stream paging passed: %d distinct logs, %d batches, nanosecond boundaries, late refresh and dense-boundary rejection", len(seen), batches)
}

// The gate checks the owned container's exit code separately. Source metadata
// alone is not evidence of process exit or a DOWN registration.
func verifyCrashedProducerLogs(t *testing.T, s *RuntimeLogService, phase string) {
	t.Helper()
	root := os.Getenv("LOKI_TEST_SOURCE")
	if root == "" {
		t.Fatal("missing owned source directory")
	}
	want := map[string]string{
		"runtime-t2-crash-stdout": "stdout",
		"runtime-t2-crash-stderr": "stderr",
	}
	if phase == "crash-restarted" {
		want["runtime-t2-after-crash"] = "stdout"
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		found := map[string]runtimelog.Entry{}
		files, err := filepath.Glob(filepath.Join(root, "manager", "*", "*.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range bytes.Split(body, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var entry runtimelog.Entry
				// A live writer can have an incomplete last line. The bounded
				// retry still requires every expected complete record.
				if json.Unmarshal(line, &entry) == nil {
					if _, expected := want[entry.Message]; expected {
						found[entry.Message] = entry
					}
				}
			}
		}
		complete := len(found) == len(want)
		for message, channel := range want {
			entry, ok := found[message]
			if !ok {
				continue
			}
			if entry.Channel != channel || entry.Node != "observer-t2" || entry.InstanceID == "" {
				t.Fatalf("invalid crash source identity/channel for %s", message)
			}
			q := RuntimeLogQuery{From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC(), Node: entry.Node, Limit: 100, UserID: 1}
			result, err := s.Query(context.Background(), "manager", entry.InstanceID, q)
			if err != nil {
				t.Fatal(err)
			}
			matched := false
			for _, record := range result.Entries {
				if record.InstanceID != entry.InstanceID || record.Node != entry.Node {
					t.Fatal("crash query leaked another instance/node")
				}
				if record.Message == message && record.Channel == channel && record.ID == entry.ID {
					matched = true
				}
				if message == "runtime-t2-after-crash" && strings.HasPrefix(record.Message, "runtime-t2-crash-") || message != "runtime-t2-after-crash" && record.Message == "runtime-t2-after-crash" {
					t.Fatal("crashed and replacement process logs were mixed")
				}
			}
			complete = complete && matched
		}
		if complete {
			old := found["runtime-t2-crash-stdout"].InstanceID
			if old != found["runtime-t2-crash-stderr"].InstanceID {
				t.Fatal("stdout and stderr lost the shared process identity")
			}
			if phase == "crash-restarted" && old == found["runtime-t2-after-crash"].InstanceID {
				t.Fatal("replacement reused the crashed process identity")
			}
			t.Logf("%s: actual producer logs readable through System query service with instance/node isolation", phase)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s producer logs did not become queryable", phase)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
