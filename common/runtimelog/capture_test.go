package runtimelog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func options(t *testing.T) Options {
	t.Helper()
	return Options{Root: t.TempDir(), Module: "manager", Role: "backend", InstanceID: "test-instance", SegmentBytes: MaxEventBytes, InstanceBytes: 2 * MaxEventBytes, NodeBytes: 4 * MaxEventBytes, SourceAge: time.Hour}
}
func TestCaptureIdentitySecretsAndBoundedInput(t *testing.T) {
	o := options(t)
	t.Setenv("CLIENT_SECRET", "private-value")
	source := `{"level":"error","msg":"failed private-value","password":"unsafe","instance_id":"other","stack":"line1\nline2"}` + "\n" + strings.Repeat("x", 4*MaxEventBytes) + "\n"
	if e := Capture(o, strings.NewReader(source), strings.NewReader("Bearer forbidden\n")); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(o.Root, o.Module, o.InstanceID, "*.jsonl"))
	var count int
	var truncated, stack bool
	for _, file := range files {
		body, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(body), "private-value") || strings.Contains(string(body), "unsafe") || strings.Contains(string(body), "forbidden") {
			t.Fatal("credential reached source file")
		}
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			var entry Entry
			if e = json.Unmarshal([]byte(line), &entry); e != nil {
				t.Fatal(e)
			}
			if entry.InstanceID != o.InstanceID || entry.Module != o.Module || len(line) > MaxEventBytes {
				t.Fatal("identity or size violation")
			}
			if entry.Channel == "stderr" && entry.Level != "unknown" {
				t.Fatal("stderr must not imply error")
			}
			count++
			truncated = truncated || entry.Truncated
			stack = stack || entry.Stack != ""
		}
	}
	if count != 3 || !truncated || !stack {
		t.Fatalf("entries=%d truncated=%v stack=%v", count, truncated, stack)
	}
}
func TestIndependentRetentionProtectsActiveSegments(t *testing.T) {
	o := options(t)
	dir := filepath.Join(o.Root, o.Module, o.InstanceID)
	os.MkdirAll(dir, 0700)
	old := filepath.Join(dir, "old.jsonl")
	active := filepath.Join(dir, "active.jsonl")
	os.WriteFile(old, []byte("old"), 0600)
	os.WriteFile(active, []byte("active"), 0600)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(old, past, past)
	os.Chtimes(active, past, past)
	f, e := os.OpenFile(active, os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	if e = Prune(o); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(old); !os.IsNotExist(e) {
		t.Fatal("expired segment kept")
	}
	if _, e = os.Stat(active); e != nil {
		t.Fatal("active segment deleted")
	}
}
func TestCaptureDiskFailureDrainsAndCounts(t *testing.T) {
	o := options(t)
	os.WriteFile(filepath.Join(o.Root, o.Module), []byte("not a directory"), 0600)
	done := make(chan error, 1)
	go func() { done <- Capture(o, strings.NewReader(strings.Repeat("output\n", 1000)), strings.NewReader("")) }()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disk failure blocked output consumer")
	}
}

func TestNodeQuotaDoesNotDeleteActiveSegmentOrOvershoot(t *testing.T) {
	o := options(t)
	o.SegmentBytes = 400
	o.InstanceBytes = 700
	o.NodeBytes = 700
	first := &segmentWriter{o: o, metrics: &Metrics{}}
	defer first.close()
	if err := first.write([]byte(strings.Repeat("a", 350))); err != nil {
		t.Fatal(err)
	}
	other := o
	other.InstanceID = "second-instance"
	second := &segmentWriter{o: other, metrics: &Metrics{}}
	defer second.close()
	if err := second.write([]byte(strings.Repeat("b", 350))); err != nil {
		t.Fatal(err)
	}
	if err := second.write([]byte("overflow")); err == nil {
		t.Fatal("active segments exceeded shared node quota")
	}
	first.close()
	if err := second.write([]byte("replacement")); err != nil {
		t.Fatal(err)
	}
	var total int64
	filepath.WalkDir(o.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".jsonl") {
			st, _ := d.Info()
			total += st.Size()
		}
		return nil
	})
	if total > o.NodeBytes {
		t.Fatalf("quota %d exceeded by %d", o.NodeBytes, total)
	}
}

func TestIndependentPruneEnforcesEachInactiveInstanceQuota(t *testing.T) {
	o := options(t)
	for _, id := range []string{"one", "two"} {
		dir := filepath.Join(o.Root, o.Module, id)
		os.MkdirAll(dir, 0700)
		for _, name := range []string{"a.jsonl", "b.jsonl", "c.jsonl"} {
			os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", MaxEventBytes)), 0600)
		}
	}
	o.NodeBytes = 10 * MaxEventBytes
	if err := Prune(o); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		files, _ := filepath.Glob(filepath.Join(o.Root, o.Module, id, "*.jsonl"))
		if len(files) != 2 {
			t.Fatalf("instance %s retained %d segments", id, len(files))
		}
	}
}

func TestOversizeStructuredMetadataCannotEscapeEventBudget(t *testing.T) {
	o := options(t)
	value := map[string]string{"time": strings.Repeat("x", 15000), "request_id": strings.Repeat("中", 10000), "message": strings.Repeat("z", 15000)}
	line, _ := json.Marshal(value)
	entry := Normalize(o, "stdout", line, 1, false)
	encoded, _ := json.Marshal(entry)
	if len(encoded) > MaxEventBytes || len(entry.RequestID) > 256 || entry.EventTime != "" || !entry.Truncated {
		t.Fatal("event metadata escaped bounds")
	}
}

func TestReceiverReportsParsingTruncationAndWriteCounts(t *testing.T) {
	o := options(t)
	if err := Capture(o, strings.NewReader("{broken-json}\n"+strings.Repeat("x", MaxEventBytes*2)+"\n"), strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(o.Root, o.Module, o.InstanceID, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]uint64
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	value = map[string]uint64{}
	for _, key := range []string{"received", "written", "parse_failures", "truncated", "dropped"} {
		var count uint64
		json.Unmarshal(raw[key], &count)
		value[key] = count
	}
	if value["received"] != 2 || value["written"] != 2 || value["parse_failures"] != 1 || value["truncated"] != 1 || value["dropped"] != 0 {
		t.Fatal(value)
	}
}

func TestPruneRemovesExpiredMetadataWithoutRemovingFreshOrActiveInstances(t *testing.T) {
	o := options(t)
	old := time.Now().Add(-2 * o.SourceAge)
	for _, id := range []string{"expired", "fresh", "with-segment"} {
		dir := filepath.Join(o.Root, o.Module, id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "status.json")
		if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if id != "fresh" {
			os.Chtimes(file, old, old)
		}
		if id == "with-segment" {
			os.WriteFile(filepath.Join(dir, "active.jsonl"), []byte("event"), 0600)
		}
	}
	if err := Prune(o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(o.Root, o.Module, "expired")); !os.IsNotExist(err) {
		t.Fatal("expired metadata retained")
	}
	for _, id := range []string{"fresh", "with-segment"} {
		if _, err := os.Stat(filepath.Join(o.Root, o.Module, id)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCaptureQueueBoundsActualBuffersAndEntryCount(t *testing.T) {
	q := newCaptureQueue()
	item := sourceLine{line: make([]byte, MaxEventBytes)}
	for i := 0; i < captureQueueBytes/MaxEventBytes; i++ {
		if !q.enqueue(item) {
			t.Fatal("byte budget rejected available capacity")
		}
	}
	if q.enqueue(item) || q.pending.Load() != captureQueueBytes {
		t.Fatal("queue exceeded memory budget")
	}
	value := <-q.entries
	q.pending.Add(-int64(cap(value.line)))
	if !q.enqueue(item) {
		t.Fatal("released capacity not reused")
	}
	small := newCaptureQueue()
	for i := 0; i < cap(small.entries); i++ {
		if !small.enqueue(sourceLine{line: []byte("a")}) {
			t.Fatal("small entries did not use available budget")
		}
	}
	if small.enqueue(sourceLine{line: []byte("b")}) {
		t.Fatal("entry count unbounded")
	}
}

func TestStartupBurstUsesByteBudgetAndFlushesEveryAcceptedEntry(t *testing.T) {
	o := options(t)
	o.InstanceBytes = 10 << 20
	o.NodeBytes = 10 << 20
	if err := Capture(o, strings.NewReader(strings.Repeat("startup burst\n", 4000)), strings.NewReader("[GIN] ordinary text\n")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(o.Root, o.Module, o.InstanceID, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var status struct{ Received, Written, Dropped, ParseFailures uint64 }
	var raw map[string]uint64
	var value map[string]json.RawMessage
	json.Unmarshal(body, &value)
	raw = map[string]uint64{}
	for _, key := range []string{"received", "written", "dropped", "parse_failures"} {
		var n uint64
		json.Unmarshal(value[key], &n)
		raw[key] = n
	}
	status.Received = raw["received"]
	status.Written = raw["written"]
	status.Dropped = raw["dropped"]
	status.ParseFailures = raw["parse_failures"]
	if status.Received != 4001 || status.Written != 4001 || status.Dropped != 0 || status.ParseFailures != 0 {
		t.Fatalf("startup burst: %+v", status)
	}
	files, _ := filepath.Glob(filepath.Join(o.Root, o.Module, o.InstanceID, "*.jsonl"))
	lines := 0
	for _, file := range files {
		body, _ := os.ReadFile(file)
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if !json.Valid([]byte(line)) {
				t.Fatal("batch corrupted entry boundary")
			}
			lines++
		}
	}
	if lines != 4001 {
		t.Fatalf("written lines %d", lines)
	}
}
