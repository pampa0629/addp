package runtimelog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fixture reads real Capture output. It never fabricates a current event.
func probeEntries(root string) []Entry {
	files, _ := filepath.Glob(filepath.Join(root, "runtime-probe", "*", "*.jsonl"))
	var entries []Entry
	for _, file := range files {
		body, _ := os.ReadFile(file)
		for _, line := range strings.Split(string(body), "\n") {
			var entry Entry
			if json.Unmarshal([]byte(line), &entry) == nil {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func TestProbeReceiverReusesSourceAndRejectsPreviousEvent(t *testing.T) {
	o := options(t)
	p := NewProbeReceiver(o)
	defer p.Close()
	var mu sync.Mutex
	var previous Entry
	stale := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/query_range" || r.Header.Get("Authorization") != "Bearer probe-read" {
			t.Error("invalid probe request")
			w.WriteHeader(403)
			return
		}
		mu.Lock()
		old, onlyOld := previous, stale
		mu.Unlock()
		var matches []Entry
		if onlyOld {
			matches = []Entry{old}
		} else {
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				for _, entry := range probeEntries(o.Root) {
					query := `{deployment="addp",module_name="runtime-probe"} | instance_id=` + strconv.Quote(entry.InstanceID) + ` |= ` + strconv.Quote(entry.Message)
					if r.URL.Query().Get("query") == query {
						matches = append(matches, entry)
					}
				}
				if len(matches) > 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
		values := make([][]string, 0, len(matches))
		for _, entry := range matches {
			body, _ := json.Marshal(entry)
			values = append(values, []string{"1", string(body)})
			mu.Lock()
			previous = entry
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"values": values}}}})
	}))
	defer server.Close()
	run := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := p.Probe(ctx, server.URL, "probe-read")
		return err
	}
	for i := 0; i < 40; i++ {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	stale = true
	mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	_, err := p.Probe(ctx, server.URL, "probe-read")
	cancel()
	if err != context.DeadlineExceeded {
		t.Fatalf("previous success accepted as current: %v", err)
	}
	mu.Lock()
	stale = false
	mu.Unlock()
	if err := run(); err != nil {
		t.Fatalf("probe did not recover: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	dirs, _ := os.ReadDir(filepath.Join(o.Root, "runtime-probe"))
	if len(dirs) != 1 {
		t.Fatalf("detections created %d source directories", len(dirs))
	}
	entries := probeEntries(o.Root)
	if len(entries) != 42 {
		t.Fatalf("lost admitted probe events: %d", len(entries))
	}
	seen := map[string]bool{}
	for i, entry := range entries {
		if entry.InstanceID != p.o.InstanceID || entry.ID != fmt.Sprintf("%s:%d", p.o.InstanceID, i+1) || seen[entry.Message] {
			t.Fatalf("probe identity or sequence reset: %#v", entry)
		}
		seen[entry.Message] = true
	}
	body, err := os.ReadFile(filepath.Join(o.Root, "runtime-probe", p.o.InstanceID, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Written          uint64    `json:"written"`
		Received         uint64    `json:"received"`
		Dropped          uint64    `json:"dropped"`
		CaptureStartedAt time.Time `json:"capture_started_at"`
		ObservedAt       time.Time `json:"observed_at"`
	}
	if json.Unmarshal(body, &status) != nil || status.Written != 42 || status.Received != 42 || status.Dropped != 0 || !status.ObservedAt.After(status.CaptureStartedAt) {
		t.Fatalf("capture lifecycle is not continuous: %s", body)
	}
	_, err = p.Probe(context.Background(), server.URL, "probe-read")
	if err == nil {
		t.Fatal("closed receiver accepted a detection")
	}
}

func TestProbeInvalidAndCancelledCallsDoNotEmit(t *testing.T) {
	o := options(t)
	p := NewProbeReceiver(o)
	if _, err := p.Probe(context.Background(), "file:///unsafe", "probe-read"); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Probe(ctx, "http://localhost:1", "probe-read"); err != context.Canceled {
		t.Fatalf("cancelled detection: %v", err)
	}
	p.Close()
	if entries := probeEntries(o.Root); len(entries) != 0 {
		t.Fatalf("invalid calls emitted %d events", len(entries))
	}
}

func TestProbeCloseCancelsInFlightAndWaitingDetections(t *testing.T) {
	o := options(t)
	p := NewProbeReceiver(o)
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	defer func() { p.Close(); server.Close() }()
	result := make(chan error, 2)
	go func() { _, err := p.Probe(context.Background(), server.URL, "probe-read"); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start request")
	}
	go func() { _, err := p.Probe(context.Background(), server.URL, "probe-read"); result <- err }()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("closed probe reported success")
			}
		case <-time.After(time.Second):
			t.Fatal("receiver closure left a detection running")
		}
	}
}
