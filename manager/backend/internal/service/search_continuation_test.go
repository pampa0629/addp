package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type continuationEngineFixture struct {
	mu             sync.Mutex
	docs           []map[string]any
	failProjection bool
}

func (f *continuationEngineFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/indexes/content":
		fmt.Fprint(w, `{"uid":"content","primaryKey":"id","createdAt":"2026-11-08T00:00:00Z","updatedAt":"2026-11-08T00:00:00Z"}`)
	case "/indexes/content/settings/pagination":
		fmt.Fprint(w, `{"maxTotalHits":1000}`)
	case "/indexes/content/documents/fetch", "/indexes/content/documents", "/indexes/content/search":
		var body map[string]any
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(400)
				return
			}
		}
		filter, _ := body["filter"].(string)
		if r.Method == http.MethodGet {
			filter = r.URL.Query().Get("filter")
		}
		projection := r.URL.Path == "/indexes/content/search" && body["limit"] != nil
		if projection && f.failProjection {
			f.failProjection = false
			w.WriteHeader(503)
			fmt.Fprint(w, `{"code":"temporarily_unavailable"}`)
			return
		}
		selected := []map[string]any{}
		for _, doc := range f.docs {
			if strings.Contains(filter, "document_id IN") && !strings.Contains(filter, doc["document_id"].(string)) {
				continue
			}
			clone := map[string]any{}
			for key, value := range doc {
				clone[key] = value
			}
			selected = append(selected, clone)
		}
		if r.URL.Path == "/indexes/content/search" {
			if projection {
				_ = json.NewEncoder(w).Encode(map[string]any{"hits": selected, "query": body["q"]})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": selected, "totalHits": len(selected), "page": 1, "hitsPerPage": body["hitsPerPage"], "query": body["q"]})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": selected, "total": len(selected), "offset": 0, "limit": len(selected)})
		}
	default:
		w.WriteHeader(404)
		fmt.Fprint(w, `{}`)
	}
}
func continuationServiceFixture(t *testing.T, n int) (*HybridSearchService, *continuationEngineFixture) {
	t.Helper()
	engine := &continuationEngineFixture{}
	for i := 0; i < n; i++ {
		id := testSearchIdentity(t, 7, 11, uint(i+1), fmt.Sprintf("Table%03d", i))
		engine.docs = append(engine.docs, map[string]any{"id": id.ID, "document_id": id.ID, "tenant_id": 7, "engine_id": 11, "engine_type": id.EngineType, "locator": id.Locator, "name": fmt.Sprintf("Table%03d", i), "search_ordinal": i + 1, "_rankingScore": 1.0, "_rankingScoreDetails": map[string]any{"words": map[string]any{"score": 1}}})
	}
	svc, _ := contentDeliveryServiceFixture(t, engine)
	return svc, engine
}
func searchIDs(result *SearchContinuationResult) []string {
	ids := []string{}
	for _, row := range result.Hits {
		ids = append(ids, row.DocumentID)
	}
	return ids
}

func TestSearchContinuationReplaysDeliveredBatchWithoutAdvancing(t *testing.T) {
	svc, _ := continuationServiceFixture(t, 4)
	ctx := t.Context()
	checker := searchVisibilityFixture{}
	first, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "Table", 1, "")
	if err != nil || len(first.Hits) != 1 || first.NextCursor == "" {
		t.Fatalf("first=%+v %v", first, err)
	}
	second, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "Table", 1, first.NextCursor)
	if err != nil || len(second.Hits) != 1 {
		t.Fatalf("second=%+v %v", second, err)
	}
	replay, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "Table", 1, first.NextCursor)
	if err != nil || fmt.Sprint(searchIDs(replay)) != fmt.Sprint(searchIDs(second)) || replay.NextCursor != second.NextCursor {
		t.Fatalf("replay=%+v second=%+v %v", replay, second, err)
	}
	if _, err := testSearchDocuments(t, svc, ctx, 8, nil, checker, "Table", 1, second.NextCursor); !errors.Is(err, ErrSearchContinuationNotFound) {
		t.Fatal("foreign subject resumed", err)
	}
	if _, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "Different", 1, second.NextCursor); !errors.Is(err, ErrSearchContinuationParameters) {
		t.Fatal("query changed", err)
	}
	third, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "Table", 1, second.NextCursor)
	if err != nil || len(third.Hits) != 1 || third.Hits[0].DocumentID == second.Hits[0].DocumentID {
		t.Fatalf("third=%+v %v", third, err)
	}
	if _, err := testSearchDocuments(t, svc, ctx, 7, nil, checker, "Table", 1, first.NextCursor); !errors.Is(err, ErrSearchContinuationNotFound) {
		t.Fatal("acknowledged batch remained replayable", err)
	}
	// Two simultaneous retries of the same input must publish the same batch.
	var group sync.WaitGroup
	responses := make(chan *SearchContinuationResult, 2)
	failures := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			r, e := testSearchDocuments(t, svc, ctx, 7, nil, checker, "Table", 1, third.NextCursor)
			responses <- r
			failures <- e
		}()
	}
	group.Wait()
	close(responses)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for r := range responses {
		if len(r.Hits) != 1 {
			t.Fatalf("final=%+v", r)
		}
	}
}

func TestSearchContinuationProjectionFailureRetainsUnpublishedBatch(t *testing.T) {
	svc, engine := continuationServiceFixture(t, 3)
	checker := searchVisibilityFixture{}
	first, err := testSearchDocuments(t, svc, t.Context(), 7, nil, checker, "Table", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	engine.failProjection = true
	engine.mu.Unlock()
	if r, err := testSearchDocuments(t, svc, t.Context(), 7, nil, checker, "Table", 1, first.NextCursor); err == nil || r != nil {
		t.Fatal("projection failure published cursor", r, err)
	}
	second, err := testSearchDocuments(t, svc, t.Context(), 7, nil, checker, "Table", 1, first.NextCursor)
	if err != nil || len(second.Hits) != 1 || second.Hits[0].DocumentID == first.Hits[0].DocumentID {
		t.Fatalf("retry=%+v %v", second, err)
	}
	third, err := testSearchDocuments(t, svc, t.Context(), 7, nil, checker, "Table", 1, second.NextCursor)
	if err != nil || len(third.Hits) != 1 {
		t.Fatalf("tail=%+v %v", third, err)
	}
	end, err := testSearchDocuments(t, svc, t.Context(), 7, nil, checker, "Table", 1, third.NextCursor)
	if err != nil || len(end.Hits) != 0 || !end.Exhausted || end.NextCursor != "" {
		t.Fatalf("end=%+v %v", end, err)
	}

}

type changingSearchChecker struct {
	base             searchVisibilityFixture
	checks, changeAt int
}

func (c *changingSearchChecker) CheckManagerSearchRead(ctx context.Context, credential string, request shared.SourceSearchCheckRequest) (*shared.SourceSearchCheck, error) {
	c.checks++
	if c.checks >= c.changeAt {
		return nil, commonClient.ErrSearchContextChanged
	}
	return c.base.CheckManagerSearchRead(ctx, credential, request)
}
func TestSearchContinuationChangedBasisDiscardsEntireSession(t *testing.T) {
	svc, _ := continuationServiceFixture(t, 3)
	first, err := testSearchDocuments(t, svc, t.Context(), 7, nil, searchVisibilityFixture{}, "Table", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	checker := &changingSearchChecker{changeAt: 2}
	result, err := testSearchDocuments(t, svc, t.Context(), 7, nil, checker, "Table", 1, first.NextCursor)
	if result != nil || !errors.Is(err, ErrSearchContextChanged) || svc.searchSessions.bytes != 0 {
		t.Fatalf("basis change retained output/state: %+v %v bytes=%d", result, err, svc.searchSessions.bytes)
	}
	if _, err = testSearchDocuments(t, svc, t.Context(), 7, nil, searchVisibilityFixture{}, "Table", 1, first.NextCursor); !errors.Is(err, ErrSearchContinuationNotFound) {
		t.Fatal(err)
	}
}
func TestSearchContinuationExpiryCapacityAndMaterialChange(t *testing.T) {
	for _, mode := range []string{"expired", "capacity", "material"} {
		t.Run(mode, func(t *testing.T) {
			svc, _ := continuationServiceFixture(t, 3)
			first, err := testSearchDocuments(t, svc, t.Context(), 7, nil, searchVisibilityFixture{}, "Table", 1, "")
			if err != nil {
				t.Fatal(err)
			}
			cursor, _ := uuid.Parse(first.NextCursor)
			session, err := svc.searchSessions.find(cursor, searchSubject(searchTestActor(7)))
			if err != nil {
				t.Fatal(err)
			}
			expected := ErrSearchContinuationExpired
			switch mode {
			case "expired":
				session.expires = time.Now().Add(-time.Second)
			case "capacity":
				svc.searchSessions.perQueryBytes = session.bytes
				expected = ErrSearchCapacityExceeded
			case "material":
				expected = ErrSearchContextChanged
				if err := svc.deliveries.WithTransaction(t.Context(), func(tx *gorm.DB) error {
					return tx.Model(&models.ContentIndexOutlet{}).Where("index_name=?", "content").Update("search_revision", 99).Error
				}); err != nil {
					t.Fatal(err)
				}
			}
			result, err := testSearchDocuments(t, svc, t.Context(), 7, nil, searchVisibilityFixture{}, "Table", 1, first.NextCursor)
			if result != nil || !errors.Is(err, expected) || svc.searchSessions.bytes != 0 {
				t.Fatalf("result=%+v err=%v bytes=%d", result, err, svc.searchSessions.bytes)
			}
		})
	}
}

type partialFailureSearchStream struct {
	hit   searchSourceHit
	reads int
}

func (s *partialFailureSearchStream) read(context.Context, *searchStep) ([]searchSourceHit, error) {
	s.reads++
	if s.reads == 1 {
		return []searchSourceHit{s.hit}, errors.New("source unavailable after partial input")
	}
	return nil, nil
}
func TestSearchContinuationRetainsPartialSourceInputAcrossFailedOperation(t *testing.T) {
	svc, engine := continuationServiceFixture(t, 1)
	id, _ := decodeSearchIdentity(engine.docs[0]["document_id"].(string))
	session := &searchSession{service: svc, tenant: 7, nonce: uuid.New()}
	if err := svc.searchSessions.create(session); err != nil {
		t.Fatal(err)
	}
	defer session.close()
	raw := &partialFailureSearchStream{hit: searchSourceHit{searchSourceCandidate: searchSourceCandidate{ID: id}, Kind: searchKeywordContent}}
	stream := &searchAuthorizedStream{session: session, raw: raw}
	step, err := session.newStep("addp_at_test", searchVisibilityFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := stream.peek(t.Context(), step); err == nil || len(stream.pending) != 1 {
		t.Fatal("failed source lost its already-read input", err)
	}
	step.finish()
	retry, err := session.newStep("addp_at_test", searchVisibilityFixture{})
	if err != nil {
		t.Fatal(err)
	}
	defer retry.finish()
	hit, ok, err := stream.peek(t.Context(), retry)
	if err != nil || !ok || hit.ID != id || raw.reads != 1 {
		t.Fatal("retry skipped/re-read pending input", hit, ok, raw.reads, err)
	}
	stream.consume()
	if _, ok, err := stream.peek(t.Context(), retry); err != nil || ok || raw.reads != 2 {
		t.Fatal("source EOF incorrect", ok, raw.reads, err)
	}
}
func TestSearchRegistryExpiresIdleStateAndCloseReleasesAllBytes(t *testing.T) {
	registry := newSearchSessionRegistry()
	sessions := []*searchSession{{bytes: 100}, {bytes: 200}}
	for _, session := range sessions {
		if err := registry.create(session); err != nil {
			t.Fatal(err)
		}
	}
	sessions[0].expires = time.Now().Add(-time.Second)
	registry.pruneExpired()
	if registry.bytes != 200 || !sessions[0].closed || sessions[1].closed {
		t.Fatal("idle expiry retained bytes", registry.bytes)
	}
	registry.closeAll()
	if registry.bytes != 0 || !sessions[1].closed {
		t.Fatal("close retained state", registry.bytes)
	}
}
