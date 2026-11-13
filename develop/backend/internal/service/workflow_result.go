package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/protection"
)

var ErrWorkflowResultUnavailable = errors.New("workflow professional result unavailable")

type workflowResultSource struct {
	Provider plugin.EnginePlugin
	Target   plugin.EngineCatalogPath
}

type liveWorkflowResult struct {
	attempt    int
	generation string
	tenant     uint
	actor      int64
	expires    time.Time
	data       []byte
	sources    []workflowResultSource
	timer      *time.Timer
}

// The process holds at most 128 complete 64 KiB tables. No row data is durable.
type workflowResultCache struct {
	mu      sync.Mutex
	entries map[string]*liveWorkflowResult
}

func newWorkflowResultCache() *workflowResultCache {
	return &workflowResultCache{entries: map[string]*liveWorkflowResult{}}
}

func (c *workflowResultCache) remove(id, generation string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item := c.entries[id]; item != nil && item.generation == generation {
		item.timer.Stop()
		delete(c.entries, id)
	}
}

func (c *workflowResultCache) put(item *execution.TaskExecution, lease execution.Lease, expires time.Time, sources []workflowResultSource, table *plugin.WorkflowTableResult) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(table)
	data := bytes.TrimSpace(buffer.Bytes())
	if err != nil || len(data) > 64<<10 || item.ActorPrincipalID == nil || len(sources) == 0 {
		return ErrWorkflowResultUnavailable
	}
	if deadline := time.Now().Add(5 * time.Minute); deadline.Before(expires) {
		expires = deadline
	}
	if !time.Now().Before(expires) {
		return ErrWorkflowResultUnavailable
	}
	entry := &liveWorkflowResult{attempt: lease.Attempt, generation: lease.Token, tenant: uint(item.TenantID), actor: *item.ActorPrincipalID, expires: expires, data: data, sources: sources}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[item.ExecutionID] != nil || len(c.entries) >= 128 {
		return ErrWorkflowResultUnavailable
	}
	entry.timer = time.AfterFunc(time.Until(expires), func() { c.remove(item.ExecutionID, lease.Token) })
	c.entries[item.ExecutionID] = entry
	return nil
}

func (c *workflowResultCache) get(id string, attempt int, tenant uint, actor int64) *liveWorkflowResult {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[id]
	if entry == nil || entry.attempt != attempt || entry.tenant != tenant || entry.actor != actor || !time.Now().Before(entry.expires) {
		return nil
	}
	return entry
}

func (e *DevExecutor) workflowResultResponse(ctx context.Context, response *models.ExecutionWithDevTask, tenant uint, bearer string) (*models.ExecutionWithDevTask, error) {
	if response.Status != execution.ExecutionStatusSuccess {
		return response, nil
	}
	facts, _ := executionUserFacts(ctx) // GetExecution already enforces current origin and function permissions.
	_, actor, _, _ := executionActorIDs(facts)
	entry := e.workflowResults.get(response.ExecutionID, response.Attempt, tenant, actor)
	if entry == nil {
		return nil, ErrWorkflowResultUnavailable
	}
	if e.workflowEngine == nil || e.workflowEngine.systemService == nil || e.workflowEngine.protectionGate == nil {
		return nil, protection.ErrRequired
	}
	targets := make([]plugin.EngineCatalogPath, 0, len(entry.sources))
	for _, source := range entry.sources {
		targets = append(targets, source.Target)
	}
	if err := e.workflowEngine.systemService.CheckDevelopWorkflowResultRead(ctx, bearer, targets); err != nil {
		return nil, protection.ErrRequired
	}
	for _, source := range entry.sources {
		end, err := e.workflowEngine.protectionGate.BeginCatalogPath(ctx, tenant, source.Provider, source.Target)
		if err != nil {
			return nil, err
		}
		defer end()
	}
	if !time.Now().Before(entry.expires) {
		return nil, ErrWorkflowResultUnavailable
	}
	var table plugin.WorkflowTableResult
	decoder := json.NewDecoder(bytes.NewReader(entry.data))
	decoder.UseNumber()
	if decoder.Decode(&table) != nil {
		return nil, ErrWorkflowResultUnavailable
	}
	// Only this detached professional response receives rows; observations retain the summary.
	visible := map[string]interface{}{}
	if result, ok := mapValue(response.Metadata["result"]); ok {
		for key, value := range result {
			visible[key] = value
		}
	}
	visible["final_result"] = &table
	visible["professional_result_expires_at"] = entry.expires.UTC()
	response.Metadata["result"] = visible
	return response, nil
}
