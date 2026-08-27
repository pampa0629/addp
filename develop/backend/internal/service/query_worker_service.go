package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

type QueryWorkerService struct {
	executor *DevExecutor
	queries  *repository.QueryExecutionRepository
}

func NewQueryWorkerService(
	executor *DevExecutor,
	queries *repository.QueryExecutionRepository,
) (*QueryWorkerService, error) {
	if executor == nil || executor.sqlEngine == nil || executor.taskExecutionRepo == nil || queries == nil {
		return nil, fmt.Errorf("Develop Query Worker dependencies are required")
	}
	return &QueryWorkerService{executor: executor, queries: queries}, nil
}

func (s *QueryWorkerService) Execute(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
) error {
	startedAt := time.Now().UTC()
	if execution == nil || execution.ExecutionID != lease.ExecutionID || execution.TenantID != lease.TenantID ||
		execution.Source != commonExecution.ModuleOrchestrator || execution.TaskType != commonExecution.TaskTypeQuery ||
		execution.ParentExecutionID == nil {
		return fmt.Errorf("claimed Develop query execution is invalid")
	}
	devTask, err := devQueryTaskFromExecution(execution)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.snapshot_invalid")
	}
	parentExecutionID, err := uuid.Parse(strings.TrimSpace(*execution.ParentExecutionID))
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.parent_invalid")
	}
	executionID, err := uuid.Parse(execution.ExecutionID)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.execution_invalid")
	}

	_, relationResult, err := relationInputBindings(devTask.Content)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.relation_inputs_invalid")
	}
	if relationResult {
		return s.executeExistingTableResult(ctx, execution, lease, devTask, parentExecutionID, executionID, startedAt)
	}
	return s.executeOrdinary(ctx, execution, lease, devTask, parentExecutionID, executionID, startedAt)
}

func (s *QueryWorkerService) executeOrdinary(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	devTask *models.DevTask,
	parentExecutionID, executionID uuid.UUID,
	startedAt time.Time,
) error {
	var authorization *IssuedSQLExecutionAuthorization
	var err error
	if s.executor.isFederatedQuery(ctx, devTask, uint(execution.TenantID)) {
		engineIDs, resolveErr := s.executor.federatedReadEngineIDs(ctx, devTask, uint(execution.TenantID))
		if resolveErr != nil {
			err = resolveErr
		} else {
			authorization, err = s.executor.sqlEngine.IssueFederatedReadExecutionAuthorizationFromExecution(
				ctx, uint(execution.TenantID), parentExecutionID, executionID, engineIDs, devTask.Timeout,
			)
		}
	} else {
		engineID := devTask.GetEngineID()
		queryText, _ := devTask.Content["query"].(string)
		if engineID == nil || *engineID == 0 || strings.TrimSpace(queryText) == "" {
			err = fmt.Errorf("Develop query snapshot has no engine or query")
		} else {
			authorization, err = s.executor.sqlEngine.IssueSQLExecutionAuthorizationFromExecution(
				ctx, uint(execution.TenantID), parentExecutionID, executionID, *engineID, queryText, devTask.Timeout,
			)
		}
	}
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.authorization_failed")
	}
	if err := s.attachAuthorization(ctx, execution, lease, authorization); err != nil {
		return err
	}
	return s.executeAndComplete(ctx, execution, lease, devTask, authorization, startedAt)
}

func (s *QueryWorkerService) executeExistingTableResult(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	devTask *models.DevTask,
	parentExecutionID, executionID uuid.UUID,
	startedAt time.Time,
) error {
	inputLocators, targetLocator, err := relationRuntimeInputs(devTask.ExecutionConfig)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.runtime_inputs_invalid")
	}
	engineID := devTask.GetEngineID()
	if engineID == nil || *engineID == 0 {
		return s.completeFailure(ctx, execution, lease, startedAt, fmt.Errorf("relation result query has no engine"), "develop.query.engine_invalid")
	}
	target, _ := resourcetree.ParseURI(targetLocator)
	if target == nil || target.EngineID != *engineID {
		return s.completeFailure(ctx, execution, lease, startedAt, fmt.Errorf("target_locator engine does not match execution_config.engine_id"), "develop.query.engine_mismatch")
	}
	authorization, err := s.executor.sqlEngine.IssueExistingTableWriteAuthorizationFromExecution(
		ctx, uint(execution.TenantID), parentExecutionID, executionID, *engineID,
		lease.Attempt, lease.Token, devTask.Timeout,
	)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.authorization_failed")
	}
	if err := s.attachAuthorization(ctx, execution, lease, authorization); err != nil {
		return err
	}
	engine, err := s.executor.sqlEngine.executionEngine(ctx, uint(execution.TenantID), executionID, *engineID, authorization)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.engine_access_failed")
	}
	compiled, err := compileExistingTableResultQuery(devTask, inputLocators, targetLocator, engine.EngineType)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.relation_compile_failed")
	}
	result, errorMessage, rowsAffected, errorCode := s.executor.executeQuery(ctx, compiled, execution.ExecutionID, execution.TenantID, authorization)
	if errorMessage != "" {
		return s.completeQueryError(ctx, execution, lease, startedAt, result, rowsAffected, errorCode, errorMessage)
	}
	metadata := queryExecutionMetadata(result)
	rowCount := int64(0)
	if rowsAffected != nil {
		rowCount = *rowsAffected
	}
	metadata["outputs"] = commonModels.JSONMap{
		"execution_id": execution.ExecutionID, "target_locator": targetLocator, "row_count": rowCount,
	}
	return s.completeSuccess(ctx, execution, lease, startedAt, metadata, rowsAffected)
}

func (s *QueryWorkerService) executeAndComplete(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	devTask *models.DevTask,
	authorization *IssuedSQLExecutionAuthorization,
	startedAt time.Time,
) error {
	result, errorMessage, rowsAffected, errorCode := s.executor.executeQuery(
		ctx, devTask, execution.ExecutionID, execution.TenantID, authorization,
	)
	if errorMessage != "" {
		return s.completeQueryError(ctx, execution, lease, startedAt, result, rowsAffected, errorCode, errorMessage)
	}
	metadata := queryExecutionMetadata(result)
	return s.completeSuccess(ctx, execution, lease, startedAt, metadata, rowsAffected)
}

func (s *QueryWorkerService) attachAuthorization(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	authorization *IssuedSQLExecutionAuthorization,
) error {
	if authorization == nil || execution.ActorPrincipalID == nil || execution.ActorTenantMembershipID == nil ||
		execution.IssuedAuthorizationVersion == nil || authorization.ActorPrincipalID != *execution.ActorPrincipalID ||
		authorization.ActorTenantMembershipID != *execution.ActorTenantMembershipID ||
		authorization.IssuedAuthorizationVersion != *execution.IssuedAuthorizationVersion {
		return fmt.Errorf("Develop query authorization lineage does not match the parent execution")
	}
	expiresAt := authorization.ExpiresAt.UTC()
	if err := s.queries.UpdateWithLease(ctx, lease, map[string]interface{}{
		"execution_authorization_id": authorization.AuthorizationID,
		"authorization_expires_at":   expiresAt,
	}); err != nil {
		return err
	}
	execution.ExecutionAuthorizationID = &authorization.AuthorizationID
	execution.AuthorizationExpiresAt = &expiresAt
	return nil
}

func (s *QueryWorkerService) completeQueryError(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	startedAt time.Time,
	result commonModels.JSONMap,
	rowsAffected *int64,
	errorCode, message string,
) error {
	status := executionStatusForError(message)
	fields := terminalQueryFields(startedAt, queryExecutionMetadata(result), rowsAffected)
	fields["error_details"] = commonModels.JSONMap{"message": message, "error_code": errorCode, "details": message}
	return s.queries.CompleteWithLease(ctx, execution, lease, status, time.Now().UTC(), fields)
}

func (s *QueryWorkerService) completeFailure(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	startedAt time.Time,
	cause error,
	code string,
) error {
	fields := terminalQueryFields(startedAt, nil, nil)
	fields["error_details"] = commonModels.JSONMap{"code": code, "message": cause.Error()}
	if err := s.queries.CompleteWithLease(ctx, execution, lease, commonExecution.ExecutionStatusFailed, time.Now().UTC(), fields); err != nil {
		return fmt.Errorf("%v; complete Develop query failure: %w", cause, err)
	}
	return nil
}

func (s *QueryWorkerService) completeSuccess(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	startedAt time.Time,
	metadata commonModels.JSONMap,
	rowsAffected *int64,
) error {
	return s.queries.CompleteWithLease(
		ctx, execution, lease, commonExecution.ExecutionStatusSuccess, time.Now().UTC(),
		terminalQueryFields(startedAt, metadata, rowsAffected),
	)
}

func devQueryTaskFromExecution(execution *commonExecution.TaskExecution) (*models.DevTask, error) {
	if execution == nil || execution.ExecutionConfig == nil {
		return nil, fmt.Errorf("Develop query execution snapshot is missing")
	}
	content, ok := mapValue(execution.ExecutionConfig["content"])
	if !ok {
		return nil, fmt.Errorf("Develop query execution content snapshot is missing")
	}
	timeout, ok := positiveInt(execution.ExecutionConfig["timeout"])
	if !ok {
		return nil, fmt.Errorf("Develop query execution timeout snapshot is invalid")
	}
	runtimeParameters, _ := mapValue(execution.ExecutionConfig["runtime_parameters"])
	config := models.DevTaskContent{"engine_id": execution.ExecutionConfig["engine_id"]}
	task := &models.DevTask{
		DevType: commonExecution.TaskTypeQuery, Content: models.DevTaskContent(content),
		ExecutionConfig: config, Timeout: timeout, TenantID: uint(execution.TenantID),
		RuntimeParameters: runtimeParameters,
	}
	if err := validateDevTaskExecutionConfig(task.DevType, task.Content, task.ExecutionConfig); err != nil {
		return nil, err
	}
	if runtimeInputs, exists := execution.ExecutionConfig["runtime_inputs"]; exists {
		task.ExecutionConfig["runtime_inputs"] = runtimeInputs
	}
	return task, nil
}

func terminalQueryFields(startedAt time.Time, metadata commonModels.JSONMap, rowsAffected *int64) map[string]interface{} {
	fields := map[string]interface{}{
		"progress": 100, "current_step": nil,
		"execution_time_ms": time.Since(startedAt).Milliseconds(),
		"rows_affected":     rowsAffected,
	}
	if metadata != nil {
		fields["metadata"] = metadata
	}
	return fields
}

func queryExecutionMetadata(result commonModels.JSONMap) commonModels.JSONMap {
	metadata := commonModels.JSONMap{}
	if result == nil {
		return metadata
	}
	metadata["result"] = result
	if payload, err := json.Marshal(result); err == nil {
		metadata["result_size_bytes"] = int64(len(payload))
	}
	return metadata
}

func relationRuntimeInputs(config models.DevTaskContent) (map[string]string, string, error) {
	raw, ok := mapValue(config["runtime_inputs"])
	if !ok {
		return nil, "", fmt.Errorf("runtime_inputs are required")
	}
	rawLocators, ok := mapValue(raw["input_locators"])
	if !ok {
		return nil, "", fmt.Errorf("input_locators are required")
	}
	inputLocators := make(map[string]string, len(rawLocators))
	for name, value := range rawLocators {
		locatorText, valueOK := value.(string)
		locator, parseErr := resourcetree.ParseURI(strings.TrimSpace(locatorText))
		if !valueOK || parseErr != nil || locator.Type != resourcetree.TypeTable {
			return nil, "", fmt.Errorf("input_locators.%s must identify a table", name)
		}
		inputLocators[name] = locator.ToURI()
	}
	targetText, ok := raw["target_locator"].(string)
	target, err := resourcetree.ParseURI(strings.TrimSpace(targetText))
	if !ok || err != nil || target.Type != resourcetree.TypeTable {
		return nil, "", fmt.Errorf("target_locator must identify a table")
	}
	return inputLocators, target.ToURI(), nil
}

func mapValue(value interface{}) (map[string]interface{}, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed, true
	case commonModels.JSONMap:
		return map[string]interface{}(typed), true
	case models.DevTaskContent:
		return map[string]interface{}(typed), true
	default:
		return nil, false
	}
}

func positiveInt(value interface{}) (int, bool) {
	parsed, ok := positiveInt64(value)
	return int(parsed), ok && int64(int(parsed)) == parsed
}
