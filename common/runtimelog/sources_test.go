package runtimelog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourcesPreserveExitedAndSilentReceiversWithoutProcessOutcome(t *testing.T) {
	o := options(t)
	o.Node = "host.example"
	for _, output := range []string{"startup failed\n", ""} {
		o.InstanceID += "x"
		if err := Capture(o, strings.NewReader(output), strings.NewReader("")); err != nil {
			t.Fatal(err)
		}
	}
	r := DiscoverSources(o, o.Node, "boot-1", 1)
	if !r.Complete || len(r.Sources) != 2 || r.Validate(time.Now()) != nil {
		t.Fatalf("invalid sources %+v", r)
	}
	for _, s := range r.Sources {
		if s.Role != o.Role || s.Module != o.Module || s.CaptureStartedAt.IsZero() {
			t.Fatalf("missing trusted metadata %+v", s)
		}
	}
	// Metadata older than the health receiver horizon must remain discoverable.
	p := filepath.Join(o.Root, o.Module, o.InstanceID, "status.json")
	body, _ := os.ReadFile(p)
	var status map[string]any
	json.Unmarshal(body, &status)
	status["observed_at"] = time.Now().Truncate(time.Microsecond).Add(-time.Hour).Format(time.RFC3339Nano)
	status["capture_started_at"] = time.Now().Truncate(time.Microsecond).Add(-2 * time.Hour).Format(time.RFC3339Nano)
	body, _ = json.Marshal(status)
	os.WriteFile(p, body, 0600)
	r = DiscoverSources(o, o.Node, "boot-1", 2)
	if !r.Complete || len(r.Sources) != 2 {
		t.Fatal("exited source was lost")
	}
	status["host_node_name"] = "foreign"
	body, _ = json.Marshal(status)
	os.WriteFile(p, body, 0600)
	r = DiscoverSources(o, o.Node, "boot-1", 3)
	if r.Complete || len(r.Sources) != 1 {
		t.Fatal("foreign source accepted or scan falsely complete")
	}
}

func TestSourceDiscoveryRejectsSymlinksAndInvalidReports(t *testing.T) {
	o := options(t)
	o.Node = "host"
	if err := os.Symlink(t.TempDir(), filepath.Join(o.Root, "escape")); err != nil {
		t.Fatal(err)
	}
	r := DiscoverSources(o, o.Node, "boot", 1)
	if r.Complete {
		t.Fatal("symlink accepted")
	}
	if len(r.Issues) != 1 || r.Issues[0].Code != "unsafe_entry" || r.Issues[0].Count != 1 || r.Validate(time.Now()) != nil {
		t.Fatalf("unsafe scan diagnostic missing: %+v", r)
	}
	r.Sequence = 0
	if r.Validate(time.Now()) == nil {
		t.Fatal("zero sequence accepted")
	}
}

func TestSourceDiscoveryClassifiesEvidenceAndLimits(t *testing.T) {
	o := options(t)
	o.Node = "host"
	write := func(id string, body []byte) {
		t.Helper()
		p := filepath.Join(o.Root, "copilot", id)
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "status.json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("missing", []byte(`{"instance_id":"missing"}`))
	write("broken", []byte(`{"role":`))
	write("oversized", []byte(strings.Repeat("x", 8193)))
	now := time.Now().UTC().Truncate(time.Microsecond)
	body, _ := json.Marshal(Source{Module: "copilot", InstanceID: "foreign", Role: "backend", Node: "other", CaptureStartedAt: now, ObservedAt: now})
	write("foreign", body)
	r := DiscoverSources(o, o.Node, "boot", 1)
	if r.Complete || len(r.Sources) != 0 || len(r.Issues) != 2 || r.Issues[0] != (SourceIssue{Code: "metadata_missing", Count: 1}) || r.Issues[1] != (SourceIssue{Code: "metadata_invalid", Count: 3}) || r.Validate(time.Now()) != nil {
		t.Fatalf("metadata evidence misclassified: %+v", r)
	}
	o.Root = filepath.Join(t.TempDir(), "absent")
	r = DiscoverSources(o, o.Node, "boot", 2)
	if len(r.Issues) != 1 || r.Issues[0].Code != "entry_unreadable" || r.Validate(time.Now()) != nil {
		t.Fatalf("root failure hidden: %+v", r)
	}
	for _, tc := range []struct {
		name  string
		count int
	}{{"source_limit", MaxSources + 1}, {"scan_limit", MaxSourceScanEntries + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			o.Root = t.TempDir()
			for i := 0; i < tc.count; i++ {
				id := fmt.Sprintf("instance-%05d", i)
				if tc.name == "scan_limit" {
					if err := os.WriteFile(filepath.Join(o.Root, id), []byte{}, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					body, _ := json.Marshal(Source{Module: "copilot", InstanceID: id, Role: "backend", Node: o.Node, CaptureStartedAt: now, ObservedAt: now})
					write(id, body)
				}
			}
			r := DiscoverSources(o, o.Node, "boot", 3)
			if r.Complete || len(r.Issues) != 1 || r.Issues[0] != (SourceIssue{Code: tc.name, Count: 1}) || r.Validate(time.Now()) != nil {
				t.Fatalf("limit hidden: %+v", r)
			}
		})
	}
}

func TestSourceReportRejectsUnboundedOrContradictoryDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name     string
		schema   string
		complete bool
		issues   []SourceIssue
	}{
		{"old protocol", "addp.log-sources/v1", true, []SourceIssue{}},
		{"missing evidence", SourceSchema, false, nil},
		{"false complete", SourceSchema, true, []SourceIssue{{Code: "scan_limit", Count: 1}}},
		{"false incomplete", SourceSchema, false, []SourceIssue{}},
		{"unknown code", SourceSchema, false, []SourceIssue{{Code: "arbitrary-path", Count: 1}}},
		{"no count", SourceSchema, false, []SourceIssue{{Code: "metadata_invalid"}}},
		{"negative count", SourceSchema, false, []SourceIssue{{Code: "metadata_invalid", Count: -1}}},
		{"unbounded count", SourceSchema, false, []SourceIssue{{Code: "metadata_invalid", Count: MaxSourceScanEntries + 2}}},
		{"duplicate", SourceSchema, false, []SourceIssue{{Code: "metadata_invalid", Count: 1}, {Code: "metadata_invalid", Count: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := SourceReport{Schema: tc.schema, Node: "host", BootID: "boot", Sequence: 1, SampledAt: time.Now(), Sources: []Source{}, Complete: tc.complete, Issues: tc.issues}
			if r.Validate(time.Now()) == nil {
				t.Fatal("invalid diagnostics accepted")
			}
		})
	}
}
