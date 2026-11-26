package service

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unsafe"

	shared "github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
)

type searchContinuationInput struct {
	query    string
	pageSize int
	cursor   uuid.UUID
}

// ParseSearchContinuation is the HTTP boundary; internal iteration receives
// fixed query parameters and a canonical opaque session reference.
func ParseSearchContinuation(query, size, cursor string) (searchContinuationInput, error) {
	input := searchContinuationInput{query: truncateQueryRunes(strings.TrimSpace(query), queryEmbeddingRuneLimit), pageSize: 10}
	if input.query == "" {
		return input, ErrSearchContinuationParameters
	}
	var err error
	if size != "" {
		input.pageSize, err = strconv.Atoi(size)
		if err != nil || input.pageSize < 1 || input.pageSize > 50 {
			return input, ErrSearchContinuationParameters
		}
	}
	if cursor != "" {
		input.cursor, err = uuid.Parse(cursor)
		if err != nil || input.cursor == uuid.Nil || input.cursor.String() != cursor {
			return input, ErrSearchContinuationParameters
		}
	}
	return input, nil
}

type SearchContinuationResult struct {
	Hits       []SearchDocument `json:"results"`
	PageSize   int              `json:"page_size"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Exhausted  bool             `json:"exhausted"`
}

func (s *HybridSearchService) SearchDocuments(ctx context.Context, current shared.AuthContext, engineID *uint, credential string, checker SearchVisibilityChecker, input searchContinuationInput) (result *SearchContinuationResult, err error) {
	result, err = s.searchPage(ctx, current, engineID, credential, checker, input)
	if errors.Is(err, errCheckpointAdvanced) {
		return s.searchPage(ctx, current, engineID, credential, checker, input)
	}
	return result, err
}

func (s *HybridSearchService) searchPage(ctx context.Context, current shared.AuthContext, engineID *uint, credential string, checker SearchVisibilityChecker, input searchContinuationInput) (result *SearchContinuationResult, err error) {
	if !s.Enabled() {
		return nil, ErrSearchDisabled
	}
	if !s.initialized.Load() || !s.ordinalReady.Load() {
		return nil, ErrSearchIsolated
	}
	if s.searchSessions == nil || checker == nil || credential == "" || current.Context.TenantID == nil {
		return nil, ErrSearchVisibilityUnavailable
	}
	tenant, parseErr := strconv.ParseUint(*current.Context.TenantID, 10, 64)
	if parseErr != nil || tenant == 0 {
		return nil, ErrSearchVisibilityUnavailable
	}
	subject := searchSubject(current)
	engine := uint(0)
	if engineID != nil {
		engine = *engineID
	}
	initial := input.cursor == uuid.Nil
	if s.checkpoints == nil {
		return nil, ErrSearchVisibilityUnavailable
	}
	session := &searchSession{service: s, subject: subject, query: input.query, tenant: uint(tenant), engine: engine, pageSize: input.pageSize}
	session.bytes = int64(unsafe.Sizeof(searchSession{})) + int64(unsafe.Sizeof(searchRRF{})) + int64(len(session.query)) + 2*int64(input.pageSize)*int64(unsafe.Sizeof(searchRankedIdentity{}))
	if err := s.searchSessions.create(session); err != nil {
		return nil, err
	}
	// The local registry bounds work in flight. Committed progress lives only
	// in the dedicated cache and survives this process closing.
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, ErrSearchContinuationExpired
	}
	defer session.close()
	session.working = make([]searchRankedIdentity, 0, input.pageSize)
	session.last = make([]searchRankedIdentity, 0, input.pageSize)
	session.rrf = newSearchRRF(session.reserve)
	ref := checkpointRef{}
	var saved searchCheckpoint
	if initial {
		cacheTime, err := s.checkpoints.Time(ctx).Result()
		if err != nil {
			return nil, fmt.Errorf("%w: search cache: %w", ErrSearchVisibilityUnavailable, err)
		}
		session.nonce = searchQueryNonce()
		ref = checkpointRef{id: uuid.NewString(), expires: cacheTime.Add(searchStateTTL)}
	} else {
		var raw string
		ref, raw, err = loadSearchCheckpoint(ctx, s.checkpoints, input.cursor, subject)
		if err != nil {
			return nil, err
		}
		session.expires = time.Now().Add(ref.remaining)
		defer func() {
			if errors.Is(err, ErrSearchContextChanged) || errors.Is(err, ErrSearchContinuationExpired) || errors.Is(err, ErrSearchCapacityExceeded) || errors.Is(err, commonClient.ErrMetadataCredentialRejected) || errors.Is(err, commonClient.ErrMetadataQueryDenied) {
				_, discardErr := discardCheckpoint.Run(ctx, s.checkpoints, checkpointKeys(ref.id), ref.version, checkpointPrefix, ref.id).Result()
				if discardErr != nil {
					err = fmt.Errorf("%w: discard search progress: %w", ErrSearchVisibilityUnavailable, discardErr)
				}
			}
		}()
		if ref.remaining <= 0 {
			return nil, ErrSearchContinuationExpired
		}
		if err = gob.NewDecoder(strings.NewReader(raw)).Decode(&saved); err != nil {
			return nil, fmt.Errorf("%w: invalid search progress", ErrSearchVisibilityUnavailable)
		}
		if err = saved.restore(session); err != nil {
			return nil, err
		}
	}
	step, err := session.newStep(credential, checker)
	if err != nil {
		return nil, err
	}
	defer step.finish()
	if _, err = step.observe(ctx, nil); err != nil {
		return nil, err
	}
	if err = session.prepareModel(ctx, true); err != nil {
		return nil, err
	}
	if !initial && (session.modelKey != saved.Model || vectorFingerprint(session.vectorQuery) != saved.Vector) {
		return nil, ErrSearchContextChanged
	}
	material, high, err := s.searchMaterial(ctx, session.tenant)
	if err != nil {
		return nil, err
	}
	if initial {
		session.material = material
		session.high = high
		pagination, err := s.client.Index(s.contentIndex).GetPaginationWithContext(ctx)
		if err != nil || pagination == nil || pagination.MaxTotalHits < 2 {
			if err == nil {
				err = ErrSearchVisibilityUnavailable
			}
			return nil, err
		}
		session.segmentSize = pagination.MaxTotalHits - 1
		if session.segmentSize > 512 {
			session.segmentSize = 512
		}
		if err = session.initializeStreams(); err != nil {
			return nil, err
		}
	} else if material != session.material {
		return nil, ErrSearchContextChanged
	}
	if !initial && input.cursor == session.lastInput {
		return session.render(ctx, step, session.last, session.lastNext, session.lastExhausted)
	}
	used := 0
	budgetReached := false
	for len(session.working) < session.pageSize {
		promoted, promoteErr := session.rrf.promote(searchStepCandidates - used)
		if promoteErr != nil {
			return nil, promoteErr
		}
		used += promoted
		for len(session.working) < session.pageSize {
			row, ok := session.rrf.pop()
			if !ok {
				break
			}
			session.working = append(session.working, row)
		}
		if len(session.working) == session.pageSize || used >= searchStepCandidates || budgetReached {
			break
		}
		if session.rrf.done[0] && session.rrf.done[1] && session.rrf.count == 0 {
			break
		}
		for source, stream := range session.streams {
			if session.rrf.done[source] {
				continue
			}
			hit, exists, peekErr := stream.peek(ctx, step)
			if errors.Is(peekErr, errSearchStepBudget) {
				budgetReached = true
				break
			}
			if peekErr != nil {
				return nil, peekErr
			}
			if !exists {
				session.rrf.done[source] = true
				continue
			}
			if err = session.rrf.accept(source, hit.ID, hit.Kind); err != nil {
				return nil, err
			}
			stream.consume()
			used++
			if used >= searchStepCandidates {
				budgetReached = true
				break
			}
		}
	}
	if !time.Now().Before(session.expires) {
		session.close()
		return nil, ErrSearchContinuationExpired
	}
	exhausted := session.rrf.done[0] && session.rrf.done[1] && session.rrf.count == 0
	next := uuid.Nil
	if !exhausted {
		next = searchCursor()
	}
	result, err = session.render(ctx, step, session.working, next, exhausted)
	if err != nil {
		return nil, err
	}
	if initial && exhausted {
		return result, nil
	}
	session.publish(input.cursor, next, exhausted)
	if exhausted {
		session.releaseCompletedWorkingState(step.bytes)
	}
	if err = ref.save(ctx, s.checkpoints, session, input.cursor); err != nil {
		return nil, err
	}
	return result, nil
}
