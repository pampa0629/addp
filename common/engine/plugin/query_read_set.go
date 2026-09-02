package plugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ErrQueryReadSetUnresolved means a provider cannot prove the complete set of
// Engine Catalog leaves read by one query. Callers must not replace this with a
// partial set or a TargetPath-only fallback.
var ErrQueryReadSetUnresolved = errors.New("query read set unresolved")

// ErrQueryOutputLineageUnresolved means a provider cannot prove the complete
// field-to-result value flow for one query. Callers must not infer it from
// result column names.
var ErrQueryOutputLineageUnresolved = errors.New("query output lineage unresolved")

// ErrPreparedQueryConsumed means execution or a read session has already
// consumed a one-shot PreparedQuery.
var ErrPreparedQueryConsumed = errors.New("prepared query already consumed")

// PreparedQuery is an immutable, one-shot query plan. ReadSet and Execute are
// bound to the same request and provider-native preparation result.
type PreparedQuery interface {
	Analysis(context.Context) (*QueryAnalysis, error)
	ReadSet(context.Context) (*QueryReadSet, error)
	OutputLineage(context.Context) (*QueryOutputLineage, error)
	Execute(context.Context) (*QueryResult, error)
}

// QueryReadSet is the complete, canonical set of Engine Catalog leaves read by
// one read-only QueryRequest. It contains no Meta, Catalog, authorization, or
// data-protection facts.
type QueryReadSet struct {
	Paths []EngineCatalogPath `json:"paths"`
}

type preparedQuery struct {
	mu             sync.Mutex
	consumed       bool
	analysis       *QueryAnalysis
	readSetReady   bool
	readSet        *QueryReadSet
	readSetErr     error
	resolveReadSet func(context.Context) (*QueryReadSet, error)
	lineageReady   bool
	lineage        *QueryOutputLineage
	lineageErr     error
	resolveLineage func(context.Context, *QueryReadSet) (*QueryOutputLineage, error)
	execute        func(context.Context) (*QueryResult, error)
}

// NewPreparedQuery builds the shared one-shot state machine used by Provider
// plans. Callbacks must close over provider-owned immutable values.
func NewPreparedQuery(
	analysis *QueryAnalysis,
	resolveReadSet func(context.Context) (*QueryReadSet, error),
	resolveLineage func(context.Context, *QueryReadSet) (*QueryOutputLineage, error),
	execute func(context.Context) (*QueryResult, error),
) (PreparedQuery, error) {
	if err := analysis.Validate(); err != nil {
		return nil, err
	}
	if execute == nil {
		return nil, fmt.Errorf("prepared query execute callback is required")
	}
	return &preparedQuery{analysis: analysis.Clone(), resolveReadSet: resolveReadSet, resolveLineage: resolveLineage, execute: execute}, nil
}

func (p *preparedQuery) Analysis(context.Context) (*QueryAnalysis, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.analysis.Clone(), nil
}

func (p *preparedQuery) ReadSet(ctx context.Context) (*QueryReadSet, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resolveReadSetLocked(ctx)
	return p.readSet.Clone(), p.readSetErr
}

func (p *preparedQuery) resolveReadSetLocked(ctx context.Context) {
	if p.readSetReady {
		return
	}
	if p.consumed {
		p.readSetErr = ErrPreparedQueryConsumed
		p.readSetReady = true
		return
	}
	p.readSetReady = true
	if p.resolveReadSet == nil {
		p.readSetErr = ErrQueryReadSetUnresolved
		return
	}
	p.readSet, p.readSetErr = p.resolveReadSet(ctx)
	if p.readSet != nil {
		p.readSet = p.readSet.Clone()
	}
}

func (p *preparedQuery) OutputLineage(ctx context.Context) (*QueryOutputLineage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lineageReady {
		return p.lineage.Clone(), p.lineageErr
	}
	if p.consumed {
		return nil, ErrPreparedQueryConsumed
	}
	p.lineageReady = true
	p.resolveReadSetLocked(ctx)
	if p.readSetErr != nil {
		p.lineageErr = p.readSetErr
		return nil, p.lineageErr
	}
	if p.resolveLineage == nil {
		p.lineageErr = ErrQueryOutputLineageUnresolved
		return nil, p.lineageErr
	}
	p.lineage, p.lineageErr = p.resolveLineage(ctx, p.readSet.Clone())
	if p.lineageErr == nil {
		p.lineageErr = ValidateQueryOutputLineage(p.readSet, p.lineage)
	}
	if p.lineage != nil {
		p.lineage = p.lineage.Clone()
	}
	return p.lineage.Clone(), p.lineageErr
}

func (p *preparedQuery) Execute(ctx context.Context) (*QueryResult, error) {
	p.mu.Lock()
	if p.consumed {
		p.mu.Unlock()
		return nil, ErrPreparedQueryConsumed
	}
	p.consumed = true
	execute := p.execute
	p.mu.Unlock()
	return execute(ctx)
}

// NewQueryReadSet validates, clones, sorts, and de-duplicates catalog leaves.
func NewQueryReadSet(paths ...EngineCatalogPath) (*QueryReadSet, error) {
	normalized := make([]EngineCatalogPath, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if err := validateQueryReadPath(path); err != nil {
			return nil, err
		}
		cloned := cloneEngineCatalogPath(path)
		key := queryReadPathKey(cloned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, cloned)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return compareQueryReadPaths(normalized[i], normalized[j]) < 0
	})
	return &QueryReadSet{Paths: normalized}, nil
}

// ValidateQueryReadSet binds a normalized read set to the ordinary query it
// was resolved from. Ordinary queries cannot silently include another engine.
func ValidateQueryReadSet(req QueryRequest, readSet *QueryReadSet) error {
	if !req.Options.ReadOnly {
		return fmt.Errorf("%w: query must be read-only", ErrQueryReadSetUnresolved)
	}
	if strings.TrimSpace(req.Language) == "" || strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("%w: query language and text are required", ErrQueryReadSetUnresolved)
	}
	if req.EngineID == 0 {
		return fmt.Errorf("%w: query engine is required", ErrQueryReadSetUnresolved)
	}
	if readSet == nil {
		return fmt.Errorf("%w: read set is required", ErrQueryReadSetUnresolved)
	}
	normalized, err := NewQueryReadSet(readSet.Paths...)
	if err != nil {
		return err
	}
	if !sameQueryReadPaths(readSet.Paths, normalized.Paths) {
		return fmt.Errorf("%w: read set must be canonical, sorted, and unique", ErrQueryReadSetUnresolved)
	}
	for _, path := range readSet.Paths {
		if path.EngineID != req.EngineID {
			return fmt.Errorf("%w: catalog path engine %d does not match query engine %d", ErrQueryReadSetUnresolved, path.EngineID, req.EngineID)
		}
	}
	return nil
}

// Clone returns a copy safe for handing across owner execution layers.
func (s *QueryReadSet) Clone() *QueryReadSet {
	if s == nil {
		return nil
	}
	result := &QueryReadSet{Paths: make([]EngineCatalogPath, len(s.Paths))}
	for index, path := range s.Paths {
		result.Paths[index] = cloneEngineCatalogPath(path)
	}
	return result
}

func validateQueryReadPath(path EngineCatalogPath) error {
	if path.Version != EngineCatalogPathVersion {
		return fmt.Errorf("%w: unsupported catalog path version %q", ErrQueryReadSetUnresolved, path.Version)
	}
	if path.EngineID == 0 {
		return fmt.Errorf("%w: catalog path engine is required", ErrQueryReadSetUnresolved)
	}
	if len(path.Segments) < 2 || !IsEngineCatalogRootSegment(path.Segments[0]) {
		return fmt.Errorf("%w: catalog leaf requires an explicit structural root", ErrQueryReadSetUnresolved)
	}
	for index, segment := range path.Segments[1:] {
		if strings.TrimSpace(segment.Term) == "" || strings.TrimSpace(segment.Kind) == "" || strings.TrimSpace(segment.Name) == "" {
			return fmt.Errorf("%w: invalid catalog business segment %d", ErrQueryReadSetUnresolved, index)
		}
	}
	last := path.Segments[len(path.Segments)-1]
	if IsEngineCatalogRootSegment(last) || last.Term == EngineCatalogTermBucket || last.Term == EngineCatalogTermPrefix ||
		last.Term == EngineCatalogTermDirectory || last.Term == EngineCatalogTermSchema || last.Term == EngineCatalogTermDatabase {
		return fmt.Errorf("%w: catalog read path must identify a leaf", ErrQueryReadSetUnresolved)
	}
	return nil
}

func queryReadPathKey(path EngineCatalogPath) string {
	var result strings.Builder
	writeQueryReadKeyPart(&result, strconv.FormatUint(uint64(path.EngineID), 10))
	writeQueryReadKeyPart(&result, path.Version)
	for _, segment := range path.Segments {
		writeQueryReadKeyPart(&result, segment.Term)
		writeQueryReadKeyPart(&result, segment.Kind)
		writeQueryReadKeyPart(&result, segment.Name)
	}
	return result.String()
}

func compareQueryReadPaths(left, right EngineCatalogPath) int {
	if left.EngineID != right.EngineID {
		if left.EngineID < right.EngineID {
			return -1
		}
		return 1
	}
	if compared := strings.Compare(left.Version, right.Version); compared != 0 {
		return compared
	}
	limit := len(left.Segments)
	if len(right.Segments) < limit {
		limit = len(right.Segments)
	}
	for index := 0; index < limit; index++ {
		for _, compared := range []int{
			strings.Compare(left.Segments[index].Term, right.Segments[index].Term),
			strings.Compare(left.Segments[index].Kind, right.Segments[index].Kind),
			strings.Compare(left.Segments[index].Name, right.Segments[index].Name),
		} {
			if compared != 0 {
				return compared
			}
		}
	}
	if len(left.Segments) < len(right.Segments) {
		return -1
	}
	if len(left.Segments) > len(right.Segments) {
		return 1
	}
	return 0
}

func writeQueryReadKeyPart(result *strings.Builder, value string) {
	result.WriteString(strconv.Itoa(len(value)))
	result.WriteByte(':')
	result.WriteString(value)
	result.WriteByte('|')
}

func cloneEngineCatalogPath(path EngineCatalogPath) EngineCatalogPath {
	path.Segments = append([]EngineCatalogSegment(nil), path.Segments...)
	return path
}

func sameQueryReadPaths(left, right []EngineCatalogPath) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if queryReadPathKey(left[index]) != queryReadPathKey(right[index]) {
			return false
		}
	}
	return true
}
