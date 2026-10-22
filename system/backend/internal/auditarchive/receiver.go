package auditarchive

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const accessFormat = "addp.audit-access/v1"
const ReceiverPath = "/api/v1/internal/audit-receipts"
const receiverBytes = 4 << 20
const receiverEvents = 1000

// AccessEvent excludes credentials, arbitrary headers/query, claims and errors;
// only two explicit object-version selectors are retained from query/headers.
// Resource names and public identity identifiers remain controlled audit data.
type AccessEvent struct {
	Version      string    `json:"version"`
	DeploymentID string    `json:"deploymentid"`
	Time         time.Time `json:"time"`
	API          struct {
		Name    string `json:"name"`
		Bucket  string `json:"bucket,omitempty"`
		Object  string `json:"object,omitempty"`
		Objects []struct {
			ObjectName string `json:"objectName"`
			VersionID  string `json:"versionId,omitempty"`
		} `json:"objects,omitempty"`
		Status     string `json:"status,omitempty"`
		StatusCode int    `json:"statusCode,omitempty"`
	} `json:"api"`
	RequestID        string `json:"requestID,omitempty"`
	RemoteHost       string `json:"remotehost,omitempty"`
	AccessKey        string `json:"accessKey,omitempty"`
	ParentUser       string `json:"parentUser,omitempty"`
	RequestedVersion string `json:"requested_version_id,omitempty"`
	ReturnedVersion  string `json:"returned_version_id,omitempty"`
}
type accessRoot struct {
	Format     string `json:"format"`
	Source     string `json:"source"`
	Deployment string `json:"deployment"`
}
type accessBatch struct {
	Format   string        `json:"format"`
	Source   string        `json:"source"`
	InputSHA string        `json:"input_sha256"`
	Events   []AccessEvent `json:"events"`
}
type AccessReceipt struct {
	Format     string    `json:"format"`
	Source     string    `json:"source"`
	Sequence   uint64    `json:"sequence"`
	Previous   Ref       `json:"previous"`
	Data       Ref       `json:"data"`
	InputSHA   string    `json:"input_sha256"`
	Count      int       `json:"count"`
	ReceivedAt time.Time `json:"received_at"`
}
type AccessObservation struct {
	State      string `json:"state"`
	Source     string `json:"source"`
	Sequence   uint64 `json:"sequence"`
	Checkpoint Ref    `json:"checkpoint"`
	Events     uint64 `json:"events"`
}

func accessPrefix(source string) string { return "access/" + source + "/" }
func accessReceiptKey(source string, seq uint64) string {
	return fmt.Sprintf("%sreceipts/%020d.json", accessPrefix(source), seq)
}

func InitReceiver(ctx context.Context, s Store, source, deployment string) (Ref, error) {
	if _, e := sourcePrefix(source); e != nil || deployment == "" {
		return Ref{}, ErrUnknown
	}
	return put(ctx, s, accessPrefix(source)+"root.json", accessRoot{accessFormat, source, deployment}, controlUntil(time.Now()), true)
}

// walkAccess retains one batch at a time. An externally pinned checkpoint detects
// rollback through that point; neither root nor receive order proves A completeness.
func walkAccess(ctx context.Context, s Store, source, deployment string, root, pin Ref, input string) (AccessObservation, *AccessReceipt, error) {
	out := AccessObservation{State: "verified_received_scope", Source: source, Checkpoint: root}
	if _, e := sourcePrefix(source); e != nil || root.Key != accessPrefix(source)+"root.json" {
		return out, nil, ErrUnknown
	}
	b, _, e := readRef(ctx, s, root, true)
	var origin accessRoot
	if e != nil || decode(b, &origin) != nil || origin != (accessRoot{accessFormat, source, deployment}) {
		return out, nil, ErrUnknown
	}
	pinned := pin == (Ref{}) || pin == root
	var match *AccessReceipt
	e = s.Walk(ctx, accessPrefix(source)+"receipts/", func(v Version) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if v.Key != accessReceiptKey(source, out.Sequence+1) || v.DeleteMarker || v.ID == "" || v.ID == "null" {
			return ErrConflict
		}
		data, p, err := s.Read(ctx, v)
		if err != nil || !validControl(v, p, time.Now()) {
			return ErrUnknown
		}
		var r AccessReceipt
		if decode(data, &r) != nil || r.Format != accessFormat || r.Source != source || r.Sequence != out.Sequence+1 || r.Previous != out.Checkpoint || r.Count < 1 || r.Count > receiverEvents || !isSHA(r.InputSHA) || r.Data.Key != accessPrefix(source)+"data/"+r.InputSHA+".json" {
			return ErrConflict
		}
		raw, protection, err := readRef(ctx, s, r.Data, false)
		dv, ve := exact(ctx, s, r.Data.Key)
		var batch accessBatch
		if err != nil || ve != nil || !validAccessBody(dv, protection) || protection.Until.Before(v.CreatedAt.Add(365*24*time.Hour)) || decode(raw, &batch) != nil || batch.Format != accessFormat || batch.Source != source || batch.InputSHA != r.InputSHA || len(batch.Events) != r.Count || !r.ReceivedAt.Equal(dv.CreatedAt) {
			return ErrUnknown
		}
		for _, event := range batch.Events {
			if !validAccessEvent(event, deployment) {
				return ErrUnknown
			}
		}
		out.Sequence = r.Sequence
		out.Events += uint64(r.Count)
		out.Checkpoint = Ref{v.Key, v.ID, digest(data)}
		if out.Checkpoint == pin {
			pinned = true
		}
		if r.InputSHA == input {
			if match != nil {
				return ErrConflict
			}
			copy := r
			match = &copy
		}
		return nil
	})
	if e != nil {
		return out, nil, e
	}
	if !pinned {
		return out, nil, ErrConflict
	}
	return out, match, nil
}
func isSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validAccessBody(v Version, p Protection) bool {
	return !v.CreatedAt.IsZero() && !p.Until.Before(v.CreatedAt.Add(365*24*time.Hour)) && p.Until.After(time.Now())
}
func validAccessEvent(e AccessEvent, deployment string) bool {
	return e.Version == "1" && e.DeploymentID == deployment && !e.Time.IsZero() && e.API.Name != "" && len(e.API.Name) <= 256 && e.API.StatusCode >= 0 && e.API.StatusCode <= 599
}

// Reject duplicate keys at every depth; different parsers must not interpret
// authenticated source or evidence fields differently. Unknown fields are omitted.
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrBudget
	}
	t, e := d.Token()
	if e != nil {
		return e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			name, ok := k.(string)
			if !ok || seen[name] {
				return ErrConflict
			}
			seen[name] = true
			if e = uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e := uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return ErrUnknown
	}
	_, e = d.Token()
	return e
}
func parseAccess(b []byte, deployment string) ([]AccessEvent, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var events []AccessEvent
	for {
		var raw json.RawMessage
		err := d.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil || len(events) >= receiverEvents {
			return nil, ErrBudget
		}
		check := json.NewDecoder(bytes.NewReader(raw))
		check.UseNumber()
		if len(raw) == 0 || raw[0] != '{' || uniqueJSON(check, 0) != nil {
			return nil, ErrUnknown
		}
		var event AccessEvent
		if json.Unmarshal(raw, &event) != nil || !validAccessEvent(event, deployment) {
			return nil, ErrUnknown
		}
		// Retain only the two non-secret version selectors. In particular,
		// never retain arbitrary presigned query strings or response headers.
		var selectors struct {
			Query   map[string]string `json:"requestQuery"`
			Headers map[string]string `json:"responseHeader"`
		}
		if json.Unmarshal(raw, &selectors) != nil {
			return nil, ErrUnknown
		}
		event.RequestedVersion = selectors.Query["versionId"]
		event.ReturnedVersion = ""
		for key, value := range selectors.Headers {
			if strings.EqualFold(key, "X-Amz-Version-Id") {
				if event.ReturnedVersion != "" {
					return nil, ErrConflict
				}
				event.ReturnedVersion = value
			}
		}
		if len(event.RequestedVersion) > 1024 || len(event.ReturnedVersion) > 1024 {
			return nil, ErrBudget
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil, ErrUnknown
	}
	return events, nil
}

// Receiver is a single-source, bounded synchronous webhook. It has no local
// durable queue. Only a verified protected receipt permits a 204 response.
type Receiver struct {
	store                             Store
	source, deployment, authorization string
	root, pin                         Ref
	days                              int
	mu                                sync.Mutex
}

func NewReceiver(s Store, source, deployment, token string, root, pin Ref, days int) (*Receiver, error) {
	if _, e := sourcePrefix(source); e != nil || s == nil || deployment == "" || len(token) < 32 || strings.ContainsAny(token, "\r\n \t") || days < 365 || days > 36500 || !root.valid() || root.Key != accessPrefix(source)+"root.json" || pin != (Ref{}) && !pin.valid() {
		return nil, ErrUnknown
	}
	return &Receiver{store: s, source: source, deployment: deployment, authorization: "Bearer " + token, root: root, pin: pin, days: days}, nil
}
func (h *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != ReceiverPath || r.URL.RawQuery != "" {
		w.WriteHeader(404)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	if len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(h.authorization)) != 1 {
		w.WriteHeader(401)
		return
	}
	if !h.mu.TryLock() {
		w.WriteHeader(503)
		return
	}
	defer h.mu.Unlock()
	if r.Header.Get("Content-Encoding") != "" {
		w.WriteHeader(415)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, receiverBytes))
	if err != nil {
		w.WriteHeader(413)
		return
	}
	events, err := parseAccess(body, h.deployment)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	if c := r.Header.Get("WebhookEventPayloadCount"); c != "" {
		n, e := strconv.Atoi(c)
		if e != nil || n != len(events) {
			w.WriteHeader(400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	input := digest(body)
	out, match, err := walkAccess(ctx, h.store, h.source, h.deployment, h.root, h.pin, input)
	if err != nil {
		w.WriteHeader(503)
		return
	}
	if match == nil {
		batch := accessBatch{accessFormat, h.source, input, events}
		key := accessPrefix(h.source) + "data/" + input + ".json"
		data, e := encode(batch)
		if e != nil {
			w.WriteHeader(400)
			return
		}
		// A lost PUT response is resolved by exact version lookup, never overwrite.
		_, _ = h.store.Create(ctx, key, data, retentionUntil(time.Now().Add(time.Duration(h.days)*24*time.Hour+retentionMargin)))
		v, e := exact(ctx, h.store, key)
		if e != nil {
			w.WriteHeader(503)
			return
		}
		actual, p, e := h.store.Read(ctx, v)
		if e != nil || !bytes.Equal(actual, data) || !validAccessBody(v, p) || p.Until.Before(v.CreatedAt.Add(time.Duration(h.days)*24*time.Hour)) {
			w.WriteHeader(503)
			return
		}
		// An orphan can be retried much later than its first upload. Preserve
		// its identity but extend protection before publishing a new receipt.
		required := retentionUntil(time.Now().Add(time.Duration(h.days)*24*time.Hour + retentionMargin))
		if p.Until.Before(required) {
			if h.store.Extend(ctx, v, required) != nil {
				w.WriteHeader(503)
				return
			}
			actual, p, e = h.store.Read(ctx, v)
			if e != nil || !bytes.Equal(actual, data) || p.Until.Before(required) {
				w.WriteHeader(503)
				return
			}
		}
		receipt := AccessReceipt{accessFormat, h.source, out.Sequence + 1, out.Checkpoint, Ref{key, v.ID, digest(data)}, input, len(events), v.CreatedAt}
		key = accessReceiptKey(h.source, receipt.Sequence)
		encoded, e := encode(receipt)
		if e != nil {
			w.WriteHeader(503)
			return
		}
		_, _ = h.store.Create(ctx, key, encoded, controlUntil(time.Now()))
		rv, e := exact(ctx, h.store, key)
		if e != nil {
			w.WriteHeader(503)
			return
		}
		actual, p, e = h.store.Read(ctx, rv)
		if e != nil || !bytes.Equal(actual, encoded) || !validControl(rv, p, time.Now()) {
			w.WriteHeader(503)
			return
		}
		// Keep the newest acknowledged anchor in memory. Deployment must export an
		// independent checkpoint to detect whole-tail rollback across process restart.
		h.pin = Ref{key, rv.ID, digest(encoded)}
	}
	if ctx.Err() != nil {
		w.WriteHeader(503)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func VerifyReceiver(ctx context.Context, s Store, source, deployment string, root, pin Ref) (AccessObservation, error) {
	out, _, e := walkAccess(ctx, s, source, deployment, root, pin, "")
	if e != nil {
		return AccessObservation{}, e
	}
	return out, nil
}
