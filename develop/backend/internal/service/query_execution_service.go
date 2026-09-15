package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

type QueryExecutionService struct {
	executor *DevExecutor
	queries  *repository.QueryExecutionRepository
}

func NewQueryExecutionService(
	executor *DevExecutor,
	queries *repository.QueryExecutionRepository,
) (*QueryExecutionService, error) {
	if executor == nil || executor.sqlEngine == nil || executor.taskExecutionRepo == nil || queries == nil {
		return nil, fmt.Errorf("Develop Query Execution Service dependencies are required")
	}
	return &QueryExecutionService{executor: executor, queries: queries}, nil
}

func (s *QueryExecutionService) Execute(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
) error {
	startedAt := time.Now().UTC()
	if execution == nil || execution.ExecutionID != lease.ExecutionID || execution.TenantID != lease.TenantID ||
		execution.Module != commonExecution.ModuleDevelop || execution.TaskType != commonExecution.TaskTypeQuery {
		return fmt.Errorf("claimed Develop query execution is invalid")
	}
	if execution.Source != commonExecution.ModuleDevelop && execution.Source != commonExecution.ModuleOrchestrator {
		return s.completeFailure(ctx, execution, lease, startedAt, fmt.Errorf("unsupported query source %q", execution.Source), "develop.query.source_invalid")
	}
	devTask, err := devQueryTaskFromExecution(execution)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.snapshot_invalid")
	}
	executionID, err := uuid.Parse(execution.ExecutionID)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.execution_invalid")
	}

	_, relationResult, err := relationParameterBindingsFromContent(devTask.Content)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.relation_parameters_invalid")
	}
	if relationResult && execution.Source == commonExecution.ModuleOrchestrator {
		if execution.ParentExecutionID == nil {
			return s.completeFailure(ctx, execution, lease, startedAt, fmt.Errorf("orchestrator query has no parent execution"), "develop.query.parent_invalid")
		}
		parentExecutionID, parseErr := uuid.Parse(strings.TrimSpace(*execution.ParentExecutionID))
		if parseErr != nil {
			return s.completeFailure(ctx, execution, lease, startedAt, parseErr, "develop.query.parent_invalid")
		}
		return s.executeExistingTableResult(ctx, execution, lease, devTask, parentExecutionID, executionID, startedAt)
	}
	return s.executeOrdinary(ctx, execution, lease, devTask, executionID, startedAt)
}

func (s *QueryExecutionService) executeOrdinary(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	devTask *models.DevTask,
	executionID uuid.UUID,
	startedAt time.Time,
) error {
	authorization, attach, err := s.ordinaryAuthorization(ctx, execution, lease, devTask, executionID)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.authorization_failed")
	}
	if attach {
		if err := s.attachAuthorization(ctx, execution, lease, authorization); err != nil {
			return err
		}
	}
	return s.executeAndComplete(ctx, execution, lease, devTask, authorization, startedAt)
}

func (s *QueryExecutionService) ordinaryAuthorization(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	devTask *models.DevTask,
	executionID uuid.UUID,
) (*IssuedSQLExecutionAuthorization, bool, error) {
	if execution.Source == commonExecution.ModuleDevelop {
		authorization, err := s.persistedAuthorization(ctx, execution, devTask)
		return authorization, false, err
	}
	if execution.ParentExecutionID == nil {
		return nil, false, fmt.Errorf("orchestrator query has no parent execution")
	}
	parentExecutionID, err := uuid.Parse(strings.TrimSpace(*execution.ParentExecutionID))
	if err != nil {
		return nil, false, fmt.Errorf("invalid parent execution: %w", err)
	}
	if s.executor.isFederatedQuery(ctx, devTask, uint(execution.TenantID)) {
		engineIDs, err := s.executor.federatedReadEngineIDs(ctx, devTask, uint(execution.TenantID))
		if err != nil {
			return nil, false, err
		}
		authorization, err := s.executor.sqlEngine.IssueFederatedReadExecutionAuthorizationFromExecution(
			ctx, uint(execution.TenantID), parentExecutionID, executionID, engineIDs,
			lease.Attempt, lease.Token, devTask.Timeout,
		)
		return authorization, true, err
	}
	engineID := devTask.GetEngineID()
	queryText, _ := devTask.Content["query"].(string)
	if engineID == nil || *engineID == 0 || strings.TrimSpace(queryText) == "" {
		return nil, false, fmt.Errorf("Develop query snapshot has no engine or query")
	}
	if devTask.GetQueryType() != "sql" {
		timeout := s.executor.sqlEngine.normalizedTimeoutForTenant(ctx, uint(execution.TenantID), devTask.Timeout)
		authorization, err := s.executor.sqlEngine.issueExecutionAuthorizationFromExecution(
			ctx, uint(execution.TenantID), parentExecutionID, executionID, lease.Attempt, lease.Token,
			[]uint{*engineID}, []SQLExecutionEffect{SQLExecutionEffectRead}, int64(timeout+30), commonExecution.AudienceDevelop,
		)
		return authorization, true, err
	}
	authorization, err := s.executor.sqlEngine.IssueSQLExecutionAuthorizationFromExecution(
		ctx, uint(execution.TenantID), parentExecutionID, executionID, *engineID,
		lease.Attempt, lease.Token, queryText, devTask.Timeout,
	)
	return authorization, true, err
}

func (s *QueryExecutionService) persistedAuthorization(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	devTask *models.DevTask,
) (*IssuedSQLExecutionAuthorization, error) {
	if execution.ExecutionAuthorizationID == nil || *execution.ExecutionAuthorizationID <= 0 ||
		execution.AuthorizationExpiresAt == nil || !execution.AuthorizationExpiresAt.After(time.Now().UTC()) ||
		execution.ActorPrincipalID == nil || *execution.ActorPrincipalID <= 0 ||
		execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID <= 0 ||
		execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion <= 0 {
		return nil, fmt.Errorf("persisted query execution authorization is incomplete or expired")
	}
	engineID := devTask.GetEngineID()
	if engineID == nil || *engineID == 0 {
		return nil, fmt.Errorf("Develop query snapshot has no engine")
	}
	engineIDs := []uint{*engineID}
	effects := []SQLExecutionEffect{SQLExecutionEffectRead}
	if s.executor.isFederatedQuery(ctx, devTask, uint(execution.TenantID)) {
		resolved, err := s.executor.federatedReadEngineIDs(ctx, devTask, uint(execution.TenantID))
		if err != nil {
			return nil, err
		}
		engineIDs = resolved
	} else if devTask.GetQueryType() == "sql" {
		queryText, _ := devTask.Content["query"].(string)
		effect, _, err := s.executor.sqlEngine.sqlExecutionAuthorizationRequest(queryText, devTask.Timeout)
		if err != nil {
			return nil, err
		}
		effects = []SQLExecutionEffect{effect}
	}
	return &IssuedSQLExecutionAuthorization{
		AuthorizationID: *execution.ExecutionAuthorizationID, Effects: effects, EngineIDs: engineIDs,
		ActorPrincipalID: *execution.ActorPrincipalID, ActorTenantMembershipID: *execution.ActorTenantMembershipID,
		IssuedAuthorizationVersion: *execution.IssuedAuthorizationVersion,
		ExpiresAt:                  execution.AuthorizationExpiresAt.UTC(),
	}, nil
}

func (s *QueryExecutionService) executeExistingTableResult(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	devTask *models.DevTask,
	parentExecutionID, executionID uuid.UUID,
	startedAt time.Time,
) error {
	relationLocators, targetLocator, err := relationRuntimeInputs(devTask.Content, devTask.ExecutionConfig)
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
	if err := s.queries.UpdateWithLease(ctx, lease, map[string]interface{}{
		"progress": 30, "current_step": "执行查询",
	}); err != nil {
		return err
	}
	engine, err := s.executor.sqlEngine.executionEngine(ctx, uint(execution.TenantID), executionID, *engineID, authorization)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.engine_access_failed")
	}
	compiled, err := compileExistingTableResultQuery(devTask, relationLocators, targetLocator, engine.EngineType)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.relation_compile_failed")
	}
	rawInputs, _ := mapValue(devTask.ExecutionConfig["runtime_inputs"])
	mode, _ := rawInputs["write_mode"].(string)
	if mode != "append" && mode != "overwrite" {
		return s.completeFailure(ctx, execution, lease, startedAt, fmt.Errorf("write_mode must be append or overwrite"), "develop.query.runtime_inputs_invalid")
	}
	timeout := s.executor.sqlEngine.normalizedTimeoutForTenant(ctx, uint(execution.TenantID), devTask.Timeout)
	writeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	statement, _ := compiled.Content["query"].(string)
	count, err := dbbridge.ExecuteTableResult(writeCtx, engine, target, statement, compiled.RuntimeParameters, mode)
	if err != nil {
		return s.completeFailure(ctx, execution, lease, startedAt, err, "develop.query.write_failed")
	}
	rowsAffected := &count
	metadata := tableResultExecutionMetadata(execution.ExecutionID, relationLocators, targetLocator, mode, count)
	return s.completeSuccess(ctx, execution, lease, startedAt, metadata, rowsAffected)
}

// Called only after the relation contract is compiled and the write commits.
// Orchestration dependencies are deliberately not an input to resource lineage.
func tableResultExecutionMetadata(executionID string, inputs map[string]string, targetLocator, mode string, count int64) commonModels.JSONMap {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	refs := make([]commonExecution.LineageResourceRef, 0, len(names))
	ports := make([]string, 0, len(names))
	for _, name := range names {
		port := "input." + name
		refs = append(refs, commonExecution.LineageResourceRef{Port: port, Locator: inputs[name]})
		ports = append(ports, port)
	}
	writeMode := mode
	if mode == "overwrite" {
		writeMode = "replace"
	}
	return commonModels.JSONMap{
		"outputs": commonModels.JSONMap{"execution_id": executionID, "target_locator": targetLocator, "row_count": count},
		"lineage_facts": &commonExecution.LineageFacts{
			SchemaVersion: commonExecution.LineageFactsSchemaVersion,
			Inputs:        refs,
			Outputs:       []commonExecution.LineageResourceRef{{Port: "target", Locator: targetLocator, WriteMode: writeMode}},
			Operations:    []commonExecution.LineageOperation{{Kind: "derive", Operator: "develop", InputPorts: ports, OutputPorts: []string{"target"}}},
		},
	}
}

func (s *QueryExecutionService) executeAndComplete(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	devTask *models.DevTask,
	authorization *IssuedSQLExecutionAuthorization,
	startedAt time.Time,
) error {
	if err := s.queries.UpdateWithLease(ctx, lease, map[string]interface{}{
		"progress": 30, "current_step": "执行查询",
	}); err != nil {
		return err
	}
	result, errorMessage, rowsAffected, errorCode := s.executor.executeQuery(
		ctx, devTask, execution.ExecutionID, execution.TenantID, authorization,
	)
	if errorMessage != "" {
		return s.completeQueryError(ctx, execution, lease, startedAt, result, rowsAffected, errorCode, errorMessage)
	}
	metadata := queryExecutionMetadata(result)
	return s.completeSuccess(ctx, execution, lease, startedAt, metadata, rowsAffected)
}

func (s *QueryExecutionService) attachAuthorization(
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

func (s *QueryExecutionService) completeQueryError(
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

func (s *QueryExecutionService) completeFailure(
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

func (s *QueryExecutionService) completeSuccess(
	ctx context.Context,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	startedAt time.Time,
	metadata commonModels.JSONMap,
	rowsAffected *int64,
) error {
	if err := s.queries.CompleteWithLease(
		ctx, execution, lease, commonExecution.ExecutionStatusSuccess, time.Now().UTC(),
		terminalQueryFields(startedAt, metadata, rowsAffected),
	); err != nil {
		return err
	}
	if metadata["lineage_facts"] != nil {
		s.executor.notifyExecutionLineage(ctx, uint(execution.TenantID), execution.ExecutionID)
	}
	return nil
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

func relationRuntimeInputs(content, config models.DevTaskContent) (map[string]string, string, error) {
	raw, ok := mapValue(config["runtime_inputs"])
	if !ok {
		return nil, "", fmt.Errorf("runtime_inputs are required")
	}
	bindings, hasRelationParameters, err := relationParameterBindingsFromContent(content)
	if err != nil || !hasRelationParameters {
		if err == nil {
			err = fmt.Errorf("relation query parameters are required")
		}
		return nil, "", err
	}
	relationLocators := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		value, exists := mapValue(raw[binding.Name])
		locatorText, valueOK := value["locator"].(string)
		locator, parseErr := resourcetree.ParseURI(strings.TrimSpace(locatorText))
		if !exists || !valueOK || parseErr != nil || locator.Type != resourcetree.TypeTable {
			return nil, "", fmt.Errorf("query parameter %s must bind a table ResourceLocator", binding.Name)
		}
		relationLocators[binding.Name] = locator.ToURI()
	}
	targetText, ok := raw["target_locator"].(string)
	target, parseErr := resourcetree.ParseURI(strings.TrimSpace(targetText))
	if !ok || parseErr != nil || target.Type != resourcetree.TypeTable {
		return nil, "", fmt.Errorf("target_locator must identify a table")
	}
	return relationLocators, target.ToURI(), nil
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
