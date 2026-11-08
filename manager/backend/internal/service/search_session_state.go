package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"unsafe"

	shared "github.com/addp/common/authorization"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

var ErrSearchContinuationNotFound = errors.New("search continuation not found")
var ErrSearchContinuationExpired = errors.New("search continuation expired")
var ErrSearchContinuationParameters = errors.New("search continuation parameters differ")
var errSearchStepBudget = errors.New("search step budget reached")

const searchStateMaxSessions = 64
const searchStateMaxBytes int64 = 64 << 20
const searchStatePerQueryBytes int64 = 8 << 20
const searchStateTTL = 5 * time.Minute
const searchStepEngineCalls = 32
const searchStepCandidates = 2000
const searchDecisionSlots = 4096
const searchKeywordMetadata uint8 = 1
const searchKeywordContent uint8 = 2
const searchVectorContent uint8 = 3

type searchSourceCandidate struct {
	ID      searchIdentity
	Score   float64
	Ordinal int64
}
type searchSourceHit struct {
	searchSourceCandidate
	ExpectedLocator [32]byte
	ExpectedEngine  uint
	Kind            uint8
}
type searchCandidateStream interface {
	peek(context.Context, *searchStep) (searchSourceHit, bool, error)
	consume()
}
type searchDecision struct {
	ID      searchIdentity
	Locator [32]byte
	Engine  uint
	Flags   uint8
	Used    bool
}

type searchSession struct {
	mu                                 sync.Mutex
	registry                           *searchSessionRegistry
	service                            *HybridSearchService
	subject                            [32]byte
	query                              string
	tenant, engine                     uint
	pageSize                           int
	nonce                              uuid.UUID
	observationTag                     [32]byte
	observationSet                     bool
	expires                            time.Time
	currentCursor, lastInput, lastNext uuid.UUID
	lastExhausted                      bool
	last, working                      []searchRankedIdentity
	bytes                              int64
	closed                             bool
	operation                          uint64
	rrf                                *searchRRF
	streams                            [2]searchCandidateStream
	material                           [32]byte
	high                               int64
	segmentSize                        int64
	vectorQuery                        *repository.SearchVectorQuery
	modelKey                           [32]byte
}
type searchSessionRegistry struct {
	mu                             sync.Mutex
	sessions                       [searchStateMaxSessions]*searchSession
	bytes, maxBytes, perQueryBytes int64
	ttl                            time.Duration
}

func newSearchSessionRegistry() *searchSessionRegistry {
	return &searchSessionRegistry{maxBytes: searchStateMaxBytes, perQueryBytes: searchStatePerQueryBytes, ttl: searchStateTTL}
}
func (r *searchSessionRegistry) create(s *searchSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked()
	for i, old := range r.sessions {
		if old == nil {
			if s.bytes > r.perQueryBytes || r.bytes+s.bytes > r.maxBytes {
				return ErrSearchCapacityExceeded
			}
			s.registry = r
			s.expires = time.Now().Add(r.ttl)
			r.sessions[i] = s
			r.bytes += s.bytes
			return nil
		}
	}
	return ErrSearchCapacityExceeded
}
func (r *searchSessionRegistry) pruneLocked() {
	for i, old := range r.sessions {
		if old != nil && time.Now().After(old.expires) && old.mu.TryLock() {
			r.bytes -= old.bytes
			old.closed = true
			old.bytes = 0
			old.releaseReferences()
			r.sessions[i] = nil
			old.mu.Unlock()
		}
	}
}
func (r *searchSessionRegistry) pruneExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked()
}
func (r *searchSessionRegistry) closeAll() {
	r.mu.Lock()
	sessions := r.sessions
	r.mu.Unlock()
	for _, session := range sessions {
		if session != nil {
			session.mu.Lock()
			session.close()
			session.mu.Unlock()
		}
	}
}
func (r *searchSessionRegistry) find(cursor uuid.UUID, subject [32]byte) (*searchSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s != nil && !s.closed && s.subject == subject && (s.currentCursor == cursor || s.lastInput == cursor) {
			return s, nil
		}
	}
	return nil, ErrSearchContinuationNotFound
}
func (s *searchSession) reserve(delta int64) error {
	r := s.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.closed {
		if delta < 0 {
			return nil
		}
		return ErrSearchContinuationNotFound
	}
	if delta > 0 && (s.bytes+delta > r.perQueryBytes || r.bytes+delta > r.maxBytes) {
		return ErrSearchCapacityExceeded
	}
	s.bytes += delta
	r.bytes += delta
	return nil
}
func (s *searchSession) close() {
	r := s.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.closed {
		return
	}
	for i, entry := range r.sessions {
		if entry == s {
			r.sessions[i] = nil
			break
		}
	}
	r.bytes -= s.bytes
	s.bytes = 0
	s.closed = true
	s.releaseReferences()
}
func (s *searchSession) publish(input, next uuid.UUID, exhausted bool) {
	r := s.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	s.lastInput = input
	s.lastNext = next
	s.currentCursor = next
	s.lastExhausted = exhausted
	s.last = append(s.last[:0], s.working...)
	s.working = s.working[:0]
}

func searchSubject(current shared.AuthContext) [32]byte {
	data, _ := json.Marshal(struct {
		Principal  shared.AuthPrincipal
		Context    shared.AuthSessionContext
		Client     shared.ClientConstraints
		Delegation *shared.DelegationFacts
	}{current.Principal, current.Context, current.Client, current.Delegation})
	return sha256.Sum256(data)
}
func searchCursor() uuid.UUID     { return uuid.New() }
func searchQueryNonce() uuid.UUID { return uuid.New() }

// Tokens and decision reuse exist only within this HTTP operation, never in a
// retained session. Output targets are checked again without this cache.
type searchStep struct {
	session                 *searchSession
	credential              string
	checker                 SearchVisibilityChecker
	number                  uint64
	engineCalls, candidates int
	decisions               []searchDecision
	decisionCount           int
	bytes                   int64
}

func (s *searchSession) newStep(credential string, checker SearchVisibilityChecker) (*searchStep, error) {
	if err := s.reserve(int64(unsafe.Sizeof(searchStep{}))); err != nil {
		return nil, err
	}
	s.operation++
	return &searchStep{session: s, credential: credential, checker: checker, number: s.operation, bytes: int64(unsafe.Sizeof(searchStep{}))}, nil
}
func (step *searchStep) finish() {
	_ = step.session.reserve(-step.bytes)
	step.decisions = nil
}
func (step *searchStep) engine() error {
	if step.engineCalls >= searchStepEngineCalls {
		return errSearchStepBudget
	}
	step.engineCalls++
	return nil
}
func (step *searchStep) input(n int) error {
	if step.candidates+n > searchStepCandidates {
		return errSearchStepBudget
	}
	step.candidates += n
	return nil
}
func (step *searchStep) decision(id searchIdentity) *searchDecision {
	if len(step.decisions) == 0 {
		return nil
	}
	position := int(searchIdentityHash(id) & uint64(len(step.decisions)-1))
	for {
		entry := &step.decisions[position]
		if !entry.Used || entry.ID == id {
			return entry
		}
		position = (position + 1) & (len(step.decisions) - 1)
	}
}
func (step *searchStep) ensureDecision(id searchIdentity) (*searchDecision, error) {
	if entry := step.decision(id); entry != nil && entry.Used {
		return entry, nil
	}
	if len(step.decisions) == 0 || (step.decisionCount+1)*10 > len(step.decisions)*6 {
		capacity := searchGrowCapacity(len(step.decisions), len(step.decisions)+1)
		delta := int64(capacity-len(step.decisions)) * int64(unsafe.Sizeof(searchDecision{}))
		if err := step.session.reserve(delta); err != nil {
			return nil, err
		}
		step.bytes += delta
		old := step.decisions
		step.decisions = make([]searchDecision, capacity)
		for _, entry := range old {
			if entry.Used {
				*step.decision(entry.ID) = entry
			}
		}
	}
	entry := step.decision(id)
	entry.Used = true
	entry.ID = id
	step.decisionCount++
	return entry, nil
}

func (s *HybridSearchService) searchMaterial(ctx context.Context, tenant uint) ([32]byte, int64, error) {
	var empty [32]byte
	if err := s.deliveries.RequireActive(ctx, s.epoch); err != nil {
		if errors.Is(err, repository.ErrContentIndexIsolated) {
			return empty, 0, ErrSearchIsolated
		}
		return empty, 0, err
	}
	initialized, err := s.protectionStore.Initialized(ctx, int64(tenant))
	if err != nil {
		return empty, 0, err
	}
	if !initialized {
		return empty, 0, ErrSearchIsolated
	}
	checkpoint, err := s.protectionStore.CurrentCursor(ctx, int64(tenant))
	if err != nil {
		return empty, 0, err
	}
	remote, err := s.client.GetIndexWithContext(ctx, s.contentIndex)
	if err != nil {
		return empty, 0, err
	}
	if remote == nil || remote.UID != s.contentIndex || remote.PrimaryKey != "id" || remote.CreatedAt.IsZero() || remote.UpdatedAt.IsZero() {
		return empty, 0, ErrSearchIsolated
	}
	local, err := s.deliveries.SearchMaterial(ctx, s.epoch, tenant)
	if err != nil {
		if errors.Is(err, repository.ErrContentIndexIsolated) {
			return empty, 0, ErrSearchIsolated
		}
		return empty, 0, err
	}
	encoded, _ := json.Marshal(struct {
		Local            repository.SearchMaterialRevision
		UID              string
		Created, Updated time.Time
		Protection       string
	}{local, remote.UID, remote.CreatedAt, remote.UpdatedAt, checkpoint})
	return sha256.Sum256(encoded), local.NextSearchOrdinal, nil
}

func (s *searchSession) releaseReferences() {
	s.query = ""
	s.rrf = nil
	s.streams = [2]searchCandidateStream{}
	s.vectorQuery = nil
	s.last = nil
	s.working = nil
}

func (s *searchSession) releaseCompletedWorkingState(stepBytes int64) {
	if s.rrf == nil {
		return
	}
	retained := int64(unsafe.Sizeof(searchSession{})) + int64(len(s.query)) + int64(cap(s.last)+cap(s.working))*int64(unsafe.Sizeof(searchRankedIdentity{})) + stepBytes
	if s.vectorQuery != nil {
		retained += int64(unsafe.Sizeof(repository.SearchVectorQuery{})) + int64(len(s.vectorQuery.ModelProfileID)) + int64(cap(s.vectorQuery.Vector))*4
	}
	_ = s.reserve(retained - s.bytes)
	s.rrf = nil
	s.streams = [2]searchCandidateStream{}
}
