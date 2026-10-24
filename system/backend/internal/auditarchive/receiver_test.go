package auditarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

const receiverTestToken = "owned-fixture-receiver-token-32-characters"
const receiverTestDeployment = "owned-minio-deployment"

func accessFixture(id string) string {
	return fmt.Sprintf(`{"version":"1","deploymentid":%q,"time":"2026-01-01T00:00:00Z","api":{"name":"GetObject","bucket":"audit","object":"safe-object","statusCode":200},"requestID":%q,"accessKey":"public-identity","requestHeader":{"Authorization":"secret-credential-marker"},"requestQuery":{"versionId":"requested-version","X-Amz-Signature":"secret-credential-marker"},"responseHeader":{"X-Amz-Version-Id":"returned-version","Set-Cookie":"secret-credential-marker"},"error":"secret-credential-marker"}`, receiverTestDeployment, id)
}
func fixtureReceiver(t *testing.T, s Store, root Ref) *Receiver {
	t.Helper()
	h, e := NewReceiver(s, testSourceID, receiverTestDeployment, receiverTestToken, root, Ref{}, 365)
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func receiveRequest(h http.Handler, body string, change func(*http.Request)) int {
	req := httptest.NewRequest(http.MethodPost, ReceiverPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+receiverTestToken)
	if change != nil {
		change(req)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}
func receiverRoot(t *testing.T, s Store) Ref {
	t.Helper()
	r, e := InitReceiver(context.Background(), s, testSourceID, receiverTestDeployment)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

type nativeQueueReadGate struct {
	Store
	entered, release chan struct{}
	once             sync.Once
}

func (s *nativeQueueReadGate) Read(ctx context.Context, v Version) ([]byte, Protection, error) {
	s.once.Do(func() {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	})
	return s.Store.Read(ctx, v)
}

func nativeQueueFixture() string {
	return strings.NewReplacer(`"GetObject"`, `"PutObject"`, `"bucket":"audit"`, `"bucket":"source-owned"`, `"object":"safe-object"`, `"object":"queued-first"`).Replace(accessFixture("known-source-put"))
}

func TestNativeQueueObservationDoesNotRejectARealDelivery(t *testing.T) {
	s := &memoryStore{}
	h := fixtureReceiver(t, s, receiverRoot(t, s))
	gate := &nativeQueueReadGate{Store: s, entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := nativeQueueObserve(ctx, gate, h); done <- err }()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal("observation did not reach its controlled read")
	}
	code := receiveRequest(h, nativeQueueFixture(), nil)
	close(gate.release)
	if err := <-done; err != nil {
		t.Fatal("observation could not finish", err)
	}
	if code != http.StatusNoContent {
		t.Fatalf("a read-only observation rejected a real delivery: status=%d", code)
	}
}

func TestNativeQueueObservationRequiresACommittedReceipt(t *testing.T) {
	s := &memoryStore{}
	root := receiverRoot(t, s)
	fault := &receiverFaultStore{Store: s, failReceipt: true}
	h := fixtureReceiver(t, fault, root)
	if code := receiveRequest(h, nativeQueueFixture(), nil); code != 503 {
		t.Fatal("fixture did not leave an unconfirmed body", code)
	}
	snapshot, err := nativeQueueObserve(context.Background(), s, h)
	if err != nil || snapshot.Sequence != 0 || snapshot.Known != [3]int{} {
		t.Fatal("observation counted an unconfirmed source request", snapshot, err)
	}
	h = fixtureReceiver(t, s, root)
	if code := receiveRequest(h, nativeQueueFixture(), nil); code != 204 {
		t.Fatal("orphan retry was not confirmed", code)
	}
	snapshot, err = nativeQueueObserve(context.Background(), s, h)
	if err != nil || snapshot.Sequence != 1 || snapshot.Known != [3]int{1, 0, 0} {
		t.Fatal("confirmed source request was not observed exactly once", snapshot, err)
	}
}

func TestReceiverPersistsWholeBatchBeforeAckAndResolvesLostConfirmation(t *testing.T) {
	s := &memoryStore{}
	root := receiverRoot(t, s)
	s.loseResponse = true
	h := fixtureReceiver(t, s, root)
	body := accessFixture("same-request") + "\n" + accessFixture("second")
	if c := receiveRequest(h, body, func(r *http.Request) { r.Header.Set("WebhookEventPayloadCount", "2") }); c != 204 {
		t.Fatalf("initial code %d", c)
	}
	observation, e := VerifyReceiver(context.Background(), s, testSourceID, receiverTestDeployment, root, Ref{})
	if e != nil || observation.Events != 2 || observation.Sequence != 1 {
		t.Fatal(observation, e)
	}
	// A process restart has no local state or hidden queue. The receipt is enough.
	restarted := fixtureReceiver(t, s, root)
	creates := s.creates
	if c := receiveRequest(restarted, body, nil); c != 204 || s.creates != creates {
		t.Fatal("duplicate wrote evidence again", c)
	}
	// Same requestID with different complete input is a distinct delivery.
	if c := receiveRequest(restarted, accessFixture("same-request"), nil); c != 204 {
		t.Fatal(c)
	}
	observation, e = VerifyReceiver(context.Background(), s, testSourceID, receiverTestDeployment, root, observation.Checkpoint)
	if e != nil || observation.Events != 3 || observation.Sequence != 2 {
		t.Fatal(observation, e)
	}
	projected, err := parseAccess([]byte(accessFixture("versions")), receiverTestDeployment)
	if err != nil || projected[0].RequestedVersion != "requested-version" || projected[0].ReturnedVersion != "returned-version" {
		t.Fatal("safe object version selectors lost", err)
	}
	for _, versions := range s.objects {
		for _, v := range versions {
			if bytes.Contains(v.data, []byte("secret-credential-marker")) {
				t.Fatal("sensitive fields persisted")
			}
		}
	}
}

func TestReceiverRefusesInvalidWholeBatchWithoutWrites(t *testing.T) {
	s := &memoryStore{}
	h := fixtureReceiver(t, s, receiverRoot(t, s))
	before := s.creates
	for _, tc := range []struct {
		body   string
		change func(*http.Request)
		code   int
	}{
		{accessFixture("a"), func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }, 401},
		{accessFixture("a"), func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+receiverTestToken) }, 401},
		{strings.Replace(accessFixture("a"), receiverTestDeployment, "forged-deployment", 1), nil, 400},
		{accessFixture("a") + "\n{", nil, 400},
		{accessFixture("a") + "\nnull", nil, 400},
		{strings.Replace(accessFixture("a"), `"version":"1"`, `"version":"1","version":"1"`, 1), nil, 400},
		{accessFixture("a"), func(r *http.Request) { r.Header.Set("WebhookEventPayloadCount", "2") }, 400},
		{strings.Repeat(" ", receiverBytes+1), nil, 413},
		{strings.Repeat(accessFixture("a")+"\n", 1001), nil, 400},
		{accessFixture("a"), func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, 415},
		{accessFixture("a"), func(r *http.Request) { r.Method = "GET" }, 405},
		{accessFixture("a"), func(r *http.Request) { r.URL.RawQuery = "source=forged" }, 404},
	} {
		if c := receiveRequest(h, tc.body, tc.change); c != tc.code {
			t.Fatalf("got %d want %d", c, tc.code)
		}
	}
	if s.creates != before {
		t.Fatal("invalid batch partially persisted")
	}
}

type receiverFaultStore struct {
	Store
	failRead, failReceipt, shortBody bool
	failReceiptRead                  bool
	gate                             chan struct{}
	entered                          chan struct{}
	once                             sync.Once
}

func (s *receiverFaultStore) Create(ctx context.Context, key string, b []byte, until time.Time) (string, error) {
	if strings.Contains(key, "/receipts/") && s.failReceipt {
		return "", ErrUnknown
	}
	if s.gate != nil && strings.Contains(key, "/data/") {
		s.once.Do(func() { close(s.entered) })
		select {
		case <-s.gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if s.shortBody && strings.Contains(key, "/data/") {
		until = time.Now().Add(24 * time.Hour)
	}
	return s.Store.Create(ctx, key, b, until)
}
func (s *receiverFaultStore) Read(ctx context.Context, v Version) ([]byte, Protection, error) {
	if s.failRead && strings.Contains(v.Key, "/data/") || s.failReceiptRead && strings.Contains(v.Key, "/receipts/") {
		return nil, Protection{}, ErrUnknown
	}
	return s.Store.Read(ctx, v)
}

func TestReceiverNeverAcknowledgesMissingReadbackAndResumesOrphan(t *testing.T) {
	for _, fault := range []string{"unavailable", "readback", "receipt", "receipt_readback", "short_retention"} {
		t.Run(fault, func(t *testing.T) {
			memory := &memoryStore{}
			root := receiverRoot(t, memory)
			store := &receiverFaultStore{Store: memory}
			h := fixtureReceiver(t, store, root)
			memory.denyCreate = fault == "unavailable"
			store.failRead = fault == "readback"
			store.failReceipt = fault == "receipt"
			store.failReceiptRead = fault == "receipt_readback"
			store.shortBody = fault == "short_retention"
			if c := receiveRequest(h, accessFixture("one"), nil); c != 503 {
				t.Fatal("failure acknowledged", c)
			}
			if fault != "receipt_readback" && len(memory.objects[accessReceiptKey(testSourceID, 1)]) != 0 {
				t.Fatal("receipt exists without verified body")
			}
			memory.denyCreate = false
			store.failRead = false
			store.failReceipt = false
			store.failReceiptRead = false
			if fault == "short_retention" {
				return
			} // Short protection cannot be silently replaced.
			if c := receiveRequest(fixtureReceiver(t, store, root), accessFixture("one"), nil); c != 204 {
				t.Fatal("orphan retry failed", c)
			}
			out, e := VerifyReceiver(context.Background(), memory, testSourceID, receiverTestDeployment, root, Ref{})
			if e != nil || out.Events != 1 {
				t.Fatal(out, e)
			}
		})
	}
}

func TestReceiverRejectsConcurrentBacklogAndPinnedTailRollback(t *testing.T) {
	memory := &memoryStore{}
	root := receiverRoot(t, memory)
	store := &receiverFaultStore{Store: memory, gate: make(chan struct{}), entered: make(chan struct{})}
	h := fixtureReceiver(t, store, root)
	done := make(chan int, 1)
	go func() { done <- receiveRequest(h, accessFixture("a"), nil) }()
	<-store.entered
	if c := receiveRequest(h, accessFixture("b"), nil); c != 503 {
		t.Fatal("unbounded request queue", c)
	}
	close(store.gate)
	if c := <-done; c != 204 {
		t.Fatal(c)
	}
	first, e := VerifyReceiver(context.Background(), memory, testSourceID, receiverTestDeployment, root, Ref{})
	if e != nil {
		t.Fatal(e)
	}
	if c := receiveRequest(h, accessFixture("b"), nil); c != 204 {
		t.Fatal(c)
	}
	last, e := VerifyReceiver(context.Background(), memory, testSourceID, receiverTestDeployment, root, Ref{})
	if e != nil {
		t.Fatal(e)
	}
	delete(memory.objects, accessReceiptKey(testSourceID, 2))
	if _, e = VerifyReceiver(context.Background(), memory, testSourceID, receiverTestDeployment, root, last.Checkpoint); e == nil {
		t.Fatal("external checkpoint rollback accepted")
	}
	// The running receiver also retains its last successful anchor.
	if c := receiveRequest(h, accessFixture("c"), nil); c != 503 {
		t.Fatal("running receiver forgot tail", c)
	}
	v := memory.objects[first.Checkpoint.Key][0]
	memory.objects[first.Checkpoint.Key] = append(memory.objects[first.Checkpoint.Key], v)
	if _, e = VerifyReceiver(context.Background(), memory, testSourceID, receiverTestDeployment, root, Ref{}); e == nil {
		t.Fatal("shadow accepted")
	}
}

func TestReceiverRejectsChangedMissingAndForkedEvidence(t *testing.T) {
	for _, fault := range []string{"body_changed", "body_missing", "receipt_fork", "protection"} {
		t.Run(fault, func(t *testing.T) {
			s := &memoryStore{}
			root := receiverRoot(t, s)
			h := fixtureReceiver(t, s, root)
			if c := receiveRequest(h, accessFixture("a"), nil); c != 204 {
				t.Fatal(c)
			}
			bodyKey := accessPrefix(testSourceID) + "data/" + digest([]byte(accessFixture("a"))) + ".json"
			switch fault {
			case "body_changed":
				o := s.objects[bodyKey][0]
				o.data = []byte(`{}`)
				s.objects[bodyKey][0] = o
			case "body_missing":
				delete(s.objects, bodyKey)
			case "protection":
				o := s.objects[bodyKey][0]
				o.protection.Until = time.Now().Add(time.Hour)
				s.objects[bodyKey][0] = o
			case "receipt_fork":
				key := accessReceiptKey(testSourceID, 1)
				o := s.objects[key][0]
				var r AccessReceipt
				_ = json.Unmarshal(o.data, &r)
				r.Previous.SHA = digest([]byte("fork"))
				o.data, _ = encode(r)
				s.objects[key][0] = o
			}
			if _, e := VerifyReceiver(context.Background(), s, testSourceID, receiverTestDeployment, root, Ref{}); e == nil {
				t.Fatal("corrupt evidence accepted")
			}
		})
	}
}

type receiverRaceStore struct {
	Store
	arrivals chan struct{}
	release  chan struct{}
}

func (s *receiverRaceStore) Create(ctx context.Context, key string, b []byte, until time.Time) (string, error) {
	if strings.Contains(key, "/receipts/") {
		s.arrivals <- struct{}{}
		select {
		case <-s.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return s.Store.Create(ctx, key, b, until)
}
func TestReceiverTwoProcessesCannotForkTheReceiptChain(t *testing.T) {
	memory := &memoryStore{}
	root := receiverRoot(t, memory)
	s := &receiverRaceStore{Store: memory, arrivals: make(chan struct{}, 2), release: make(chan struct{})}
	h1 := fixtureReceiver(t, s, root)
	h2 := fixtureReceiver(t, s, root)
	results := make(chan int, 2)
	go func() { results <- receiveRequest(h1, accessFixture("race-one"), nil) }()
	go func() { results <- receiveRequest(h2, accessFixture("race-two"), nil) }()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for i := 0; i < 2; i++ {
		select {
		case <-s.arrivals:
		case <-timer.C:
			close(s.release)
			t.Fatal("both handlers did not reach the real conditional receipt conflict")
		}
	}
	close(s.release)
	a, b := <-results, <-results
	if !((a == 204 && b == 503) || (a == 503 && b == 204)) {
		t.Fatal("competing receipt slot was not exclusive", a, b)
	}
	out, e := VerifyReceiver(context.Background(), memory, testSourceID, receiverTestDeployment, root, Ref{})
	if e != nil || out.Sequence != 1 || out.Events != 1 {
		t.Fatal("competing processes produced a fork", e)
	}
}

func TestReceiverLateOrphanRetryProtectsBodyBeyondNewReceipt(t *testing.T) {
	memory := &memoryStore{}
	root := receiverRoot(t, memory)
	faults := &receiverFaultStore{Store: memory, failReceipt: true}
	body := accessFixture("late-orphan")
	if c := receiveRequest(fixtureReceiver(t, faults, root), body, nil); c != 503 {
		t.Fatal(c)
	}
	key := accessPrefix(testSourceID) + "data/" + digest([]byte(body)) + ".json"
	old := memory.objects[key][0]
	old.version.CreatedAt = time.Now().Add(-100 * 24 * time.Hour)
	old.protection.Until = old.version.CreatedAt.Add(365*24*time.Hour + time.Hour)
	memory.objects[key][0] = old
	faults.failReceipt = false
	if c := receiveRequest(fixtureReceiver(t, faults, root), body, nil); c != 204 {
		t.Fatal("late orphan retry failed", c)
	}
	receipt := memory.objects[accessReceiptKey(testSourceID, 1)][0]
	protected := memory.objects[key][0]
	if protected.version.ID != old.version.ID || protected.protection.Until.Before(receipt.version.CreatedAt.Add(365*24*time.Hour)) {
		t.Fatal("new receipt exhausted its body protection")
	}
	if _, e := VerifyReceiver(context.Background(), memory, testSourceID, receiverTestDeployment, root, Ref{}); e != nil {
		t.Fatal(e)
	}
}

func TestReceiverRetentionOracleRequiresPinnedWORMResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"actual_worm", minio.ErrorResponse{Code: "InvalidRequest", StatusCode: 400, Message: "Object is WORM protected and cannot be overwritten"}, true},
		{"operation_succeeded", nil, false},
		{"transport", errors.New("owned transport failure"), false},
		{"policy_denied", minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403, Message: "Access Denied."}, false},
		{"bad_date", minio.ErrorResponse{Code: "InvalidRequest", StatusCode: 400, Message: "the retain until date must be in the future"}, false},
		{"wrong_status", minio.ErrorResponse{Code: "InvalidRequest", StatusCode: 500, Message: "Object is WORM protected and cannot be overwritten"}, false},
		{"bad_signature", minio.ErrorResponse{Code: "SignatureDoesNotMatch", StatusCode: 403}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fixedMinioWORMShorteningDenied(tc.err); got != tc.want {
				t.Fatalf("retention evidence accepted=%t want=%t", got, tc.want)
			}
		})
	}
}
