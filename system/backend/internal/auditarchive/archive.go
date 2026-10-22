// Package auditarchive preserves committed audit events in independently protected objects.
package auditarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxObjectBytes = 16 << 20
const MaxQueryRows = 1000
const MaxQueryBytes = 16 << 20
const minRetentionDays = 365
const format = "addp.audit-archive/v2"
const maxParts = 256
const partTarget = 4 << 20
const segmentTarget = 128 << 20
const retentionMargin = time.Hour

var auditColumns = []string{"id", "principal_id", "principal_type", "context_type", "tenant_id", "event_name", "result", "risk_level", "module_name", "http_method", "resource_path", "http_status", "request_id", "ip_address", "user_agent", "entity_type", "entity_id", "details", "created_at"}
var ErrUnknown = errors.New("audit_archive_unknown")
var ErrBudget = errors.New("audit_archive_budget_exceeded")
var ErrConflict = errors.New("audit_archive_conflict")

type Row map[string]*string
type Source struct {
	ID          string `json:"id"`
	SystemID    string `json:"system_id"`
	Database    string `json:"database"`
	Timeline    uint32 `json:"timeline"`
	Slot        string `json:"slot"`
	Publication string `json:"publication"`
}
type Version struct {
	Key, ID      string
	DeleteMarker bool
	CreatedAt    time.Time
}
type Protection struct {
	Until time.Time
	Hold  bool
}
type Ref struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	SHA     string `json:"sha256"`
}

func (r Ref) valid() bool {
	b, e := hex.DecodeString(r.SHA)
	return r.Key != "" && r.Version != "" && r.Version != "null" && e == nil && len(b) == 32
}

// There is deliberately no deletion or release-hold operation in this interface.
type Store interface {
	Create(context.Context, string, []byte, time.Time, bool) (string, error)
	Read(context.Context, Version) ([]byte, Protection, error)
	Extend(context.Context, Version, time.Time) error
	Walk(context.Context, string, func(Version) error) error
}
type Transaction struct {
	EndLSN uint64    `json:"end_lsn"`
	At     time.Time `json:"at"`
	Count  int       `json:"count"`
}
type Part struct {
	Format       string        `json:"format"`
	Source       Source        `json:"source"`
	Segment      uint64        `json:"segment"`
	Index        int           `json:"index"`
	Previous     string        `json:"previous_sha256"`
	Anchor       Ref           `json:"previous_checkpoint"`
	Baseline     bool          `json:"baseline"`
	BeforeLSN    uint64        `json:"before_lsn"`
	EndLSN       uint64        `json:"end_lsn"`
	ArchivedAt   time.Time     `json:"archived_at"`
	Transactions []Transaction `json:"transactions,omitempty"`
	Rows         []Row         `json:"rows"`
}
type Entry struct {
	Ref    Ref    `json:"object"`
	Bytes  int    `json:"bytes"`
	Count  int    `json:"count"`
	EndLSN uint64 `json:"end_lsn"`
}
type Checkpoint struct {
	Format    string    `json:"format"`
	Source    Source    `json:"source"`
	Sequence  uint64    `json:"sequence"`
	Previous  Ref       `json:"previous"`
	Baseline  bool      `json:"baseline"`
	BeforeLSN uint64    `json:"before_lsn"`
	EndLSN    uint64    `json:"end_lsn"`
	ClosedAt  time.Time `json:"closed_at"`
	Until     time.Time `json:"protected_until"`
	Parts     []Entry   `json:"parts"`
}
type Root struct {
	Format      string `json:"format"`
	Source      Source `json:"source"`
	Baseline    Ref    `json:"baseline_checkpoint"`
	SnapshotLSN uint64 `json:"snapshot_lsn"`
}
type segment struct {
	Sequence          uint64
	Previous          Ref
	Baseline          bool
	BeforeLSN, EndLSN uint64
	Parts             []Entry
	Bytes             int
	Day               string
}
type Chain struct {
	Source         Source    `json:"source"`
	Root           Ref       `json:"root"`
	Last           Ref       `json:"last_checkpoint"`
	Tail           Ref       `json:"observed_tail"`
	EndLSN         uint64    `json:"end_lsn"`
	Count          int       `json:"record_count"`
	Segments       uint64    `json:"sealed_segments"`
	ExpiredObjects int       `json:"expired_objects"`
	ArchivedAt     time.Time `json:"archived_at"`
	LastCommitAt   time.Time `json:"last_commit_at"`
	Active         *segment  `json:"-"`
}

func sourcePrefix(id string) (string, error) {
	u, e := uuid.Parse(id)
	if e != nil || u.String() != id {
		return "", ErrUnknown
	}
	return "sources/" + id + "/", nil
}
func rootKey(id string) string { return "sources/" + id + "/root.json" }
func checkpointKey(id string, n uint64) string {
	return fmt.Sprintf("sources/%s/checkpoints/%020d.json", id, n)
}
func dataPrefix(id string, n uint64) string { return fmt.Sprintf("sources/%s/data/%020d/", id, n) }
func partKey(id string, n uint64, i int) string {
	return fmt.Sprintf("%s%06d.json", dataPrefix(id, n), i)
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func validSource(s Source) bool {
	_, e := sourcePrefix(s.ID)
	_, n := strconv.ParseUint(s.SystemID, 10, 64)
	return e == nil && n == nil && s.Database != "" && s.Timeline > 0 && identifier(s.Slot) && identifier(s.Publication)
}
func identifier(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for _, c := range s {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
func validRow(r Row) bool {
	if len(r) != len(auditColumns) {
		return false
	}
	for _, k := range auditColumns {
		if _, ok := r[k]; !ok {
			return false
		}
	}
	for _, k := range []string{"id", "event_name", "result", "risk_level", "module_name", "created_at", "details"} {
		if r[k] == nil || *r[k] == "" {
			return false
		}
	}
	n, e := strconv.ParseInt(*r["id"], 10, 64)
	var d map[string]json.RawMessage
	return e == nil && n > 0 && json.Unmarshal([]byte(*r["details"]), &d) == nil && d != nil
}
func encode(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, ErrUnknown
	}
	if len(b) > MaxObjectBytes {
		return nil, ErrBudget
	}
	return b, nil
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return ErrUnknown
	}
	same, e := encode(v)
	if e != nil || string(same) != string(b) {
		return ErrConflict
	}
	return nil
}
func exact(ctx context.Context, s Store, key string) (Version, error) {
	var got Version
	n := 0
	e := s.Walk(ctx, key, func(v Version) error {
		if v.Key != key {
			return nil
		}
		n++
		got = v
		if n > 1 || v.DeleteMarker || v.ID == "" || v.ID == "null" {
			return ErrConflict
		}
		return nil
	})
	if e != nil {
		return Version{}, e
	}
	if n != 1 {
		return Version{}, ErrUnknown
	}
	return got, nil
}
func readRef(ctx context.Context, s Store, r Ref, hold bool) ([]byte, Protection, error) {
	if !r.valid() {
		return nil, Protection{}, ErrUnknown
	}
	v, e := exact(ctx, s, r.Key)
	if e != nil || v.ID != r.Version {
		return nil, Protection{}, ErrConflict
	}
	b, p, e := s.Read(ctx, v)
	if e != nil || digest(b) != r.SHA || (hold && !p.Hold) {
		return nil, p, ErrUnknown
	}
	return b, p, nil
}

// The SDK formats PUT retention headers to whole seconds. Round up so a
// fractional deadline cannot silently lose protection or fail exact readback.
func retentionUntil(t time.Time) time.Time {
	u := t.UTC().Truncate(time.Second)
	if u.Before(t) {
		u = u.Add(time.Second)
	}
	return u
}

func put(ctx context.Context, s Store, key string, value any, until time.Time, hold bool) (Ref, error) {
	until = retentionUntil(until)
	b, e := encode(value)
	if e != nil {
		return Ref{}, e
	}
	id, writeErr := s.Create(ctx, key, b, until, hold)
	v, e := exact(ctx, s, key)
	if e != nil {
		return Ref{}, e
	}
	if writeErr == nil && id != v.ID {
		return Ref{}, ErrConflict
	}
	r := Ref{key, v.ID, digest(b)}
	got, p, e := readRef(ctx, s, r, hold)
	if e != nil || string(got) != string(b) || p.Until.Before(until) {
		return Ref{}, ErrUnknown
	}
	if !v.CreatedAt.IsZero() && until.Before(v.CreatedAt.Add(minRetentionDays*24*time.Hour)) {
		return Ref{}, ErrUnknown
	}
	return r, nil
}
func readRoot(ctx context.Context, s Store, r Ref) (Root, error) {
	var root Root
	b, _, e := readRef(ctx, s, r, true)
	if e != nil {
		return root, e
	}
	if e = decode(b, &root); e != nil {
		return root, e
	}
	if root.Format != format || !validSource(root.Source) || root.SnapshotLSN == 0 || !root.Baseline.valid() || r.Key != rootKey(root.Source.ID) {
		return root, ErrUnknown
	}
	return root, nil
}
func checkPart(p Part, src Source, g *segment, entry *Entry) error {
	if p.Format != format || p.Source != src || p.Segment != g.Sequence || p.Index != len(g.Parts) || p.Baseline != g.Baseline || p.BeforeLSN != g.EndLSN || p.Anchor != g.Previous || p.ArchivedAt.IsZero() {
		return ErrConflict
	}
	prev := ""
	if len(g.Parts) > 0 {
		prev = g.Parts[len(g.Parts)-1].Ref.SHA
	}
	if p.Previous != prev {
		return ErrConflict
	}
	for _, r := range p.Rows {
		if !validRow(r) {
			return ErrUnknown
		}
	}
	if p.Baseline {
		if len(p.Transactions) != 0 || p.EndLSN != p.BeforeLSN {
			return ErrConflict
		}
	} else {
		n := 0
		end := p.BeforeLSN
		for _, tx := range p.Transactions {
			if tx.EndLSN <= end || tx.Count < 1 || tx.At.IsZero() {
				return ErrConflict
			}
			end = tx.EndLSN
			n += tx.Count
		}
		if n != len(p.Rows) || n == 0 || end != p.EndLSN {
			return ErrConflict
		}
	}
	if entry != nil && (entry.Count != len(p.Rows) || entry.EndLSN != p.EndLSN) {
		return ErrConflict
	}
	return nil
}

// Load walks metadata and individual bounded objects, never the whole evidence body.
// A specified root is a trust input; an observed tail is not a claim of absolute freshness.
func Load(ctx context.Context, s Store, rootRef Ref, verify bool, visit func(Row, bool) error) (*Chain, error) {
	return loadAt(ctx, s, rootRef, verify, visit, time.Now().UTC())
}
func loadAt(ctx context.Context, s Store, rootRef Ref, verify bool, visit func(Row, bool) error, now time.Time) (*Chain, error) {
	root, e := readRoot(ctx, s, rootRef)
	if e != nil {
		return nil, e
	}
	c := &Chain{Source: root.Source, Root: rootRef, EndLSN: root.SnapshotLSN}
	baseDone := false
	var baselineID int64
	e = s.Walk(ctx, "sources/"+root.Source.ID+"/checkpoints/", func(v Version) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if v.Key != checkpointKey(root.Source.ID, c.Segments) || v.DeleteMarker {
			return ErrConflict
		}
		b, pr, e := s.Read(ctx, v)
		if e != nil || !pr.Hold {
			return ErrUnknown
		}
		var cp Checkpoint
		if e = decode(b, &cp); e != nil {
			return e
		}
		ref := Ref{v.Key, v.ID, digest(b)}
		if cp.Format != format || cp.Source != root.Source || cp.Sequence != c.Segments || cp.Previous != c.Last || cp.BeforeLSN != c.EndLSN || cp.Baseline == baseDone || len(cp.Parts) > maxParts || cp.ClosedAt.IsZero() || cp.Until.Before(cp.ClosedAt.Add(minRetentionDays*24*time.Hour)) {
			return ErrConflict
		}
		if cp.Baseline && (cp.BeforeLSN != root.SnapshotLSN || cp.EndLSN != root.SnapshotLSN) {
			return ErrConflict
		}
		if verify {
			index := 0
			if e := s.Walk(ctx, dataPrefix(root.Source.ID, cp.Sequence), func(dv Version) error {
				if index >= len(cp.Parts) || dv.DeleteMarker || dv.Key != cp.Parts[index].Ref.Key || dv.ID != cp.Parts[index].Ref.Version {
					return ErrConflict
				}
				index++
				return nil
			}); e != nil {
				return e
			}
			if index != len(cp.Parts) {
				return ErrUnknown
			}
		}
		g := &segment{Sequence: cp.Sequence, Previous: cp.Previous, Baseline: cp.Baseline, BeforeLSN: cp.BeforeLSN, EndLSN: cp.BeforeLSN}
		for i, entry := range cp.Parts {
			if entry.Ref.Key != partKey(root.Source.ID, cp.Sequence, i) || !entry.Ref.valid() || entry.Count < 1 || entry.Bytes < 1 || entry.Bytes > MaxObjectBytes || (cp.Baseline && entry.EndLSN != root.SnapshotLSN) || (!cp.Baseline && entry.EndLSN <= g.EndLSN) {
				return ErrConflict
			}
			if verify {
				b, p, e := readRef(ctx, s, entry.Ref, false)
				if e != nil {
					return e
				}
				if len(b) != entry.Bytes || p.Until.Before(cp.Until) {
					return ErrConflict
				}
				var part Part
				if e = decode(b, &part); e != nil {
					return e
				}
				if e = checkPart(part, root.Source, g, &entry); e != nil {
					return e
				}
				protected := p.Hold || p.Until.After(now)
				if !protected {
					c.ExpiredObjects++
				}
				for _, row := range part.Rows {
					if cp.Baseline {
						id, _ := strconv.ParseInt(*row["id"], 10, 64)
						if id <= baselineID {
							return ErrConflict
						}
						baselineID = id
					}
					if visit != nil {
						if e = visit(row, protected); e != nil {
							return e
						}
					}
				}
				c.ArchivedAt = part.ArchivedAt
				if n := len(part.Transactions); n > 0 {
					c.LastCommitAt = part.Transactions[n-1].At
				}
			}
			g.Parts = append(g.Parts, entry)
			g.EndLSN = entry.EndLSN
			g.Bytes += entry.Bytes
			c.Count += entry.Count
		}
		if cp.EndLSN != g.EndLSN || (!cp.Baseline && len(cp.Parts) == 0) {
			return ErrConflict
		}
		if cp.Baseline && ref == root.Baseline {
			baseDone = true
		}
		c.Last = ref
		c.Tail = ref
		c.EndLSN = cp.EndLSN
		c.Segments++
		return nil
	})
	if e != nil {
		return nil, e
	}
	if !baseDone {
		return nil, ErrUnknown
	}
	g := &segment{Sequence: c.Segments, Previous: c.Last, BeforeLSN: c.EndLSN, EndLSN: c.EndLSN}
	e = s.Walk(ctx, dataPrefix(root.Source.ID, g.Sequence), func(v Version) error {
		if len(g.Parts) >= maxParts {
			return ErrBudget
		}
		if v.Key != partKey(root.Source.ID, g.Sequence, len(g.Parts)) || v.DeleteMarker {
			return ErrConflict
		}
		b, p, e := s.Read(ctx, v)
		if e != nil {
			return e
		}
		var part Part
		if e = decode(b, &part); e != nil {
			return e
		}
		if e = checkPart(part, root.Source, g, nil); e != nil {
			return e
		}
		if !p.Hold && !p.Until.After(now) {
			return ErrUnknown
		}
		entry := Entry{Ref{v.Key, v.ID, digest(b)}, len(b), len(part.Rows), part.EndLSN}
		g.Parts = append(g.Parts, entry)
		c.Tail = entry.Ref
		g.Bytes += len(b)
		g.EndLSN = part.EndLSN
		g.Day = part.ArchivedAt.Format("2006-01-02")
		c.Count += len(part.Rows)
		c.EndLSN = part.EndLSN
		c.ArchivedAt = part.ArchivedAt
		if n := len(part.Transactions); n > 0 {
			c.LastCommitAt = part.Transactions[n-1].At
		}
		if visit != nil {
			for _, r := range part.Rows {
				if e = visit(r, true); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	c.Active = g
	return c, nil
}

type Query struct {
	TenantID  string `json:"tenant_id,omitempty"`
	EventName string `json:"event_name,omitempty"`
}

func QueryArchive(ctx context.Context, s, log Store, root Ref, actor string, days int, q Query, verifyOnly bool) (*Chain, []Row, error) {
	u, e := uuid.Parse(actor)
	if e != nil || u.String() != actor || days < minRetentionDays || days > 36500 {
		return nil, nil, ErrUnknown
	}
	if q.TenantID != "" {
		n, e := strconv.ParseInt(q.TenantID, 10, 64)
		if e != nil || n <= 0 {
			return nil, nil, ErrUnknown
		}
	}
	if len(q.EventName) > 200 || strings.ContainsAny(q.EventName, "\r\n") {
		return nil, nil, ErrUnknown
	}
	var rows []Row
	size := 0
	c, err := Load(ctx, s, root, true, func(r Row, protected bool) error {
		if verifyOnly || !protected {
			return nil
		}
		if q.TenantID != "" && (r["tenant_id"] == nil || *r["tenant_id"] != q.TenantID) {
			return nil
		}
		if q.EventName != "" && *r["event_name"] != q.EventName {
			return nil
		}
		b, e := encode(r)
		if e != nil {
			return e
		}
		size += len(b)
		if len(rows) >= MaxQueryRows || size > MaxQueryBytes {
			return ErrBudget
		}
		rows = append(rows, r)
		return nil
	})
	now := time.Now().UTC()
	result := "verified_retained_scope"
	if err != nil {
		result = "unknown"
		rows = nil
	}
	record := struct {
		Format, Actor, Result string
		Root                  Ref
		Query                 Query
		Count                 int
		At                    time.Time
	}{format, actor, result, root, q, len(rows), now}
	if _, e = put(ctx, log, "queries/"+actor+"/"+uuid.NewString()+".json", record, now.Add(time.Duration(days)*24*time.Hour+retentionMargin), false); e != nil {
		return nil, nil, ErrUnknown
	}
	if err != nil {
		return nil, nil, err
	}
	return c, rows, nil
}

type PlanObject struct {
	Ref   Ref       `json:"object"`
	Until time.Time `json:"protected_until"`
}
type Plan struct {
	Root          Ref          `json:"root"`
	Observed      Ref          `json:"observed_checkpoint"`
	At            time.Time    `json:"at"`
	Segments      []uint64     `json:"segments"`
	Objects       []PlanObject `json:"objects"`
	SHA           string       `json:"sha256"`
	Authorization bool         `json:"execution_authorized"`
}

// PlanExpiry is strictly read-only: not even a query/plan receipt is written.
func PlanExpiry(ctx context.Context, s Store, root Ref) (*Plan, error) {
	return planAt(ctx, s, root, time.Now().UTC())
}
func planAt(ctx context.Context, s Store, root Ref, now time.Time) (*Plan, error) {
	c, e := loadAt(ctx, s, root, true, nil, now)
	if e != nil {
		return nil, e
	}
	p := &Plan{Root: root, Observed: c.Last, At: now}
	stop := false
	index := uint64(0)
	previous := Ref{}
	e = s.Walk(ctx, "sources/"+c.Source.ID+"/checkpoints/", func(v Version) error {
		if stop {
			return nil
		}
		b, pr, e := s.Read(ctx, v)
		if e != nil || !pr.Hold {
			return ErrUnknown
		}
		var cp Checkpoint
		if e = decode(b, &cp); e != nil {
			return e
		}
		if index >= c.Segments {
			stop = true
			return nil
		}
		ref := Ref{v.Key, v.ID, digest(b)}
		if v.DeleteMarker || v.Key != checkpointKey(c.Source.ID, index) || cp.Source != c.Source || cp.Sequence != index || cp.Previous != previous || (index == c.Segments-1 && ref != c.Last) {
			return ErrConflict
		}
		index++
		previous = ref
		var candidates []PlanObject
		for _, entry := range cp.Parts {
			_, pr, e := readRef(ctx, s, entry.Ref, false)
			if e != nil {
				return e
			}
			if pr.Hold || now.Before(pr.Until.Add(24*time.Hour)) {
				stop = true
				return nil
			}
			candidates = append(candidates, PlanObject{entry.Ref, pr.Until})
		}
		if len(cp.Parts) == 0 {
			return nil
		}
		if len(p.Segments) >= 256 || len(p.Objects)+len(candidates) > 4096 {
			return ErrBudget
		}
		p.Segments = append(p.Segments, cp.Sequence)
		p.Objects = append(p.Objects, candidates...)
		return nil
	})
	if e != nil {
		return nil, e
	}
	b, e := encode(p)
	if e != nil || len(b) > 4<<20 {
		return nil, ErrBudget
	}
	p.SHA = digest(b)
	return p, nil
}
