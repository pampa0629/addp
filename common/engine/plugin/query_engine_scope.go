package plugin

import (
	"errors"
	"fmt"
	"sort"
)

// ErrQueryEngineReadScopeUnresolved means the provider has not proved an
// exhaustive engine boundary. It is not evidence that the query reads nothing.
var ErrQueryEngineReadScopeUnresolved = errors.New("query engine read scope unresolved")

// QueryEngineReadScope is an exhaustive upper bound supplied by the provider
// that owns the immutable execution plan. It contains no authorization facts.
// A non-nil empty scope proves that no engine data can be read; nil is unknown.
type QueryEngineReadScope struct {
	EngineIDs []uint
}

func NewQueryEngineReadScope(engineIDs ...uint) (*QueryEngineReadScope, error) {
	ids := append([]uint{}, engineIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	unique := ids[:0]
	for _, id := range ids {
		if id == 0 {
			return nil, fmt.Errorf("%w: engine ID is required", ErrQueryEngineReadScopeUnresolved)
		}
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	return &QueryEngineReadScope{EngineIDs: unique}, nil
}

func (s *QueryEngineReadScope) Validate() error {
	if s == nil {
		return ErrQueryEngineReadScopeUnresolved
	}
	for i, id := range s.EngineIDs {
		if id == 0 || i > 0 && id <= s.EngineIDs[i-1] {
			return fmt.Errorf("%w: engine IDs must be positive, sorted and unique", ErrQueryEngineReadScopeUnresolved)
		}
	}
	return nil
}

func (s *QueryEngineReadScope) Clone() *QueryEngineReadScope {
	if s == nil {
		return nil
	}
	return &QueryEngineReadScope{EngineIDs: append([]uint{}, s.EngineIDs...)}
}

func (s *QueryEngineReadScope) Contains(engineID uint) bool {
	if s == nil || engineID == 0 {
		return false
	}
	i := sort.Search(len(s.EngineIDs), func(i int) bool { return s.EngineIDs[i] >= engineID })
	return i < len(s.EngineIDs) && s.EngineIDs[i] == engineID
}
