package runtimelog

import (
	"encoding/json"
	"errors"
	"github.com/addp/common/logpipeline"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const SourceSchema = "addp.log-sources/v2"
const MaxSources = 2048
const MaxSourceScanEntries = 10000

// SourceIssue contains only bounded evidence, never paths or filesystem errors.
type SourceIssue struct {
	Code  string `json:"code"`
	Count int    `json:"count,omitempty"`
}

var sourceIssueCodes = []string{"metadata_missing", "metadata_invalid", "entry_unreadable", "unsafe_entry", "scan_limit", "source_limit"}

// Source describes the receiver, never the business process start or outcome.
type Source struct {
	Module           string    `json:"module_name"`
	InstanceID       string    `json:"instance_id"`
	Role             string    `json:"role"`
	Node             string    `json:"host_node_name"`
	CaptureStartedAt time.Time `json:"capture_started_at"`
	ObservedAt       time.Time `json:"observed_at"`
}
type SourceReport struct {
	Schema    string        `json:"schema"`
	Node      string        `json:"node"`
	BootID    string        `json:"boot_id"`
	Sequence  uint64        `json:"sequence"`
	SampledAt time.Time     `json:"sampled_at"`
	Complete  bool          `json:"complete"`
	Sources   []Source      `json:"sources"`
	Issues    []SourceIssue `json:"issues"`
}

func (s Source) valid(node string, now time.Time) bool {
	role := s.Role == "backend" || s.Role == "worker" || s.Role == "scheduler" || s.Role == "ingress"
	return identifier.MatchString(s.Module) && identifier.MatchString(s.InstanceID) && s.Node == node && role && !s.CaptureStartedAt.IsZero() && s.CaptureStartedAt.Equal(s.CaptureStartedAt.Truncate(time.Microsecond)) && s.ObservedAt.Equal(s.ObservedAt.Truncate(time.Microsecond)) && !s.ObservedAt.Before(s.CaptureStartedAt) && !s.ObservedAt.After(now.Add(30*time.Second))
}
func (r SourceReport) Validate(now time.Time) error {
	if r.Schema != SourceSchema || !logpipeline.Identity.MatchString(r.Node) || !identifier.MatchString(r.BootID) || r.Sequence == 0 || r.Sequence > 1<<63-1 || r.SampledAt.Before(now.Add(-time.Minute)) || r.SampledAt.After(now.Add(30*time.Second)) || r.Sources == nil || len(r.Sources) > MaxSources || r.Issues == nil || len(r.Issues) > len(sourceIssueCodes) || r.Complete != (len(r.Issues) == 0) {
		return errors.New("invalid log source report")
	}
	issueSeen, total := map[string]bool{}, 0
	for _, issue := range r.Issues {
		known := false
		for _, code := range sourceIssueCodes {
			if issue.Code == code {
				known = true
				break
			}
		}
		if !known || issueSeen[issue.Code] || issue.Count < 1 || issue.Count > MaxSourceScanEntries+1 {
			return errors.New("invalid log source issue")
		}
		issueSeen[issue.Code] = true
		total += issue.Count
	}
	if total > MaxSourceScanEntries+1 {
		return errors.New("invalid log source issue count")
	}
	seen := map[string]bool{}
	for _, s := range r.Sources {
		if !s.valid(r.Node, r.SampledAt) || seen[s.InstanceID] || s.Module == "housekeeper" || s.Module == "runtime-probe" {
			return errors.New("invalid log source")
		}
		seen[s.InstanceID] = true
	}
	return nil
}

// DiscoverSources keeps historical receiver metadata separate from health receivers.
// A failed or limited scan cannot be represented as an authoritative empty catalog.
func DiscoverSources(o Options, node, boot string, seq uint64) SourceReport {
	r := SourceReport{Schema: SourceSchema, Node: node, BootID: boot, Sequence: seq, SampledAt: time.Now().UTC(), Complete: true, Sources: []Source{}, Issues: []SourceIssue{}}
	counts := map[string]int{}
	visited := 0
	err := filepath.WalkDir(o.Root, func(path string, d fs.DirEntry, err error) error {
		visited++
		if err != nil {
			counts["entry_unreadable"]++
			return err
		}
		if visited > MaxSourceScanEntries {
			counts["scan_limit"]++
			return errors.New("source scan limit")
		}
		if d.Type()&os.ModeSymlink != 0 {
			counts["unsafe_entry"]++
			return errors.New("invalid source tree")
		}
		if d.IsDir() || d.Name() != "status.json" {
			return nil
		}
		module := filepath.Base(filepath.Dir(filepath.Dir(path)))
		if module == "housekeeper" || module == "runtime-probe" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			counts["entry_unreadable"]++
			return nil
		}
		if !info.Mode().IsRegular() {
			counts["unsafe_entry"]++
			return nil
		}
		if info.Size() > 8192 {
			counts["metadata_invalid"]++
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			counts["entry_unreadable"]++
			return nil
		}
		var s Source
		decoder := json.NewDecoder(io.LimitReader(file, 8193))
		err = decoder.Decode(&s)
		if err == nil && decoder.Decode(new(any)) != io.EOF {
			err = errors.New("invalid source metadata")
		}
		file.Close()
		if err != nil {
			counts["metadata_invalid"]++
			return nil
		}
		if s.Module == "" || s.InstanceID == "" || s.Role == "" || s.Node == "" || s.CaptureStartedAt.IsZero() || s.ObservedAt.IsZero() {
			counts["metadata_missing"]++
			return nil
		}
		if !s.valid(node, r.SampledAt) || s.Module != module || s.InstanceID != filepath.Base(filepath.Dir(path)) {
			counts["metadata_invalid"]++
			return nil
		}
		if len(r.Sources) == MaxSources {
			counts["source_limit"]++
			return errors.New("source catalog scan limit")
		}
		r.Sources = append(r.Sources, s)
		return nil
	})
	if err != nil {
		r.Complete = false
	}
	for _, code := range sourceIssueCodes {
		if count := counts[code]; count > 0 {
			r.Issues = append(r.Issues, SourceIssue{Code: code, Count: count})
		}
	}
	r.Complete = len(r.Issues) == 0
	return r
}
