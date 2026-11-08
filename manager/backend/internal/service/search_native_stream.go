package service

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"unsafe"

	"github.com/addp/manager/internal/repository"
	"github.com/meilisearch/meilisearch-go"
)

type searchLeaf struct {
	Head             searchSourceCandidate
	Segment          int64
	Position, Length int
}
type searchLeafHeap []searchLeaf

func (h searchLeafHeap) Len() int { return len(h) }
func (h searchLeafHeap) Less(i, j int) bool {
	if h[i].Head.Score != h[j].Head.Score {
		return h[i].Head.Score > h[j].Head.Score
	}
	return h[i].Head.Ordinal < h[j].Head.Ordinal
}
func (h searchLeafHeap) Swap(i, j int)   { h[i], h[j] = h[j], h[i] }
func (h *searchLeafHeap) Push(value any) { *h = append(*h, value.(searchLeaf)) }
func (h *searchLeafHeap) Pop() any {
	n := len(*h) - 1
	row := (*h)[n]
	(*h)[n] = searchLeaf{}
	*h = (*h)[:n]
	return row
}

type searchLeafCache struct {
	Valid   bool
	Segment int64
	Used    uint64
	Rows    []searchSourceCandidate
}
type searchNativeKeyword struct {
	session                 *searchSession
	kind                    uint8
	rootProbed, initialized bool
	nextSegment             int64
	leaves                  searchLeafHeap
	cache                   [8]searchLeafCache
	clock                   uint64
}

func (s *searchNativeKeyword) fetch(ctx context.Context, step *searchStep, segment int64) ([]searchSourceCandidate, bool, error) {
	if err := step.engine(); err != nil {
		return nil, false, err
	}
	session := s.session
	filter := buildSearchFilter(&session.tenant, session.engineFilter())
	if segment >= 0 {
		filter += fmt.Sprintf(" AND search_ordinal > %d AND search_ordinal <= %d", segment*session.segmentSize, (segment+1)*session.segmentSize)
	}
	page, limit := int64(1), session.segmentSize+1
	request := &meilisearch.SearchRequest{Filter: filter, Page: page, HitsPerPage: &limit, ShowRankingScore: true, ShowRankingScoreDetails: true, AttributesToRetrieve: []string{"document_id", "search_ordinal"}}
	if s.kind == searchKeywordMetadata {
		request.AttributesToSearchOn = []string{"name", "full_name", "fields.name"}
	}
	response, err := session.service.client.Index(session.service.contentIndex).SearchWithContext(ctx, session.query, request)
	if err != nil {
		return nil, false, err
	}
	if response == nil || response.Page != 1 || response.HitsPerPage != limit || response.Query != session.query {
		return nil, false, ErrSearchVisibilityUnavailable
	}
	var hits []struct {
		ID      string          `json:"document_id"`
		Ordinal int64           `json:"search_ordinal"`
		Score   *float64        `json:"_rankingScore"`
		Details json.RawMessage `json:"_rankingScoreDetails"`
	}
	if err := response.Hits.DecodeInto(&hits); err != nil {
		return nil, false, err
	}
	if len(hits) > int(limit) || response.TotalHits < int64(len(hits)) {
		return nil, false, ErrSearchVisibilityUnavailable
	}
	skipped := false
	for _, hit := range hits {
		if hit.Score == nil || math.IsNaN(*hit.Score) || math.IsInf(*hit.Score, 0) || *hit.Score < 0 || *hit.Score > 1 || len(hit.Details) == 0 {
			return nil, false, ErrSearchVisibilityUnavailable
		}
		if bytes.Contains(hit.Details, []byte(`"skipped"`)) {
			skipped = true
		}
		if hit.Ordinal <= 0 {
			return nil, false, ErrSearchContextChanged
		}
	}
	complete := !skipped && response.TotalHits == int64(len(hits)) && response.TotalHits <= session.segmentSize
	if segment >= 0 && !complete {
		return nil, false, ErrSearchVisibilityUnavailable
	}
	if segment < 0 && !complete {
		return nil, false, nil
	}
	result := make([]searchSourceCandidate, 0, len(hits))
	for _, hit := range hits {
		if id, ok := decodeSearchIdentity(hit.ID); ok {
			result = append(result, searchSourceCandidate{ID: id, Score: *hit.Score, Ordinal: hit.Ordinal})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Ordinal < result[j].Ordinal
	})
	return result, complete, nil
}
func (s *searchNativeKeyword) put(segment int64, rows []searchSourceCandidate) error {
	selected := 0
	for i := range s.cache {
		if !s.cache[i].Valid {
			selected = i
			break
		}
		if s.cache[i].Used < s.cache[selected].Used {
			selected = i
		}
	}
	old := s.cache[selected].Rows
	if err := s.session.reserve(int64(cap(rows)-cap(old)) * int64(unsafe.Sizeof(searchSourceCandidate{}))); err != nil {
		return err
	}
	s.clock++
	s.cache[selected] = searchLeafCache{Valid: true, Segment: segment, Used: s.clock, Rows: rows}
	return nil
}
func (s *searchNativeKeyword) rows(ctx context.Context, step *searchStep, segment int64) ([]searchSourceCandidate, error) {
	for i := range s.cache {
		entry := &s.cache[i]
		if entry.Valid && entry.Segment == segment {
			s.clock++
			entry.Used = s.clock
			return entry.Rows, nil
		}
	}
	rows, _, err := s.fetch(ctx, step, segment)
	if err != nil {
		return nil, err
	}
	if err = s.put(segment, rows); err != nil {
		return nil, err
	}
	return rows, nil
}
func (s *searchNativeKeyword) addLeaf(segment int64, rows []searchSourceCandidate) error {
	if len(rows) == 0 {
		return nil
	}
	if len(s.leaves) == cap(s.leaves) {
		capacity := searchGrowCapacity(cap(s.leaves), len(s.leaves)+1)
		if err := s.session.reserve(int64(capacity-cap(s.leaves)) * int64(unsafe.Sizeof(searchLeaf{}))); err != nil {
			return err
		}
		leaves := make(searchLeafHeap, len(s.leaves), capacity)
		copy(leaves, s.leaves)
		s.leaves = leaves
	}
	if err := s.put(segment, rows); err != nil {
		return err
	}
	heap.Push(&s.leaves, searchLeaf{Head: rows[0], Segment: segment, Length: len(rows)})
	return nil
}
func (s *searchNativeKeyword) prepare(ctx context.Context, step *searchStep) error {
	if s.initialized {
		return nil
	}
	if !s.rootProbed {
		rows, complete, err := s.fetch(ctx, step, -1)
		if err != nil {
			return err
		}
		if complete {
			if err := s.addLeaf(-1, rows); err != nil {
				return err
			}
			s.rootProbed = true
			s.initialized = true
			return nil
		}
		s.rootProbed = true
		if s.session.high <= 0 {
			return ErrSearchIsolated
		}
	}
	for s.nextSegment*s.session.segmentSize < s.session.high {
		rows, _, err := s.fetch(ctx, step, s.nextSegment)
		if err != nil {
			return err
		}
		if err := s.addLeaf(s.nextSegment, rows); err != nil {
			return err
		}
		s.nextSegment++
	}
	s.initialized = true
	return nil
}
func (s *searchNativeKeyword) peek(ctx context.Context, step *searchStep) (searchSourceCandidate, bool, error) {
	if err := s.prepare(ctx, step); err != nil {
		return searchSourceCandidate{}, false, err
	}
	if len(s.leaves) == 0 {
		return searchSourceCandidate{}, false, nil
	}
	return s.leaves[0].Head, true, nil
}
func (s *searchNativeKeyword) advance(ctx context.Context, step *searchStep) error {
	leaf := s.leaves[0]
	next := leaf.Position + 1
	if next < leaf.Length {
		rows, err := s.rows(ctx, step, leaf.Segment)
		if err != nil {
			return err
		}
		if len(rows) != leaf.Length || rows[leaf.Position] != leaf.Head {
			return ErrSearchContextChanged
		}
		s.leaves[0].Position = next
		s.leaves[0].Head = rows[next]
		heap.Fix(&s.leaves, 0)
	} else {
		heap.Pop(&s.leaves)
		for i := range s.cache {
			entry := &s.cache[i]
			if entry.Valid && entry.Segment == leaf.Segment {
				_ = s.session.reserve(-int64(cap(entry.Rows)) * int64(unsafe.Sizeof(searchSourceCandidate{})))
				*entry = searchLeafCache{}
				break
			}
		}
		if len(s.leaves) == 0 {
			_ = s.session.reserve(-int64(cap(s.leaves)) * int64(unsafe.Sizeof(searchLeaf{})))
			s.leaves = nil
		}
	}
	return nil
}

type searchRawKeyword struct{ metadata, content *searchNativeKeyword }

func (s *searchRawKeyword) read(ctx context.Context, step *searchStep) ([]searchSourceHit, error) {
	result := make([]searchSourceHit, 0, repository.SearchVectorBatchSize)
	for len(result) < repository.SearchVectorBatchSize {
		meta, hasMeta, err := s.metadata.peek(ctx, step)
		if err != nil {
			return result, err
		}
		content, hasContent, err := s.content.peek(ctx, step)
		if err != nil {
			return result, err
		}
		if !hasMeta && !hasContent {
			return result, nil
		}
		selected, hit := s.metadata, meta
		// The pre-authorization merge commutes with source-specific filtering.
		// Content wins equal scores, exactly as the agreed keyword tie rule.
		if hasContent && (!hasMeta || content.Score >= meta.Score) {
			selected, hit = s.content, content
		}
		if err := selected.advance(ctx, step); err != nil {
			return result, err
		}
		result = append(result, searchSourceHit{searchSourceCandidate: hit, Kind: selected.kind})
	}
	return result, nil
}

type searchRawVector struct {
	session        *searchSession
	after          repository.SearchVectorPosition
	hasAfter, done bool
}

func (s *searchRawVector) read(ctx context.Context, step *searchStep) ([]searchSourceHit, error) {
	if s.done || s.session.vectorQuery == nil {
		return nil, nil
	}
	for {
		if err := step.engine(); err != nil {
			return nil, err
		}
		var after *repository.SearchVectorPosition
		if s.hasAfter {
			after = &s.after
		}
		rows, err := s.session.service.vectorRepo.SearchReadyVectorBatch(ctx, *s.session.vectorQuery, after)
		if err != nil {
			return nil, err
		}
		result := make([]searchSourceHit, 0, len(rows))
		for _, row := range rows {
			if row.Embedding == nil {
				return nil, ErrSearchVisibilityUnavailable
			}
			emb := row.Embedding
			if id, ok := decodeSearchIdentity(emb.ItemFingerprint); ok {
				result = append(result, searchSourceHit{searchSourceCandidate: searchSourceCandidate{ID: id}, ExpectedEngine: emb.EngineID, ExpectedLocator: sha256.Sum256([]byte(emb.Locator)), Kind: searchVectorContent})
			}
		}
		if len(rows) > 0 {
			last := rows[len(rows)-1]
			old := 0
			if s.hasAfter {
				old = len(s.after.Fingerprint)
			}
			if err := s.session.reserve(int64(len(last.Embedding.ItemFingerprint) - old)); err != nil {
				return nil, err
			}
			s.after = repository.SearchVectorPosition{Distance: last.Distance, Fingerprint: strings.Clone(last.Embedding.ItemFingerprint)}
		}
		s.hasAfter = s.hasAfter || len(rows) > 0
		s.done = len(rows) < repository.SearchVectorBatchSize
		if s.done && s.hasAfter {
			_ = s.session.reserve(-int64(len(s.after.Fingerprint)))
			s.after = repository.SearchVectorPosition{}
			s.hasAfter = false
		}
		if len(result) > 0 || s.done {
			return result, nil
		}
	}
}
func (s *searchSession) initializeStreams() error {
	size := int64(2*unsafe.Sizeof(searchNativeKeyword{}) + unsafe.Sizeof(searchRawKeyword{}) + 2*unsafe.Sizeof(searchAuthorizedStream{}) + unsafe.Sizeof(searchRawVector{}))
	if err := s.reserve(size); err != nil {
		return err
	}
	rawKeyword := &searchRawKeyword{metadata: &searchNativeKeyword{session: s, kind: searchKeywordMetadata}, content: &searchNativeKeyword{session: s, kind: searchKeywordContent}}
	s.streams[0] = &searchAuthorizedStream{session: s, raw: rawKeyword}
	s.streams[1] = &searchAuthorizedStream{session: s, raw: &searchRawVector{session: s}}
	return nil
}
