package service

import (
	"context"
	"errors"
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
	var session *searchSession
	if initial {
		session = &searchSession{service: s, subject: subject, query: strings.Clone(input.query), tenant: uint(tenant), engine: engine, pageSize: input.pageSize, nonce: searchQueryNonce(), currentCursor: searchCursor()}
		session.bytes = int64(unsafe.Sizeof(searchSession{})) + int64(unsafe.Sizeof(searchRRF{})) + int64(len(session.query)) + 2*int64(input.pageSize)*int64(unsafe.Sizeof(searchRankedIdentity{}))
		if err := s.searchSessions.create(session); err != nil {
			return nil, err
		}
		session.working = make([]searchRankedIdentity, 0, input.pageSize)
		session.last = make([]searchRankedIdentity, 0, input.pageSize)
		session.rrf = newSearchRRF(session.reserve)
	} else {
		session, err = s.searchSessions.find(input.cursor, subject)
		if err != nil {
			return nil, err
		}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, ErrSearchContinuationNotFound
	}
	if !time.Now().Before(session.expires) {
		session.close()
		return nil, ErrSearchContinuationExpired
	}
	if session.query != input.query || session.engine != engine || session.pageSize != input.pageSize {
		return nil, ErrSearchContinuationParameters
	}
	if !initial && input.cursor != session.currentCursor && input.cursor != session.lastInput {
		return nil, ErrSearchContinuationNotFound
	}
	defer func() {
		if err != nil && (initial || errors.Is(err, ErrSearchContextChanged) || errors.Is(err, ErrSearchContinuationExpired) || errors.Is(err, ErrSearchCapacityExceeded) || errors.Is(err, commonClient.ErrMetadataCredentialRejected) || errors.Is(err, commonClient.ErrMetadataQueryDenied)) {
			session.close()
		}
	}()
	step, err := session.newStep(credential, checker)
	if err != nil {
		return nil, err
	}
	defer step.finish()
	if _, err = step.observe(ctx, nil); err != nil {
		return nil, err
	}
	if err = session.prepareModel(ctx, initial); err != nil {
		return nil, err
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
	session.publish(session.currentCursor, next, exhausted)
	if exhausted {
		if initial {
			session.close()
		} else {
			session.releaseCompletedWorkingState(step.bytes)
		}
	}
	return result, nil
}
