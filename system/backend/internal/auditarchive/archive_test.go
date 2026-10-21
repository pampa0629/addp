package auditarchive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type memoryObject struct {
	version Version
	data    []byte
	until   time.Time
}
type memoryStore struct {
	mu                       sync.Mutex
	objects                  []memoryObject
	loseResponse, denyCreate bool
}

func (s *memoryStore) Create(_ context.Context, key string, data []byte, until time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denyCreate {
		return "", ErrUnknown
	}
	for _, o := range s.objects {
		if o.version.Key == key {
			return "", ErrConflict
		}
	}
	id := fmt.Sprint(len(s.objects) + 1)
	s.objects = append(s.objects, memoryObject{Version{Key: key, ID: id}, append([]byte(nil), data...), until})
	if s.loseResponse {
		return "", ErrUnknown
	}
	return id, nil
}
func (s *memoryStore) Read(_ context.Context, v Version) ([]byte, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.objects {
		if o.version.Key == v.Key && o.version.ID == v.ID {
			return append([]byte(nil), o.data...), o.until, nil
		}
	}
	return nil, time.Time{}, ErrUnknown
}
func (s *memoryStore) Versions(_ context.Context, prefix string) ([]Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Version
	for _, o := range s.objects {
		if len(o.version.Key) >= len(prefix) && o.version.Key[:len(prefix)] == prefix {
			result = append(result, o.version)
		}
	}
	return result, nil
}

const testSourceID = "a21db94b-6dd5-446a-b66a-df379ea51dc8"
const testActor = "748d2bb7-8071-4a85-83b8-873f5c8e1fd7"

func str(s string) *string { return &s }
func testRow(id string) Row {
	r := Row{}
	for _, key := range auditColumns {
		r[key] = nil
	}
	for key, value := range map[string]string{"id": id, "event_name": "iam.test", "result": "succeeded", "risk_level": "high", "module_name": "system", "created_at": "2026-10-22 00:00:00+00", "details": "{}"} {
		r[key] = str(value)
	}
	return r
}
func testBatch() Batch {
	return Batch{Format: format, Source: Source{ID: testSourceID, SystemID: "12345", Database: "addp_test", Timeline: 1, Slot: "audit_slot", Publication: "audit_publication"}, EndLSN: 256, ArchivedAt: time.Now().UTC().Truncate(time.Second), RetentionDays: 2, Rows: []Row{testRow("1")}}
}

func TestArchiveConfirmationLossAndTamperDetection(t *testing.T) {
	ctx := context.Background()
	t.Run("lost_put_response_resolves_exact_version", func(t *testing.T) {
		s := &memoryStore{loseResponse: true}
		b := testBatch()
		if _, e := Save(ctx, s, b); e != nil {
			t.Fatal(e)
		}
		if _, e := Save(ctx, s, b); e != nil {
			t.Fatal(e)
		}
		c, e := Load(ctx, s, testSourceID)
		if e != nil || c.Count != 1 || len(s.objects) != 1 {
			t.Fatal("retry changed archived facts", e)
		}
	})
	for _, scenario := range []string{"content", "retention", "shadow_version", "delete_marker", "gap", "source", "order"} {
		t.Run(scenario, func(t *testing.T) {
			s := &memoryStore{}
			b := testBatch()
			sha, e := Save(ctx, s, b)
			if e != nil {
				t.Fatal(e)
			}
			b.Sequence = 1
			b.Previous = sha
			b.EndLSN++
			if _, e = Save(ctx, s, b); e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "content":
				s.objects[0].data[0] = '!'
			case "retention":
				s.objects[0].until = time.Now().Add(-time.Hour)
			case "shadow_version":
				o := s.objects[0]
				o.version.ID = "shadow"
				s.objects = append(s.objects, o)
			case "delete_marker":
				s.objects[0].version.DeleteMarker = true
			case "gap":
				s.objects = s.objects[1:]
			case "source":
				b.Source.Timeline++
				s.objects[1].data, _ = json.Marshal(b)
			case "order":
				b.EndLSN = 256
				s.objects[1].data, _ = json.Marshal(b)
			}
			if c, e := Load(ctx, s, testSourceID); e == nil || c != nil {
				t.Fatal("accepted unknown or altered history")
			}
		})
	}
}

func TestArchiveQueryRecordsAccessBeforeReleasingAnyRows(t *testing.T) {
	ctx := context.Background()
	s := &memoryStore{}
	b := testBatch()
	b.Rows = append(b.Rows, testRow("2"))
	b.Rows[1]["tenant_id"] = str("17")
	if _, e := Save(ctx, s, b); e != nil {
		t.Fatal(e)
	}
	log := &memoryStore{denyCreate: true}
	if c, rows, e := QueryArchive(ctx, s, log, testSourceID, testActor, 2, Query{TenantID: "17"}, false); e == nil || c != nil || rows != nil {
		t.Fatal("query leaked without durable access audit")
	}
	log.denyCreate = false
	c, rows, e := QueryArchive(ctx, s, log, testSourceID, testActor, 2, Query{TenantID: "17"}, false)
	if e != nil || c.Count != 2 || len(rows) != 1 || *rows[0]["id"] != "2" || len(log.objects) != 1 {
		t.Fatal("query or access record mismatch", e)
	}
	s.objects[0].data[0] = '!'
	if _, rows, e = QueryArchive(ctx, s, log, testSourceID, testActor, 2, Query{}, false); e == nil || rows != nil || len(log.objects) != 2 {
		t.Fatal("failed verification was not logged without releasing data")
	}
}

func TestArchiveQueryRefusesTruncatedResults(t *testing.T) {
	s := &memoryStore{}
	b := testBatch()
	b.Rows = nil
	for i := 0; i <= MaxQueryRows; i++ {
		b.Rows = append(b.Rows, testRow(fmt.Sprint(i+1)))
	}
	if _, e := Save(context.Background(), s, b); e != nil {
		t.Fatal(e)
	}
	log := &memoryStore{}
	if c, rows, e := QueryArchive(context.Background(), s, log, testSourceID, testActor, 2, Query{}, false); !errors.Is(e, ErrBudget) || c != nil || rows != nil {
		t.Fatal("silently truncated export", e)
	}
}
