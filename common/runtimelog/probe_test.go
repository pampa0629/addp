package runtimelog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	confirm := func(ctx context.Context, instance, marker string) (bool, error) {
		mu.Lock()
		old, onlyOld := previous, stale
		mu.Unlock()
		message := "runtime-log-delivery-probe " + marker
		if onlyOld {
			return old.InstanceID == instance && old.Message == message, nil
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			for _, entry := range probeEntries(o.Root) {
				if entry.InstanceID == instance && entry.Message == message {
					mu.Lock()
					previous = entry
					mu.Unlock()
					return true, nil
				}
			}
			time.Sleep(time.Millisecond)
		}
		return false, ctx.Err()
	}
	run := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := p.Probe(ctx, confirm)
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
	_, err := p.Probe(ctx, confirm)
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
	_, err = p.Probe(context.Background(), confirm)
	if err == nil {
		t.Fatal("closed receiver accepted a detection")
	}
}

func TestProbeInvalidAndCancelledCallsDoNotEmit(t *testing.T) {
	o := options(t)
	p := NewProbeReceiver(o)
	if _, err := p.Probe(context.Background(), nil); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Probe(ctx, func(context.Context, string, string) (bool, error) {
		t.Fatal("cancelled probe queried")
		return false, nil
	}); err != context.Canceled {
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
	confirm := func(ctx context.Context, instance, marker string) (bool, error) {
		close(entered)
		<-ctx.Done()
		return false, ctx.Err()
	}
	defer p.Close()
	result := make(chan error, 2)
	go func() { _, err := p.Probe(context.Background(), confirm); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start request")
	}
	go func() { _, err := p.Probe(context.Background(), confirm); result <- err }()
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
