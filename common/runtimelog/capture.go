// Package runtimelog owns the bounded, instance-scoped runtime output contract.
// Application processes never communicate with Loki directly.
package runtimelog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

const MaxEventBytes = 64 * 1024

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}$`)
var secretKey = regexp.MustCompile(`(?i)(password|secret|token|authorization|credential|api_key|private_key)`)
var secretText = regexp.MustCompile(`(?i)(bearer\s+\S+|(?:password|secret|token|api_key)\s*[=:]\s*[^\s,;]+)`)
var urlCredentials = regexp.MustCompile(`(?i)(https?|postgres(?:ql)?|mysql|redis)://[^/@\s]+:[^/@\s]+@`)
var urlQuery = regexp.MustCompile(`(https?://[^\s?]+)\?[^\s]+`)

type Options struct {
	Root, Module, Role, InstanceID, Node   string
	SegmentBytes, InstanceBytes, NodeBytes int64
	SourceAge                              time.Duration
}

func FromEnvironment(module, role, id string) (Options, error) {
	o := Options{Root: os.Getenv("ADDP_RUNTIME_LOG_ROOT"), Module: module, Role: role, InstanceID: id, Node: os.Getenv("ADDP_HOST_NODE_NAME"), SourceAge: 48 * time.Hour}
	if o.Root == "" {
		o.Root = "logs/runtime"
	}
	o.SegmentBytes = 50 << 20
	o.InstanceBytes = 1 << 30
	o.NodeBytes = 10 << 30
	for name, dst := range map[string]*int64{"ADDP_RUNTIME_LOG_SEGMENT_BYTES": &o.SegmentBytes, "ADDP_RUNTIME_LOG_INSTANCE_BYTES": &o.InstanceBytes, "ADDP_RUNTIME_LOG_NODE_BYTES": &o.NodeBytes} {
		if v := os.Getenv(name); v != "" {
			n, e := strconv.ParseInt(v, 10, 64)
			if e != nil || n < MaxEventBytes {
				return o, fmt.Errorf("invalid %s", name)
			}
			*dst = n
		}
	}
	if v := os.Getenv("ADDP_RUNTIME_LOG_SOURCE_HOURS"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 168 {
			return o, errors.New("invalid source retention")
		}
		o.SourceAge = time.Duration(n) * time.Hour
	}
	if !identifier.MatchString(module) || !identifier.MatchString(id) {
		return o, errors.New("invalid log identity")
	}
	switch role {
	case "backend", "worker", "scheduler", "ingress":
	default:
		return o, errors.New("invalid log role")
	}
	if o.InstanceBytes < o.SegmentBytes || o.NodeBytes < o.InstanceBytes {
		return o, errors.New("invalid log quotas")
	}
	return o, nil
}

type Entry struct {
	Timestamp  string `json:"timestamp"`
	EventTime  string `json:"event_time,omitempty"`
	Module     string `json:"module_name"`
	Role       string `json:"role"`
	InstanceID string `json:"instance_id"`
	Node       string `json:"node_name,omitempty"`
	ID         string `json:"entry_id"`
	Level      string `json:"level"`
	Channel    string `json:"channel"`
	Message    string `json:"message"`
	Stack      string `json:"stack,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// Redact runs before any source file write. Structured application logging must
// additionally avoid producing business payloads; pattern matching is not a DLP.
func Redact(value string) string {
	value = secretText.ReplaceAllString(value, "[REDACTED]")
	value = urlCredentials.ReplaceAllString(value, "$1://[REDACTED]@")
	value = urlQuery.ReplaceAllString(value, "$1?[REDACTED]")
	for _, env := range os.Environ() {
		key, secret, ok := strings.Cut(env, "=")
		if ok && secretKey.MatchString(key) && len(secret) >= 6 {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func sanitize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if secretKey.MatchString(k) || k == "body" || k == "payload" || k == "response_body" || k == "request_body" {
				x[k] = "[REDACTED]"
			} else {
				x[k] = sanitize(v)
			}
		}
		return x
	case []any:
		for i, v := range x {
			x[i] = sanitize(v)
		}
		return x
	case string:
		return Redact(x)
	default:
		return v
	}
}

func Normalize(o Options, channel string, line []byte, seq uint64, truncated bool) Entry {
	e := Entry{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Module: o.Module, Role: o.Role, InstanceID: o.InstanceID, Node: o.Node, ID: fmt.Sprintf("%s:%d", o.InstanceID, seq), Level: "unknown", Channel: channel, Truncated: truncated}
	var data map[string]any
	if json.Unmarshal(line, &data) == nil && data != nil {
		data = sanitize(data).(map[string]any)
		if v, ok := data["level"].(string); ok {
			switch strings.ToLower(v) {
			case "debug", "info", "warn", "error":
				e.Level = strings.ToLower(v)
			case "warning":
				e.Level = "warn"
			case "critical", "fatal":
				e.Level = "error"
			}
		}
		if v, ok := data["time"].(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
				e.EventTime = v
			}
		}
		if v, ok := data["timestamp"].(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
				e.EventTime = v
			}
		}
		if v, ok := data["stack"].(string); ok {
			e.Stack = v
		}
		if v, ok := data["request_id"].(string); ok {
			e.RequestID = boundedString(v, 256)
			if len(v) > 256 {
				e.Truncated = true
			}
		}
		// Preserve structured fields for diagnosis without trusting their identity.
		delete(data, "instance_id")
		delete(data, "module_name")
		delete(data, "role")
		delete(data, "entry_id")
		body, _ := json.Marshal(data)
		e.Message = string(body)
	} else {
		e.Message = Redact(string(line))
	}
	// JSON encoding can expand control characters, so enforce the event budget
	// after encoding as well as while reading the input.
	encoded, _ := json.Marshal(e)
	if len(encoded) > MaxEventBytes {
		e.Stack = ""
		e.Message = boundedString(e.Message, MaxEventBytes/8)
		e.Truncated = true
	}
	return e
}

func boundedString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

type Metrics struct {
	Received      atomic.Uint64
	Written       atomic.Uint64
	ParseFailures atomic.Uint64
	Truncated     atomic.Uint64
	Dropped       atomic.Uint64
	WriteFailures atomic.Uint64
	Cleaned       atomic.Uint64
}
type sourceLine struct {
	channel   string
	line      []byte
	truncated bool
}

const captureQueueBytes = 16 << 20

type captureQueue struct {
	entries chan sourceLine
	pending atomic.Int64
}

func newCaptureQueue() *captureQueue { return &captureQueue{entries: make(chan sourceLine, 8192)} }
func (q *captureQueue) enqueue(item sourceLine) bool {
	cost := int64(cap(item.line))
	for {
		current := q.pending.Load()
		if current+cost > captureQueueBytes {
			return false
		}
		if q.pending.CompareAndSwap(current, current+cost) {
			break
		}
	}
	select {
	case q.entries <- item:
		return true
	default:
		q.pending.Add(-cost)
		return false
	}
}

// Capture drains both channels even when disk writes fail or the bounded
// byte budget is full. Batches amortize node quota scans without relaxing them.
func Capture(o Options, stdout, stderr io.Reader) error {
	queue := newCaptureQueue()
	var readers sync.WaitGroup
	metrics := &Metrics{}
	for channel, reader := range map[string]io.Reader{"stdout": stdout, "stderr": stderr} {
		readers.Add(1)
		go func(channel string, reader io.Reader) {
			defer readers.Done()
			r := bufio.NewReaderSize(reader, MaxEventBytes)
			var line []byte
			cut := false
			for {
				part, err := r.ReadSlice('\n')
				if len(line) < MaxEventBytes {
					n := min(len(part), MaxEventBytes-len(line))
					line = append(line, part[:n]...)
					if n < len(part) {
						cut = true
					}
				} else if len(part) > 0 {
					cut = true
				}
				if err == bufio.ErrBufferFull {
					cut = true
					continue
				}
				if len(line) > 0 {
					metrics.Received.Add(1)
					if !queue.enqueue(sourceLine{channel, bytes.TrimRight(line, "\r\n"), cut}) {
						metrics.Dropped.Add(1)
					}
					line = nil
					cut = false
				}
				if err != nil {
					return
				}
			}
		}(channel, reader)
	}
	go func() { readers.Wait(); close(queue.entries) }()
	w := &segmentWriter{o: o, metrics: metrics}
	defer w.close()
	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()
	flushTicker := time.NewTicker(50 * time.Millisecond)
	defer flushTicker.Stop()
	var seq, count uint64
	var batch []byte
	flush := func() {
		if count == 0 {
			return
		}
		if err := w.write(batch); err != nil {
			metrics.WriteFailures.Add(count)
			metrics.Dropped.Add(count)
		} else {
			metrics.Written.Add(count)
		}
		batch = batch[:0]
		count = 0
	}
	for {
		select {
		case item, ok := <-queue.entries:
			if !ok {
				flush()
				w.status()
				return nil
			}
			queue.pending.Add(-int64(cap(item.line)))
			seq++
			entry := Normalize(o, item.channel, item.line, seq, item.truncated)
			trimmed := bytes.TrimSpace(item.line)
			if len(trimmed) > 0 && trimmed[0] == '{' && !json.Valid(trimmed) {
				metrics.ParseFailures.Add(1)
			}
			if entry.Truncated {
				metrics.Truncated.Add(1)
			}
			body, _ := json.Marshal(entry)
			if len(batch)+len(body)+1 > MaxEventBytes {
				flush()
			}
			batch = append(batch, body...)
			batch = append(batch, '\n')
			count++
			if count >= 64 {
				flush()
			}
		case <-flushTicker.C:
			flush()
		case <-statusTicker.C:
			w.status()
		}
	}
}

type segmentWriter struct {
	o       Options
	metrics *Metrics
	file    *os.File
	size    int64
	segment uint64
}

func (w *segmentWriter) dir() string { return filepath.Join(w.o.Root, w.o.Module, w.o.InstanceID) }
func (w *segmentWriter) close() {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
}
func (w *segmentWriter) write(body []byte) error {
	if w.file == nil || w.size+int64(len(body)) > w.o.SegmentBytes {
		w.close()
		if err := os.MkdirAll(w.dir(), 0700); err != nil {
			return err
		}
		w.segment++
		path := filepath.Join(w.dir(), fmt.Sprintf("%020d-%06d.jsonl", time.Now().UnixNano(), w.segment))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			f.Close()
			return err
		}
		w.file = f
		w.size = 0
	}
	// The node lock covers projected quotas and the write. This prevents
	// simultaneous receivers from each consuming the same remaining capacity.
	lock, err := w.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = w.cleanLocked(int64(len(body))); err != nil {
		return err
	}
	n, err := w.file.Write(body)
	w.size += int64(n)
	if err != nil {
		w.close()
	}
	return err
}
func (w *segmentWriter) status() {
	if os.MkdirAll(w.dir(), 0700) != nil {
		return
	}
	body, _ := json.Marshal(map[string]any{"instance_id": w.o.InstanceID, "observed_at": time.Now().UTC(), "received": w.metrics.Received.Load(), "written": w.metrics.Written.Load(), "parse_failures": w.metrics.ParseFailures.Load(), "truncated": w.metrics.Truncated.Load(), "dropped": w.metrics.Dropped.Load(), "write_failures": w.metrics.WriteFailures.Load(), "source_files_cleaned": w.metrics.Cleaned.Load()})
	tmp := filepath.Join(w.dir(), "status.tmp")
	if os.WriteFile(tmp, body, 0600) == nil {
		_ = os.Rename(tmp, filepath.Join(w.dir(), "status.json"))
	}
}

type segment struct {
	path     string
	size     int64
	modified time.Time
}

func (w *segmentWriter) lock() (*os.File, error) {
	lock, err := os.OpenFile(filepath.Join(w.o.Root, ".cleanup.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}
func (w *segmentWriter) clean() error {
	lock, err := w.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	return w.cleanLocked(0)
}
func (w *segmentWriter) cleanLocked(projected int64) error {
	var files []segment
	var nodeSize int64
	sizes := map[string]int64{}
	sizes[w.dir()] = projected
	nodeSize = projected
	err := filepath.WalkDir(w.o.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			st, e := d.Info()
			if e != nil {
				return e
			}
			files = append(files, segment{path, st.Size(), st.ModTime()})
			nodeSize += st.Size()
			sizes[filepath.Dir(path)] += st.Size()
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.Before(files[j].modified) })
	for _, f := range files {
		dir := filepath.Dir(f.path)
		if w.file != nil && f.path == w.file.Name() {
			continue
		}
		if time.Since(f.modified) > w.o.SourceAge || nodeSize > w.o.NodeBytes || sizes[dir] > w.o.InstanceBytes {
			active, e := os.OpenFile(f.path, os.O_RDWR, 0)
			if e != nil {
				return e
			}
			if syscall.Flock(int(active.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
				active.Close()
				continue
			}
			e = os.Remove(f.path)
			active.Close()
			if e != nil {
				return e
			}
			nodeSize -= f.size
			sizes[dir] -= f.size
			w.metrics.Cleaned.Add(1)
		}
	}
	if nodeSize > w.o.NodeBytes || sizes[w.dir()] > w.o.InstanceBytes {
		return errors.New("runtime source quota exhausted")
	}
	return nil
}

// Prune runs independently of application activity. Active segments are locked
// by their receiver and cannot be removed even when they have been quiet.
func Prune(o Options) error {
	if err := os.MkdirAll(o.Root, 0700); err != nil {
		return err
	}
	w := &segmentWriter{o: o, metrics: &Metrics{}}
	err := w.clean()
	if err == nil {
		err = pruneExpiredMetadata(o)
	}
	body, _ := json.Marshal(map[string]any{"observed_at": time.Now().UTC(), "source_files_cleaned": w.metrics.Cleaned.Load(), "quota_exhausted": err != nil})
	_ = os.WriteFile(filepath.Join(o.Root, "housekeeping-status.json"), body, 0600)
	return err
}

// Only remove expired receiver metadata after every segment has gone. Fresh
// metadata and any active segment keep an instance directory in place.
func pruneExpiredMetadata(o Options) error {
	modules, err := os.ReadDir(o.Root)
	if err != nil {
		return err
	}
	for _, module := range modules {
		if !module.IsDir() {
			continue
		}
		moduleDir := filepath.Join(o.Root, module.Name())
		instances, err := os.ReadDir(moduleDir)
		if err != nil {
			return err
		}
		for _, instance := range instances {
			if !instance.IsDir() {
				continue
			}
			dir := filepath.Join(moduleDir, instance.Name())
			files, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			expired := true
			for _, file := range files {
				if file.Name() != "status.json" && file.Name() != "status.tmp" {
					expired = false
					break
				}
				info, err := file.Info()
				if err != nil {
					return err
				}
				if time.Since(info.ModTime()) <= o.SourceAge {
					expired = false
					break
				}
			}
			if !expired {
				continue
			}
			for _, file := range files {
				if err := os.Remove(filepath.Join(dir, file.Name())); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			// A concurrently starting receiver may have created a segment. Never use
			// recursive deletion; an occupied directory is deliberately retained.
			_ = os.Remove(dir)
		}
		_ = os.Remove(moduleDir)
	}
	return nil
}
