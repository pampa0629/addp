package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/protection"
)

const (
	queryResultRetention     = 5 * time.Minute
	queryResultMaxBytes      = 64 << 20
	queryResultMaxEntryBytes = 8 << 20
	queryResultMaxEntries    = 128
)

type queryResultReader interface {
	ReadQueryResult(context.Context, int64, *dataprotection.AuthorizedQueryResult, dataprotection.SubjectReference) (*dataprotection.AuthorizedQueryReadResult, error)
}

type liveQueryResult struct {
	value      *dataprotection.AuthorizedQueryResult
	attempt    int
	generation string
	expires    time.Time
	bytes      int
	timer      *time.Timer
}

// queryResultCache is confined to the Backend process. No persistent fallback.
type queryResultCache struct {
	mu      sync.Mutex
	entries map[string]liveQueryResult
	bytes   int
}

func newQueryResultCache() *queryResultCache {
	return &queryResultCache{entries: make(map[string]liveQueryResult)}
}

func (s *queryResultCache) removeLocked(id string) {
	if item, ok := s.entries[id]; ok {
		item.timer.Stop()
		s.bytes -= item.bytes
		delete(s.entries, id)
	}
}

func (s *queryResultCache) remove(id, generation string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.entries[id]; ok && item.generation == generation {
		s.removeLocked(id)
	}
}

func (s *queryResultCache) put(id string, attempt int, generation string, value *dataprotection.AuthorizedQueryResult, deadline *time.Time) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > queryResultMaxEntryBytes {
		return fmt.Errorf("authorized query result exceeds the delivery budget")
	}
	var detached dataprotection.AuthorizedQueryResult
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&detached); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, exists := s.entries[id]; exists && old.attempt > attempt {
		return fmt.Errorf("query result delivery attempt is stale")
	}
	s.removeLocked(id)
	for len(s.entries) >= queryResultMaxEntries || s.bytes+len(encoded) > queryResultMaxBytes {
		oldest := ""
		for key, item := range s.entries {
			if oldest == "" || item.expires.Before(s.entries[oldest].expires) {
				oldest = key
			}
		}
		s.removeLocked(oldest)
	}
	expires := time.Now().UTC().Add(queryResultRetention)
	if deadline != nil && deadline.Before(expires) {
		expires = deadline.UTC()
	}
	if !time.Now().Before(expires) {
		return fmt.Errorf("query result authorization expired")
	}
	// The timer captures identities only, never a result that could outlive eviction.
	timer := time.AfterFunc(time.Until(expires), func() { s.remove(id, generation) })
	s.entries[id] = liveQueryResult{value: &detached, attempt: attempt, generation: generation, expires: expires, bytes: len(encoded), timer: timer}
	s.bytes += len(encoded)
	return nil
}

func (s *queryResultCache) get(id string, attempt int, tenant int64, subject dataprotection.SubjectReference) (liveQueryResult, bool) {
	if s == nil {
		return liveQueryResult{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.entries[id]
	if ok && !time.Now().Before(item.expires) {
		s.removeLocked(id)
		ok = false
	}
	if !ok || item.attempt != attempt || item.value.TenantID != tenant || item.value.Subject != subject {
		return liveQueryResult{}, false
	}
	return item, true
}

// GetQueryResult is the current workbench delivery path. GetExecution/ListExecutions
// always return the default protected preview, even while a grant is active.
func (e *DevExecutor) GetQueryResult(ctx context.Context, executionID string, tenantID uint) (*models.ExecutionWithDevTask, error) {
	response, err := e.GetExecution(ctx, executionID, tenantID)
	if err != nil {
		return nil, err
	}
	return e.queryResultResponse(ctx, response, tenantID)
}

func (e *DevExecutor) queryResultResponse(ctx context.Context, response *models.ExecutionWithDevTask, tenantID uint) (*models.ExecutionWithDevTask, error) {
	facts, ok := executionUserFacts(ctx)
	tenant, _, _, _ := executionActorIDs(facts)
	if !ok || tenant != int(tenantID) {
		return nil, protection.ErrResultRequired
	}
	if response.TaskType != commonExecution.TaskTypeQuery {
		return nil, protection.ErrResultRequired
	}
	if response.Status != commonExecution.ExecutionStatusSuccess {
		return response, nil
	}
	subject := dataprotection.SubjectReference{Type: "user", ID: facts.Principal.ID}
	item, exists := e.queryResults.get(response.ExecutionID, response.Attempt, int64(tenantID), subject)
	if !exists {
		return response, nil
	}
	if e.sqlEngine == nil {
		return nil, protection.ErrResultRequired
	}
	reader, ok := e.sqlEngine.protectionGate.(queryResultReader)
	if !ok {
		return nil, protection.ErrResultRequired
	}
	result, err := reader.ReadQueryResult(ctx, int64(tenantID), item.value, subject)
	if err != nil {
		return nil, err
	}
	if !time.Now().Before(item.expires) {
		e.queryResults.remove(response.ExecutionID, item.generation)
		return response, nil
	}
	encoded, err := json.Marshal(response.Metadata["result"])
	if err != nil {
		return nil, protection.ErrResultRequired
	}
	var visible map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&visible) != nil || visible == nil {
		return nil, protection.ErrResultRequired
	}
	visible["columns"], visible["rows_count"], visible["truncated"] = result.Columns, len(result.Rows), item.value.Truncated
	wireRows, err := datatype.RowsForJSON(result.Fields, result.Rows)
	if err != nil {
		return nil, protection.ErrResultRequired
	}
	visible["fields"] = result.Fields
	visible["summary"] = map[string]interface{}{"column_count": len(result.Columns), "preview_rows": wireRows}
	if result.PlaintextExpiresAt == nil {
		e.queryResults.remove(response.ExecutionID, item.generation)
	}
	if result.PlaintextExpiresAt != nil {
		until := *result.PlaintextExpiresAt
		if item.expires.Before(until) {
			until = item.expires
		}
		visible["plaintext_expires_at"] = until.UTC()
	}
	response.Metadata["result"] = visible
	return response, nil
}
