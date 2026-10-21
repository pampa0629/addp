// Package auditarchive owns independent, immutable copies of committed System audit events.
// It never changes source audit facts or authorizes a business operation.
package auditarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxObjectBytes = 16 << 20
const MaxChainObjects = 10000
const MaxChainBytes = 64 << 20
const MaxQueryRows = 1000
const format = "addp.audit-archive/v1"

var auditColumns = []string{"id", "principal_id", "principal_type", "context_type", "tenant_id", "event_name", "result", "risk_level", "module_name", "http_method", "resource_path", "http_status", "request_id", "ip_address", "user_agent", "entity_type", "entity_id", "details", "created_at"}

var ErrUnknown = errors.New("audit_archive_unknown")
var ErrBudget = errors.New("audit_archive_budget_exceeded")
var ErrConflict = errors.New("audit_archive_conflict")

// Row preserves PostgreSQL's textual representation, including SQL NULL, without
// lossy conversion of BIGINT identities through floating point JSON numbers.
type Row map[string]*string

type Source struct {
	ID          string `json:"id"`
	SystemID    string `json:"system_id"`
	Database    string `json:"database"`
	Timeline    uint32 `json:"timeline"`
	Slot        string `json:"slot"`
	Publication string `json:"publication"`
}

type Batch struct {
	Format        string    `json:"format"`
	Source        Source    `json:"source"`
	Sequence      uint64    `json:"sequence"`
	Previous      string    `json:"previous_sha256"`
	EndLSN        uint64    `json:"end_lsn"`
	ArchivedAt    time.Time `json:"archived_at"`
	CommitAt      time.Time `json:"commit_at"`
	RetentionDays int       `json:"retention_days"`
	Rows          []Row     `json:"rows"`
}

type Version struct {
	Key, ID      string
	DeleteMarker bool
}

// Store must enforce conditional create and return a concrete immutable version.
// Reader and collector receive independently scoped credentials at deployment.
type Store interface {
	Create(context.Context, string, []byte, time.Time) (string, error)
	Read(context.Context, Version) ([]byte, time.Time, error)
	Versions(context.Context, string) ([]Version, error)
}

type Chain struct {
	Bytes        int       `json:"-"`
	Batches      []Batch   `json:"-"`
	Source       Source    `json:"source"`
	Count        int       `json:"event_count"`
	LastSHA      string    `json:"last_sha256"`
	EndLSN       uint64    `json:"end_lsn"`
	ArchivedAt   time.Time `json:"archived_at"`
	LastCommitAt time.Time `json:"last_commit_at"`
}

func sourcePrefix(id string) (string, error) {
	u, err := uuid.Parse(id)
	if err != nil || u.String() != id {
		return "", ErrUnknown
	}
	return "sources/" + id + "/", nil
}

func batchKey(id string, sequence uint64) (string, error) {
	p, err := sourcePrefix(id)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%sbatches/%020d.json", p, sequence), nil
}

func digest(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }

func validSource(s Source) bool {
	_, err := sourcePrefix(s.ID)
	_, iderr := strconv.ParseUint(s.SystemID, 10, 64)
	return err == nil && iderr == nil && s.Database != "" && s.Timeline > 0 && identifier(s.Slot) && identifier(s.Publication)
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
	for _, key := range auditColumns {
		if _, ok := r[key]; !ok {
			return false
		}
	}
	for _, key := range []string{"id", "event_name", "result", "risk_level", "module_name", "created_at", "details"} {
		if r[key] == nil || *r[key] == "" {
			return false
		}
	}
	if n, err := strconv.ParseInt(*r["id"], 10, 64); err != nil || n <= 0 {
		return false
	}
	var details map[string]json.RawMessage
	return json.Unmarshal([]byte(*r["details"]), &details) == nil && details != nil
}

func encodeBatch(b Batch) ([]byte, error) {
	if b.Format != format || !validSource(b.Source) || b.EndLSN == 0 || b.ArchivedAt.IsZero() || b.RetentionDays < 1 || b.RetentionDays > 36500 {
		return nil, ErrUnknown
	}
	if b.Sequence == 0 && b.Previous != "" || b.Sequence > 0 && len(b.Previous) != 64 {
		return nil, ErrUnknown
	}
	for _, r := range b.Rows {
		if !validRow(r) {
			return nil, ErrUnknown
		}
	}
	data, err := json.Marshal(b)
	if len(data) > MaxObjectBytes {
		return nil, ErrBudget
	}
	return data, err
}

// Save confirms the exact bytes and retention before the caller may acknowledge WAL.
// A lost PUT response is resolved by versioned readback, never by overwriting.
func Save(ctx context.Context, store Store, b Batch) (string, error) {
	data, err := encodeBatch(b)
	if err != nil {
		return "", err
	}
	key, err := batchKey(b.Source.ID, b.Sequence)
	if err != nil {
		return "", err
	}
	until := b.ArchivedAt.AddDate(0, 0, b.RetentionDays)
	version, putErr := store.Create(ctx, key, data, until)
	if putErr != nil {
		versions, e := store.Versions(ctx, key)
		if e != nil || len(versions) != 1 || versions[0].Key != key || versions[0].DeleteMarker {
			return "", ErrUnknown
		}
		version = versions[0].ID
	}
	if version == "" || version == "null" {
		return "", ErrUnknown
	}
	versions, e := store.Versions(ctx, key)
	if e != nil || len(versions) != 1 || versions[0].Key != key || versions[0].ID != version || versions[0].DeleteMarker {
		return "", ErrConflict
	}
	got, retention, err := store.Read(ctx, Version{Key: key, ID: version})
	if err != nil || digest(got) != digest(data) || retention.Before(until) {
		return "", ErrUnknown
	}
	return digest(data), nil
}

// Load refuses gaps, shadow versions and foreign objects. A partial chain is not evidence.
func Load(ctx context.Context, store Store, id string) (*Chain, error) {
	p, err := sourcePrefix(id)
	if err != nil {
		return nil, err
	}
	versions, err := store.Versions(ctx, p+"batches/")
	if err != nil {
		return nil, ErrUnknown
	}
	if len(versions) == 0 {
		return nil, ErrUnknown
	}
	if len(versions) > MaxChainObjects {
		return nil, ErrBudget
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Key < versions[j].Key })
	c := &Chain{}
	for i, v := range versions {
		key, _ := batchKey(id, uint64(i))
		if v.Key != key || v.ID == "" || v.ID == "null" || v.DeleteMarker {
			return nil, ErrConflict
		}
		data, retention, e := store.Read(ctx, v)
		if e != nil || len(data) > MaxObjectBytes || !retention.After(time.Now()) {
			return nil, ErrUnknown
		}
		c.Bytes += len(data)
		if c.Bytes > MaxChainBytes {
			return nil, ErrBudget
		}
		var b Batch
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&b) != nil {
			return nil, ErrUnknown
		}
		canonical, e := encodeBatch(b)
		if e != nil || string(canonical) != string(data) || b.Source.ID != id || b.Sequence != uint64(i) || b.Previous != c.LastSHA || retention.Before(b.ArchivedAt.AddDate(0, 0, b.RetentionDays)) {
			return nil, ErrConflict
		}
		if i > 0 && (b.Source != c.Source || b.EndLSN <= c.EndLSN || b.ArchivedAt.Before(c.ArchivedAt)) {
			return nil, ErrConflict
		}
		c.Source = b.Source
		c.EndLSN = b.EndLSN
		c.LastSHA = digest(data)
		c.ArchivedAt = b.ArchivedAt
		c.LastCommitAt = b.CommitAt
		c.Count += len(b.Rows)
		c.Batches = append(c.Batches, b)
	}
	return c, nil
}

type Query struct {
	TenantID  string `json:"tenant_id,omitempty"`
	EventName string `json:"event_name,omitempty"`
}

// QueryArchive is the controlled offline platform-auditor entry. A tenant does
// not receive raw archive credentials. Even verification-only access is logged.
func QueryArchive(ctx context.Context, reader, accessLog Store, id, actor string, retentionDays int, q Query, verifyOnly bool) (*Chain, []Row, error) {
	if retentionDays < 1 || retentionDays > 36500 {
		return nil, nil, ErrUnknown
	}
	if u, e := uuid.Parse(actor); e != nil || u.String() != actor {
		return nil, nil, ErrUnknown
	}
	if q.TenantID != "" {
		if n, e := strconv.ParseInt(q.TenantID, 10, 64); e != nil || n <= 0 {
			return nil, nil, ErrUnknown
		}
	}
	if len(q.EventName) > 200 || strings.ContainsAny(q.EventName, "\r\n") {
		return nil, nil, ErrUnknown
	}
	c, err := Load(ctx, reader, id)
	var rows []Row
	if err == nil && !verifyOnly {
		for _, b := range c.Batches {
			for _, r := range b.Rows {
				if q.TenantID != "" && (r["tenant_id"] == nil || *r["tenant_id"] != q.TenantID) {
					continue
				}
				if q.EventName != "" && *r["event_name"] != q.EventName {
					continue
				}
				rows = append(rows, r)
				if len(rows) > MaxQueryRows {
					err = ErrBudget
					break
				}
			}
			if err != nil {
				break
			}
		}
	}
	// The storage policy must bind this prefix to the caller's independently
	// provisioned identity. actor is trusted deployment configuration, not a CLI flag.
	now := time.Now().UTC().Truncate(time.Second)
	record := struct {
		Format, Actor, Source, Result string
		Query                         Query
		VerifyOnly                    bool
		Count                         int
		At                            time.Time
	}{"addp.audit-query/v1", actor, id, "verified", q, verifyOnly, len(rows), now}
	if err != nil {
		record.Result = "unknown"
		record.Count = 0
	}
	data, _ := json.Marshal(record)
	key := "queries/" + actor + "/" + uuid.NewString() + ".json"
	v, e := accessLog.Create(ctx, key, data, now.AddDate(0, 0, retentionDays))
	if e != nil || v == "" || v == "null" {
		return nil, nil, ErrUnknown
	}
	got, until, e := accessLog.Read(ctx, Version{Key: key, ID: v})
	if e != nil || digest(got) != digest(data) || until.Before(now.AddDate(0, 0, retentionDays)) {
		return nil, nil, ErrUnknown
	}
	if err != nil {
		return nil, nil, err
	}
	return c, rows, nil
}
