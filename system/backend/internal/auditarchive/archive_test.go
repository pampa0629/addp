package auditarchive

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryObject struct {
	version    Version
	data       []byte
	protection Protection
}
type memoryStore struct {
	mu                                   sync.Mutex
	objects                              map[string][]memoryObject
	next, creates, extends               int
	loseResponse, denyCreate, denyExtend bool
	now                                  func() time.Time
	largestRead                          int
}

func (s *memoryStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now().UTC()
}
func (s *memoryStore) Create(_ context.Context, key string, b []byte, until time.Time, hold bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creates++
	if s.denyCreate {
		return "", ErrUnknown
	}
	if s.objects == nil {
		s.objects = map[string][]memoryObject{}
	}
	if len(s.objects[key]) != 0 {
		return "", ErrConflict
	}
	s.next++
	id := fmt.Sprint(s.next)
	s.objects[key] = []memoryObject{{Version{key, id, false, s.clock()}, append([]byte(nil), b...), Protection{until, hold}}}
	if s.loseResponse {
		return "", ErrUnknown
	}
	return id, nil
}
func (s *memoryStore) Read(_ context.Context, v Version) ([]byte, Protection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.objects[v.Key] {
		if o.version.ID == v.ID {
			if len(o.data) > s.largestRead {
				s.largestRead = len(o.data)
			}
			return append([]byte(nil), o.data...), o.protection, nil
		}
	}
	return nil, Protection{}, ErrUnknown
}
func (s *memoryStore) Extend(_ context.Context, v Version, until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extends++
	if s.denyExtend {
		return ErrUnknown
	}
	for i, o := range s.objects[v.Key] {
		if o.version.ID == v.ID {
			if until.Before(o.protection.Until) {
				return ErrConflict
			}
			s.objects[v.Key][i].protection.Until = until
			return nil
		}
	}
	return ErrUnknown
}
func (s *memoryStore) Walk(ctx context.Context, prefix string, visit func(Version) error) error {
	s.mu.Lock()
	var result []Version
	if strings.HasSuffix(prefix, ".json") {
		for _, o := range s.objects[prefix] {
			result = append(result, o.version)
		}
	} else {
		var keys []string
		for k := range s.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, o := range s.objects[k] {
				result = append(result, o.version)
			}
		}
	}
	s.mu.Unlock()
	for _, v := range result {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := visit(v); e != nil {
			return e
		}
	}
	return nil
}

const testSourceID = "a21db94b-6dd5-446a-b66a-df379ea51dc8"
const testActor = "748d2bb7-8071-4a85-83b8-873f5c8e1fd7"

func str(s string) *string { return &s }
func testRow(id string) Row {
	r := Row{}
	for _, k := range auditColumns {
		r[k] = nil
	}
	for k, v := range map[string]string{"id": id, "event_name": "iam.test", "result": "succeeded", "risk_level": "high", "module_name": "system", "created_at": "2026-10-22 00:00:00+00", "details": "{}"} {
		r[k] = str(v)
	}
	return r
}
func testSource() Source {
	return Source{testSourceID, "12345", "addp_test", 1, "audit_slot", "audit_publication"}
}
func memoryArchive(t *testing.T, s *memoryStore) (*writer, Ref) {
	t.Helper()
	ctx := context.Background()
	w := newWriter(s, testSource(), 365, &segment{Baseline: true, BeforeLSN: 256, EndLSN: 256})
	w.now = s.clock
	if e := w.add(ctx, []Row{testRow("1")}, 256, time.Time{}); e != nil {
		t.Fatal(e)
	}
	if e := w.flush(ctx); e != nil {
		t.Fatal(e)
	}
	base, e := w.seal(ctx)
	if e != nil {
		t.Fatal(e)
	}
	root, e := put(ctx, s, rootKey(testSourceID), Root{format, testSource(), base, 256}, s.clock().Add(365*24*time.Hour+retentionMargin), true)
	if e != nil {
		t.Fatal(e)
	}
	w.current.Baseline = false
	return w, root
}
func appendRecord(t *testing.T, w *writer, id string, end uint64) {
	t.Helper()
	ctx := context.Background()
	if e := w.add(ctx, []Row{testRow(id)}, end, w.now()); e != nil {
		t.Fatal(e)
	}
	if e := w.flush(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestArchiveConfirmationLossAndTamperDetection(t *testing.T) {
	ctx := context.Background()
	t.Run("actual_confirmation_readback", func(t *testing.T) {
		s := &memoryStore{loseResponse: true}
		w, root := memoryArchive(t, s)
		appendRecord(t, w, "2", 257)
		c, e := Load(ctx, s, root, true, nil)
		if e != nil || c.Count != 2 {
			t.Fatal(e)
		}
	})
	for _, scenario := range []string{"body", "hold", "shadow", "marker", "gap", "root", "v1", "expired_missing"} {
		t.Run(scenario, func(t *testing.T) {
			s := &memoryStore{}
			w, root := memoryArchive(t, s)
			appendRecord(t, w, "2", 257)
			_, e := w.seal(ctx)
			if e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "body":
				s.objects[partKey(testSourceID, 0, 0)][0].data[0] = '!'
			case "hold":
				s.objects[checkpointKey(testSourceID, 0)][0].protection.Hold = false
			case "shadow":
				o := s.objects[checkpointKey(testSourceID, 1)][0]
				o.version.ID = "shadow"
				s.objects[o.version.Key] = append(s.objects[o.version.Key], o)
			case "marker":
				s.objects[checkpointKey(testSourceID, 1)][0].version.DeleteMarker = true
			case "gap":
				delete(s.objects, checkpointKey(testSourceID, 0))
			case "root":
				root.SHA = strings.Repeat("0", 64)
			case "v1":
				s.objects[rootKey(testSourceID)][0].data = []byte(`{"format":"addp.audit-archive/v1"}`)
			case "expired_missing":
				delete(s.objects, partKey(testSourceID, 0, 0))
			}
			if c, e := Load(ctx, s, root, true, nil); e == nil || c != nil {
				t.Fatal("accepted invalid evidence")
			}
		})
	}
}
func TestArchiveQueryRecordsAccessBeforeReleasingAnyRows(t *testing.T) {
	ctx := context.Background()
	s := &memoryStore{}
	_, root := memoryArchive(t, s)
	log := &memoryStore{denyCreate: true}
	if c, r, e := QueryArchive(ctx, s, log, root, testActor, 365, Query{}, false); e == nil || c != nil || r != nil {
		t.Fatal("released unaudited rows")
	}
	log.denyCreate = false
	c, r, e := QueryArchive(ctx, s, log, root, testActor, 365, Query{}, false)
	if e != nil || c.Count != 1 || len(r) != 1 {
		t.Fatal(e)
	}
	s.objects[partKey(testSourceID, 0, 0)][0].data[0] = '!'
	if _, r, e = QueryArchive(ctx, s, log, root, testActor, 365, Query{}, false); e == nil || r != nil || log.creates != 3 {
		t.Fatal("partial result or missing failure audit")
	}
}
func TestArchiveStreamingBeyondOldWholeChainLimits(t *testing.T) {
	ctx := context.Background()
	s := &memoryStore{}
	w, root := memoryArchive(t, s)
	row := testRow("2")
	row["user_agent"] = str(strings.Repeat("x", 7000))
	for i := 0; i < 10001; i++ {
		row["id"] = str(fmt.Sprint(i + 2))
		if e := w.add(ctx, []Row{row}, uint64(257+i), w.now()); e != nil {
			t.Fatal(e)
		}
		if e := w.flush(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := w.seal(ctx); e != nil {
		t.Fatal(e)
	}
	c, e := Load(ctx, s, root, true, nil)
	if e != nil || c.Count != 10002 || c.Segments < 2 || s.largestRead > MaxObjectBytes {
		t.Fatalf("unbounded/truncated chain: count=%v error=%v", c, e)
	}
	if c, rows, e := QueryArchive(ctx, s, &memoryStore{}, root, testActor, 365, Query{}, false); e != ErrBudget || c != nil || rows != nil {
		t.Fatal("truncated query accepted", e)
	}
}
func TestArchiveExpiryPlanIsReadOnlyAndStopsAtFirstProtectedSegment(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-368 * 24 * time.Hour)
	s := &memoryStore{now: func() time.Time { return old }}
	w, root := memoryArchive(t, s)
	appendRecord(t, w, "2", 257)
	if _, e := w.seal(context.Background()); e != nil {
		t.Fatal(e)
	}
	creates, extends := s.creates, s.extends
	p, e := planAt(context.Background(), s, root, now)
	if e != nil || len(p.Segments) != 2 || len(p.Objects) != 2 || p.Authorization || s.creates != creates || s.extends != extends {
		t.Fatal("plan wrote or omitted expired prefix", p, e)
	}
	c, e := loadAt(context.Background(), s, root, true, nil, now)
	if e != nil || c.ExpiredObjects != 2 {
		t.Fatal("expired body invalidated metadata or was claimed protected", e)
	}
	s.objects[partKey(testSourceID, 0, 0)][0].protection.Hold = true
	p, e = planAt(context.Background(), s, root, now)
	if e != nil || len(p.Objects) != 0 {
		t.Fatal("jumped over held prefix", e)
	}
	s.objects[partKey(testSourceID, 0, 0)][0].protection.Hold = false
	s.objects[partKey(testSourceID, 0, 0)][0].protection.Until = now.Add(-23 * time.Hour)
	p, e = planAt(context.Background(), s, root, now)
	if e != nil || len(p.Objects) != 0 {
		t.Fatal("ignored grace period", e)
	}
}
func TestArchiveSealCannotPublishBeforeRetentionExtension(t *testing.T) {
	ctx := context.Background()
	clock := time.Now().UTC()
	s := &memoryStore{now: func() time.Time { return clock }}
	w, root := memoryArchive(t, s)
	appendRecord(t, w, "2", 257)
	clock = clock.Add(2 * time.Hour)
	s.denyExtend = true
	if _, e := w.seal(ctx); e == nil {
		t.Fatal("sealed unextended objects")
	}
	if len(s.objects[checkpointKey(testSourceID, 1)]) != 0 {
		t.Fatal("published premature checkpoint")
	}
	s.denyExtend = false
	if _, e := w.seal(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(ctx, s, root, true, nil); e != nil {
		t.Fatal(e)
	}
}

func TestArchiveNeverCompletesAnInterruptedBaseline(t *testing.T) {
	ctx := context.Background()
	s := &memoryStore{}
	w := newWriter(s, testSource(), 365, &segment{Baseline: true, BeforeLSN: 256, EndLSN: 256})
	if e := w.add(ctx, []Row{testRow("1")}, 256, time.Time{}); e != nil {
		t.Fatal(e)
	}
	if e := w.flush(ctx); e != nil {
		t.Fatal(e)
	}
	before := s.creates
	p := PostgreSQL{SourceID: testSourceID}
	if _, e := p.Initialize(ctx, s); e == nil || s.creates != before {
		t.Fatal("silently recreated an interrupted snapshot")
	}
	if _, e := Load(ctx, s, Ref{rootKey(testSourceID), "missing", digest([]byte("missing"))}, true, nil); e == nil {
		t.Fatal("partial baseline became evidence")
	}
}
func TestArchiveQueryHasIndependentByteBudget(t *testing.T) {
	ctx := context.Background()
	s := &memoryStore{}
	w, root := memoryArchive(t, s)
	for i := 0; i < 10; i++ {
		r := testRow(fmt.Sprint(i + 2))
		r["user_agent"] = str(strings.Repeat("x", 2<<20))
		if e := w.add(ctx, []Row{r}, uint64(257+i), w.now()); e != nil {
			t.Fatal(e)
		}
		if e := w.flush(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if _, rows, e := QueryArchive(ctx, s, &memoryStore{}, root, testActor, 365, Query{}, false); e != ErrBudget || rows != nil {
		t.Fatal("released a result above its byte budget", e)
	}
}
func TestArchiveChecksCrossSegmentPredecessorAndUnlistedParts(t *testing.T) {
	for _, kind := range []string{"predecessor", "extra_part", "baseline_order"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s := &memoryStore{}
			w, root := memoryArchive(t, s)
			appendRecord(t, w, "2", 257)
			if _, e := w.seal(ctx); e != nil {
				t.Fatal(e)
			}
			key := checkpointKey(testSourceID, 1)
			switch kind {
			case "predecessor":
				var cp Checkpoint
				if e := decode(s.objects[key][0].data, &cp); e != nil {
					t.Fatal(e)
				}
				cp.Previous.SHA = strings.Repeat("0", 64)
				s.objects[key][0].data, _ = encode(cp)
			case "extra_part":
				o := s.objects[partKey(testSourceID, 0, 0)][0]
				o.version.Key = partKey(testSourceID, 0, 1)
				s.objects[o.version.Key] = []memoryObject{o}
			case "baseline_order":
				o := &s.objects[partKey(testSourceID, 1, 0)][0]
				var p Part
				if e := decode(o.data, &p); e != nil {
					t.Fatal(e)
				}
				p.Anchor.SHA = strings.Repeat("0", 64)
				o.data, _ = encode(p)
			}
			if _, e := Load(ctx, s, root, true, nil); e == nil {
				t.Fatal("accepted cross-segment alteration")
			}
		})
	}
}

func TestArchiveExpiredBaselineDoesNotInvalidateProtectedLaterSegment(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	clock := now.Add(-368 * 24 * time.Hour)
	s := &memoryStore{now: func() time.Time { return clock }}
	w, root := memoryArchive(t, s)
	clock = now
	appendRecord(t, w, "2", 257)
	if _, e := w.seal(ctx); e != nil {
		t.Fatal(e)
	}
	var protectedIDs []string
	c, e := loadAt(ctx, s, root, true, func(r Row, protected bool) error {
		if protected {
			protectedIDs = append(protectedIDs, *r["id"])
		}
		return nil
	}, now)
	if e != nil || c.ExpiredObjects != 1 || len(protectedIDs) != 1 || protectedIDs[0] != "2" {
		t.Fatal("confused expired history and current protected scope", e)
	}
	p, e := planAt(ctx, s, root, now)
	if e != nil || len(p.Segments) != 1 || p.Segments[0] != 0 {
		t.Fatal("plan crossed into protected later segment", e)
	}
	delete(s.objects, partKey(testSourceID, 0, 0))
	if _, e := loadAt(ctx, s, root, true, nil, now); e == nil {
		t.Fatal("a read-only plan authorized missing body")
	}
}
func TestArchiveIdleBoundarySealsWithoutInventingTransactions(t *testing.T) {
	ctx := context.Background()
	clock := time.Now().UTC()
	s := &memoryStore{now: func() time.Time { return clock }}
	w, root := memoryArchive(t, s)
	appendRecord(t, w, "2", 257)
	clock = clock.Add(24 * time.Hour)
	if e := w.flush(ctx); e != nil {
		t.Fatal(e)
	}
	c, e := Load(ctx, s, root, true, nil)
	if e != nil || c.Count != 2 || c.EndLSN != 257 || c.Segments != 2 || len(c.Active.Parts) != 0 {
		t.Fatal("idle day boundary duplicated or omitted a commit", e)
	}
}

func TestArchiveExpiryTimeBoundariesAndPlanBudget(t *testing.T) {
	ctx := context.Background()
	clock := time.Now().UTC().Add(-368 * 24 * time.Hour)
	s := &memoryStore{now: func() time.Time { return clock }}
	w, root := memoryArchive(t, s)
	until := s.objects[partKey(testSourceID, 0, 0)][0].protection.Until
	for _, tc := range []struct {
		at               time.Time
		expired, planned int
	}{{until.Add(-time.Nanosecond), 0, 0}, {until, 1, 0}, {until.Add(24*time.Hour - time.Nanosecond), 1, 0}, {until.Add(24 * time.Hour), 1, 1}} {
		c, e := loadAt(ctx, s, root, true, nil, tc.at)
		if e != nil || c.ExpiredObjects != tc.expired {
			t.Fatal("wrong expiry boundary", e)
		}
		p, e := planAt(ctx, s, root, tc.at)
		if e != nil || len(p.Objects) != tc.planned {
			t.Fatal("wrong cleanup grace boundary", e)
		}
	}
	w.partLimit = 1
	for i := 0; i < 256; i++ {
		appendRecord(t, w, fmt.Sprint(i+2), uint64(i+257))
	}
	if _, e := w.seal(ctx); e != nil {
		t.Fatal(e)
	}
	beforeCreate, beforeExtend := s.creates, s.extends
	if p, e := planAt(ctx, s, root, clock.Add(368*24*time.Hour)); e != ErrBudget || p != nil || s.creates != beforeCreate || s.extends != beforeExtend {
		t.Fatal("truncated or wrote an oversized plan", e)
	}
}

func TestArchiveRetentionSurvivesSDKWholeSecondHeaders(t *testing.T) {
	s := &memoryStore{}
	w, root := memoryArchive(t, s)
	var cp Checkpoint
	b, _, e := readRef(context.Background(), s, w.current.Previous, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = decode(b, &cp); e != nil {
		t.Fatal(e)
	}
	if cp.Until.Nanosecond() != 0 || cp.Until.Before(cp.ClosedAt.Add(365*24*time.Hour)) {
		t.Fatal("fractional retention loses required protection")
	}
	for _, objects := range s.objects {
		for _, o := range objects {
			if o.protection.Until.Nanosecond() != 0 {
				t.Fatal("PUT deadline cannot survive SDK header precision")
			}
		}
	}
	if _, e = Load(context.Background(), s, root, true, nil); e != nil {
		t.Fatal(e)
	}
}
