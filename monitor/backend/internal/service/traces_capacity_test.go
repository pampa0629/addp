package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/httptrace"
	"github.com/addp/monitor/internal/tracequery"
)

// The standard gate executes this entire fixture inside one CPU/memory-limited
// disposable container. It never drives the developer's registered services.
func TestOwnedTraceCapacity(t *testing.T) {
	if os.Getenv("TRACE_CAPACITY_ENABLED") != "true" {
		t.Skip("explicit capacity gate only")
	}
	w := os.Getenv("TRACE_T2_WORK")
	users := &ownedTraceUsers{}
	var captures sync.WaitGroup
	t.Cleanup(captures.Wait)
	leaf := startOwnedTrace(t, w, users, &captures, "system", "backend", "", 3)
	middle := startOwnedTrace(t, w, users, &captures, "monitor", "backend", leaf, 2)
	gateway := startOwnedTrace(t, w, users, &captures, "gateway", "ingress", middle, 1)
	write := func(name string, value any) {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(w, name+".json")
		if err = os.WriteFile(path+".tmp", b, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(path+".tmp", path); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(name string) {
		for deadline := time.Now().Add(12 * time.Minute); time.Now().Before(deadline); {
			if _, err := os.Stat(filepath.Join(w, name)); err == nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("capacity controller deadline: " + name)
	}
	client := &http.Client{Timeout: 6 * time.Second}
	counters := func() []httptrace.Counters {
		result := []httptrace.Counters{}
		for _, addr := range []string{gateway, middle, leaf} {
			response, err := client.Get(addr + "/health/ready")
			if err != nil {
				t.Fatal(err)
			}
			var c httptrace.Counters
			err = json.NewDecoder(response.Body).Decode(&c)
			response.Body.Close()
			if err != nil || c.Dropped != 0 || c.ExportFailed != 0 {
				t.Fatalf("source loss: %+v %v", c, err)
			}
			result = append(result, c)
		}
		return result
	}
	qc, err := httptrace.TLSHTTPClient(filepath.Join(w, "query-tls"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := tracequery.NewClient(os.Getenv("TRACE_T2_QUERY"), qc)
	if err != nil {
		t.Fatal(err)
	}
	service := NewTraceService(users, backend)
	write("capacity-ready", map[string]bool{"ready": true})
	for phase, model := range []struct{ rps, failure, slow int }{{50, 5, 5}, {70, 30, 10}} {
		n := phase + 1
		wait(fmt.Sprintf("capacity-start-%d", n))
		before := counters()
		from := time.Now().UTC()
		start := time.Now()
		var mu sync.Mutex
		var requests sync.WaitGroup
		permits := make(chan struct{}, 64)
		counts := map[string]int{"ordinary": 0, "failure": 0, "slow": 0, "transport_errors": 0, "bad_response": 0, "schedule_misses": 0}
		for i := 0; i < model.rps*600; i++ {
			due := start.Add(time.Duration(i) * time.Second / time.Duration(model.rps))
			if delay := time.Until(due); delay > 0 {
				time.Sleep(delay)
			}
			if time.Since(due) > 200*time.Millisecond {
				mu.Lock()
				counts["schedule_misses"]++
				mu.Unlock()
				continue
			}
			select {
			case permits <- struct{}{}:
			default:
				mu.Lock()
				counts["schedule_misses"]++
				mu.Unlock()
				continue
			}
			kind := "ordinary"
			code := 200
			// Spread the classes across each 100 requests instead of issuing bursts.
			bucket := (i * 37) % 100
			if bucket < model.failure {
				kind = "failure"
				code = 503
			} else if bucket < model.failure+model.slow {
				kind = "slow"
			}
			requests.Add(1)
			go func(kind string, code int) {
				defer requests.Done()
				defer func() { <-permits }()
				began := time.Now()
				response, e := client.Get(gateway + "/items/" + kind)
				mu.Lock()
				defer mu.Unlock()
				if e != nil {
					counts["transport_errors"]++
					return
				}
				defer response.Body.Close()
				var body struct {
					ID string `json:"trace_id"`
				}
				e = json.NewDecoder(response.Body).Decode(&body)
				if e != nil || !httptrace.ParseTraceID(body.ID) || response.StatusCode != code || (kind == "slow" && time.Since(began) < time.Second) {
					counts["bad_response"]++
					return
				}
				counts[kind]++
			}(kind, code)
		}
		requests.Wait()
		until := time.Now().UTC()
		write(fmt.Sprintf("capacity-loaded-%d", n), map[string]any{"start": from, "end": until, "duration_seconds": until.Sub(from).Seconds(), "rps": model.rps, "expected_requests": model.rps * 600, "counts": counts, "source_before": before})
		if counts["transport_errors"] != 0 || counts["bad_response"] != 0 || counts["schedule_misses"] != 0 {
			t.Fatalf("capacity load was not achieved: %+v", counts)
		}
		wait(fmt.Sprintf("capacity-verify-%d", n))
		after := counters()
		queryStarted := time.Now()
		result, e := service.Query(context.Background(), "capacity-user", "addp_at_t2_current_user", TraceQuery{InstanceID: 1, Start: from.Add(-time.Second), End: until.Add(time.Second), Limit: 20})
		querySeconds := time.Since(queryStarted).Seconds()
		if e != nil || len(result.Data) < 20 || querySeconds > 5 {
			t.Fatalf("capacity retained query insufficient: %v count=%d seconds=%.3f", e, len(result.Data), querySeconds)
		}
		for _, d := range result.Data {
			if d.Partial || len(d.Spans) != 5 || len(d.Edges) != 2 {
				t.Fatalf("capacity incomplete chain: %s spans=%d edges=%d partial=%v", d.ID, len(d.Spans), len(d.Edges), d.Partial)
			}
		}
		write(fmt.Sprintf("capacity-done-%d", n), map[string]any{"source_after": after, "queried_complete_traces": len(result.Data), "query_seconds": querySeconds})
	}
}
