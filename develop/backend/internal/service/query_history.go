package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	"github.com/addp/common/dataprotection"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/protection"
)

type queryHistoryReader interface {
	ReadQueryHistory(context.Context, int64, *dataprotection.HistoricalQueryResult, dataprotection.SubjectReference) (*dataprotection.HistoricalQueryReadResult, error)
}

func (e *DevExecutor) protectedExecutionResponse(ctx context.Context, execution *commonExecution.TaskExecution) (*models.ExecutionWithDevTask, error) {
	response := professionalExecutionResponse(execution)
	raw, exists := execution.Metadata[protection.QueryHistoryMetadataKey]
	if !exists {
		return response, nil
	}
	if execution.TaskType != commonExecution.TaskTypeQuery || e.sqlEngine == nil || e.sqlEngine.protectionGate == nil {
		return nil, protection.ErrResultRequired
	}
	facts, ok := executionUserFacts(ctx)
	tenant, principal, _, _ := executionActorIDs(facts)
	if !ok || tenant != execution.TenantID || execution.ActorPrincipalID == nil || principal != *execution.ActorPrincipalID {
		return nil, protection.ErrResultRequired
	}
	reader, ok := e.sqlEngine.protectionGate.(queryHistoryReader)
	if !ok {
		return nil, protection.ErrResultRequired
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, protection.ErrResultRequired
	}
	var history dataprotection.HistoricalQueryResult
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&history) != nil {
		return nil, protection.ErrResultRequired
	}
	result, err := reader.ReadQueryHistory(ctx, int64(tenant), &history, dataprotection.SubjectReference{Type: "user", ID: strconv.FormatInt(principal, 10)})
	if err != nil {
		return nil, err
	}
	// Detach the public envelope: response projection must not mutate saved history.
	encoded, err = json.Marshal(execution.Metadata["result"])
	if err != nil {
		return nil, protection.ErrResultRequired
	}
	var visible map[string]interface{}
	decoder = json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&visible) != nil || visible == nil {
		return nil, protection.ErrResultRequired
	}
	visible["columns"], visible["rows_count"] = result.Columns, len(result.Rows)
	visible["truncated"] = history.Truncated
	visible["summary"] = map[string]interface{}{"column_count": len(result.Columns), "preview_rows": result.Rows}
	if result.PlaintextExpiresAt != nil {
		visible["plaintext_expires_at"] = result.PlaintextExpiresAt.UTC()
	}
	response.Metadata["result"] = visible
	return response, nil
}
