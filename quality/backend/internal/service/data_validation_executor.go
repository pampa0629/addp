package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

const (
	gateConfigInvalidCode       = "quality.data_validation.config_invalid"
	gateReadContextFailedCode   = "quality.data_validation.read_context_failed"
	gateUnsupportedEngineCode   = "quality.data_validation.unsupported_engine"
	gateAuthorizationFailedCode = "quality.data_validation.authorization_failed"
	gateCompileFailedCode       = "quality.data_validation.assertion_compile_failed"
	gateSQLFailedCode           = "quality.data_validation.sql_execution_failed"
	gateAssertionFailedCode     = "quality.data_validation.assertion_failed"
	gateResultInvalidCode       = "quality.data_validation.result_invalid"
)

type dataValidationExecutionConfig struct {
	SchemaVersion     string                          `json:"schema_version"`
	TaskVersion       int64                           `json:"task_version"`
	TableBindings     []DataValidationTableBinding    `json:"table_bindings"`
	Assertions        DataValidationAssertionDocument `json:"assertions"`
	ParentExecutionID string                          `json:"parent_execution_id"`
	CheckTimeoutMS    int64                           `json:"check_timeout_ms"`
}

type DataValidationAssertionResult struct {
	AssertionKey string                 `json:"assertion_key"`
	Type         string                 `json:"type"`
	Severity     string                 `json:"severity"`
	Passed       bool                   `json:"passed"`
	FailedCount  int64                  `json:"failed_count"`
	Observed     map[string]interface{} `json:"observed"`
}

type DataValidationResult struct {
	Assertions []DataValidationAssertionResult `json:"assertions"`
	Passed     bool                            `json:"passed"`
}

type gateCompiledAssertion struct {
	Assertion DataValidationAssertion
	SQL       string
	Args      []interface{}
	RowCount  *gateRowCountParams
}

type gateCounts struct {
	TotalCount  int64 `gorm:"column:total_count"`
	FailedCount int64 `gorm:"column:failed_count"`
}

func (e *CheckExecutor) processPendingDataValidation(ctx context.Context, workerID string) bool {
	execution, task, err := e.gateTaskRepo.ClaimPendingExecution(ctx, workerID, time.Now().UTC(), e.workerLease)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("quality data validation claim failed: %v", err)
		}
		return false
	}
	if execution == nil || task == nil {
		return false
	}
	e.workerActive.Add(1)
	defer e.workerActive.Add(-1)
	lease, err := commonExecution.LeaseFromExecution(*execution)
	if err != nil {
		log.Printf("quality data validation %s has invalid lease: %v", execution.ExecutionID, err)
		return true
	}
	config, err := decodeDataValidationExecutionConfig(execution.ExecutionConfig)
	if err != nil {
		e.completeDataValidation(ctx, task, execution, lease, nil, failExecution(gateConfigInvalidCode, err), false)
		return true
	}
	gateCtx, cancel := context.WithTimeout(ctx, time.Duration(config.CheckTimeoutMS)*time.Millisecond)
	heartbeatDone := make(chan error, 1)
	go e.renewGateLease(gateCtx, cancel, lease, heartbeatDone)
	result, execErr := e.doDataValidation(gateCtx, task, execution, lease, config)
	timedOut := errors.Is(gateCtx.Err(), context.DeadlineExceeded) || errors.Is(execErr, context.DeadlineExceeded)
	execErr = executionErrorForDeadline(execErr, timedOut)
	cancel()
	if heartbeatErr := <-heartbeatDone; heartbeatErr != nil {
		log.Printf("quality data validation %s lease renewal failed: %v", execution.ExecutionID, heartbeatErr)
		return true
	}
	e.completeDataValidation(ctx, task, execution, lease, result, execErr, timedOut)
	return true
}

func decodeDataValidationExecutionConfig(config commonModels.JSONMap) (*dataValidationExecutionConfig, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var snapshot dataValidationExecutionConfig
	if err := decodeStrictJSON(raw, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.SchemaVersion != dataValidationExecutionConfigVersion || snapshot.TaskVersion <= 0 || snapshot.ParentExecutionID == "" || snapshot.CheckTimeoutMS <= 0 {
		return nil, fmt.Errorf("data validation execution config is invalid")
	}
	assertionsRaw, _ := json.Marshal(snapshot.Assertions)
	if _, err := validateDataValidationContract(snapshot.TableBindings, assertionsRaw); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (e *CheckExecutor) doDataValidation(ctx context.Context, task *models.DataValidationTask, execution *commonExecution.TaskExecution, lease commonExecution.Lease, config *dataValidationExecutionConfig) (*DataValidationResult, error) {
	if task.Version != config.TaskVersion || execution.ParentExecutionID == nil || *execution.ParentExecutionID != config.ParentExecutionID {
		return nil, failExecution(gateConfigInvalidCode, fmt.Errorf("data validation task or parent changed"))
	}
	first, _ := resourcetree.ParseURI(config.TableBindings[0].Locator)
	engineID := int64(first.EngineID)

	authorizationID := ""
	if execution.ExecutionAuthorizationID == nil {
		issued, issueErr := e.systemClient.WithTenantID(uint(task.TenantID)).IssueExecutionAuthorizationFromExecution(ctx, commonClient.IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: config.ParentExecutionID, Audience: commonExecution.AudienceQuality,
			ExecutionID: execution.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token,
			Accesses: []commonClient.ExecutionEngineAccessScope{{EngineID: strconv.FormatInt(engineID, 10), Effects: []string{"read"}}}, ExpiresIn: 3600,
		})
		if issueErr != nil {
			return nil, failExecution(gateAuthorizationFailedCode, issueErr)
		}
		authorizationFields, fieldErr := commonClient.TaskExecutionAuthorizationFields(issued)
		if fieldErr != nil {
			return nil, failExecution(gateAuthorizationFailedCode, fieldErr)
		}
		if err := e.gateTaskRepo.AttachExecutionAuthorization(ctx, lease, authorizationFields); err != nil {
			return nil, failExecution(gateAuthorizationFailedCode, err)
		}
		authorizationID = issued.ID
	} else {
		authorizationID = strconv.FormatInt(*execution.ExecutionAuthorizationID, 10)
	}
	engineAccess, err := e.systemClient.WithTenantID(uint(task.TenantID)).GetExecutionEngineAccess(ctx, authorizationID, commonClient.ExecutionEngineAccessRequest{
		ExecutionID: execution.ExecutionID, EngineID: strconv.FormatInt(engineID, 10), RequiredEffects: []string{"read"},
	})
	if err != nil {
		return nil, failExecution(gateAuthorizationFailedCode, err)
	}
	if engineAccess.Engine == nil || !strings.EqualFold(engineAccess.Engine.EngineType, "postgresql") {
		return nil, failExecution(gateUnsupportedEngineCode, fmt.Errorf("data validation only supports PostgreSQL"))
	}
	targetDB, err := dbbridge.GetOrCreatePool(engineAccess.Engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, failExecution(gateSQLFailedCode, err)
	}
	var result *DataValidationResult
	err = targetDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runErr error
		result, runErr = runDataValidation(ctx, tx, config)
		return runErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
func runDataValidation(ctx context.Context, targetDB *gorm.DB, config *dataValidationExecutionConfig) (*DataValidationResult, error) {
	readContext, err := readValidationTables(targetDB.WithContext(ctx), config.TableBindings)
	if err != nil {
		return nil, failExecution(gateReadContextFailedCode, err)
	}
	compiled, aliases, err := compileDataValidation(config, readContext)
	if err != nil {
		return nil, failExecution(gateCompileFailedCode, err)
	}
	_ = aliases
	result := &DataValidationResult{Assertions: make([]DataValidationAssertionResult, 0, len(compiled)), Passed: true}
	for _, item := range compiled {
		var counts gateCounts
		if err := targetDB.WithContext(ctx).Raw(item.SQL, item.Args...).Scan(&counts).Error; err != nil {
			return nil, failExecution(gateSQLFailedCode, fmt.Errorf("execute assertion %s: %w", item.Assertion.AssertionKey, err))
		}
		if counts.TotalCount < 0 || counts.FailedCount < 0 || counts.FailedCount > counts.TotalCount {
			return nil, failExecution(gateResultInvalidCode, fmt.Errorf("assertion %s returned invalid counts", item.Assertion.AssertionKey))
		}
		passed := counts.FailedCount == 0
		observed := map[string]interface{}{"total_count": counts.TotalCount}
		if item.RowCount != nil {
			observed["row_count"] = counts.TotalCount
			passed = gateRowCountPassed(counts.TotalCount, *item.RowCount)
			if !passed {
				counts.FailedCount = 1
			}
		}
		result.Assertions = append(result.Assertions, DataValidationAssertionResult{AssertionKey: item.Assertion.AssertionKey, Type: item.Assertion.Type, Severity: item.Assertion.Severity, Passed: passed, FailedCount: counts.FailedCount, Observed: observed})
		if !passed && item.Assertion.Severity == "error" {
			result.Passed = false
		}
	}
	if !result.Passed {
		return result, failExecution(gateAssertionFailedCode, fmt.Errorf("one or more error assertions failed"))
	}
	return result, nil
}

func compileDataValidation(config *dataValidationExecutionConfig, readContext *validationReadContext) ([]gateCompiledAssertion, map[string]validationReadItem, error) {
	if len(readContext.Items) != len(config.TableBindings) {
		return nil, nil, fmt.Errorf("physical table context does not match bindings")
	}
	aliases := make(map[string]validationReadItem, len(config.TableBindings))
	for index, binding := range config.TableBindings {
		item := readContext.Items[index]
		if item.Locator != binding.Locator {
			return nil, nil, fmt.Errorf("physical table context order changed")
		}
		aliases[binding.Alias] = item
	}
	compiled := make([]gateCompiledAssertion, 0, len(config.Assertions.Assertions))
	for _, assertion := range config.Assertions.Assertions {
		item, err := compileGateAssertion(assertion, aliases)
		if err != nil {
			return nil, nil, fmt.Errorf("assertion %s: %w", assertion.AssertionKey, err)
		}
		compiled = append(compiled, item)
	}
	return compiled, aliases, nil
}

func compileGateAssertion(assertion DataValidationAssertion, aliases map[string]validationReadItem) (gateCompiledAssertion, error) {
	dialect := query.ForDialect(query.DialectPostgreSQL)
	tableSQL := func(alias string) (string, map[string]struct{}, error) {
		item, exists := aliases[alias]
		if !exists {
			return "", nil, fmt.Errorf("table alias is not bound")
		}
		locator, err := resourcetree.ParseURI(item.Locator)
		if err != nil || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 || int64(locator.EngineID) != item.EngineID {
			return "", nil, fmt.Errorf("table locator is invalid")
		}
		columns := make(map[string]struct{}, len(item.Columns))
		for _, column := range item.Columns {
			columns[column.Name] = struct{}{}
		}
		return dialect.QualifiedTable(locator.Path[0], locator.Path[1]), columns, nil
	}
	columnSQL := func(columns map[string]struct{}, column string) (string, error) {
		if _, exists := columns[column]; !exists {
			return "", fmt.Errorf("column %q is not present in physical table", column)
		}
		return dialect.QuoteIdentifier(column), nil
	}
	compiled := gateCompiledAssertion{Assertion: assertion}
	switch assertion.Type {
	case "not_null":
		var params gateNotNullParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		column, err := columnSQL(columns, params.Column)
		if err != nil {
			return compiled, err
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE %s IS NULL) AS failed_count FROM %s", column, table)
	case "allowed_values":
		var params gateAllowedValuesParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		column, err := columnSQL(columns, params.Column)
		if err != nil {
			return compiled, err
		}
		placeholders := make([]string, len(params.Values))
		compiled.Args = make([]interface{}, len(params.Values))
		for index, value := range params.Values {
			placeholders[index] = "$" + strconv.Itoa(index+1)
			compiled.Args[index] = value
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE %s IS NOT NULL AND %s::text NOT IN (%s)) AS failed_count FROM %s", column, column, strings.Join(placeholders, ", "), table)
	case "unique_key":
		var params gateUniqueKeyParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		quoted := make([]string, len(params.Columns))
		for i, name := range params.Columns {
			quoted[i], err = columnSQL(columns, name)
			if err != nil {
				return compiled, err
			}
		}
		nonNull := make([]string, len(quoted))
		for i, name := range quoted {
			nonNull[i] = name + " IS NOT NULL"
		}
		compiled.SQL = fmt.Sprintf("SELECT (SELECT COUNT(*) FROM %s) AS total_count, (SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE %s GROUP BY %s HAVING COUNT(*) > 1) AS duplicate_groups) AS failed_count", table, table, strings.Join(nonNull, " AND "), strings.Join(quoted, ", "))
	case "foreign_key":
		var params gateForeignKeyParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil {
			return compiled, err
		}
		child, childColumns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		parent, parentColumns, err := tableSQL(params.ReferenceTable)
		if err != nil {
			return compiled, err
		}
		eligible, matches := make([]string, len(params.Columns)), make([]string, len(params.Columns))
		for i := range params.Columns {
			childColumn, err := columnSQL(childColumns, params.Columns[i])
			if err != nil {
				return compiled, err
			}
			parentColumn, err := columnSQL(parentColumns, params.ReferenceColumns[i])
			if err != nil {
				return compiled, err
			}
			eligible[i] = "child." + childColumn + " IS NOT NULL"
			matches[i] = "parent." + parentColumn + " = child." + childColumn
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE %s AND NOT EXISTS (SELECT 1 FROM %s AS parent WHERE %s)) AS failed_count FROM %s AS child", strings.Join(eligible, " AND "), parent, strings.Join(matches, " AND "), child)
	case "predicate_implication":
		var params gatePredicateImplicationParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		whenSQL, whenArgs, err := compileGateCondition(params.When, columns, dialect, 1)
		if err != nil {
			return compiled, err
		}
		thenSQL, thenArgs, err := compileGateCondition(params.Then, columns, dialect, len(whenArgs)+1)
		if err != nil {
			return compiled, err
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE (%s) IS TRUE AND NOT ((%s) IS TRUE)) AS failed_count FROM %s", whenSQL, thenSQL, table)
		compiled.Args = append(whenArgs, thenArgs...)
	case "row_count":
		var params gateRowCountParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil {
			return compiled, err
		}
		table, _, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, 0::bigint AS failed_count FROM %s", table)
		compiled.RowCount = &params
	default:
		return compiled, fmt.Errorf("unsupported assertion type")
	}
	return compiled, nil
}

func compileGateCondition(condition gateCondition, columns map[string]struct{}, dialect query.Dialect, firstParameter int) (string, []interface{}, error) {
	if _, exists := columns[condition.Column]; !exists {
		return "", nil, fmt.Errorf("condition column is not present in physical table")
	}
	column := dialect.QuoteIdentifier(condition.Column)
	switch condition.Operator {
	case "eq":
		return column + " = $" + strconv.Itoa(firstParameter), []interface{}{condition.Value}, nil
	case "not_eq":
		return column + " <> $" + strconv.Itoa(firstParameter), []interface{}{condition.Value}, nil
	case "is_null":
		return column + " IS NULL", nil, nil
	case "is_not_null":
		return column + " IS NOT NULL", nil, nil
	case "is_true":
		return column + " IS TRUE", nil, nil
	case "is_false":
		return column + " IS FALSE", nil, nil
	default:
		return "", nil, fmt.Errorf("condition op is unsupported")
	}
}

func gateRowCountPassed(count int64, params gateRowCountParams) bool {
	if params.Exact != nil {
		return count == *params.Exact
	}
	if params.Min != nil && count < *params.Min {
		return false
	}
	if params.Max != nil && count > *params.Max {
		return false
	}
	return true
}

func (e *CheckExecutor) renewGateLease(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(e.workerLease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := e.gateTaskRepo.RenewLease(ctx, lease, now.UTC().Add(e.workerLease)); err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				cancel()
				done <- err
				return
			}
		}
	}
}

func (e *CheckExecutor) completeDataValidation(ctx context.Context, task *models.DataValidationTask, execution *commonExecution.TaskExecution, lease commonExecution.Lease, result *DataValidationResult, execErr error, timedOut bool) {
	completedAt := time.Now().UTC()
	status := commonExecution.ExecutionStatusSuccess
	fields := map[string]interface{}{"progress": 100}
	if execution.StartedAt != nil {
		fields["execution_time_ms"] = completedAt.Sub(*execution.StartedAt).Milliseconds()
	}
	if result != nil {
		fields["metadata"] = commonModels.JSONMap{
			"schema_version": dataValidationResultVersion,
			"assertions":     result.Assertions, "passed": result.Passed,
			"outputs": commonModels.JSONMap{"passed": result.Passed},
		}
	}
	if timedOut {
		status = commonExecution.ExecutionStatusTimeout
		fields["error_details"] = commonModels.JSONMap{"code": qualityExecutionTimeout, "message": "quality data validation timed out"}
	} else if execErr != nil {
		status = commonExecution.ExecutionStatusFailed
		fields["error_details"] = commonModels.JSONMap{"code": executionFailureCode(execErr), "message": "quality data validation failed"}
	}
	if err := e.gateTaskRepo.CompleteExecutionWithLease(ctx, task.ID, task.TenantID, lease, status, fields, completedAt); err != nil {
		log.Printf("quality data validation %s completion failed: %v", execution.ExecutionID, err)
	}
}

// Validation consumes only explicit, same-engine physical resource identities.
type validationColumn struct{ Name string }
type validationReadItem struct {
	Locator  string
	EngineID int64
	Columns  []validationColumn
}
type validationReadContext struct{ Items []validationReadItem }

func readValidationTables(db *gorm.DB, bindings []DataValidationTableBinding) (*validationReadContext, error) {
	result := &validationReadContext{Items: make([]validationReadItem, 0, len(bindings))}
	for _, b := range bindings {
		locator, err := resourcetree.ParseURI(b.Locator)
		if err != nil {
			return nil, err
		}
		item := validationReadItem{Locator: b.Locator, EngineID: int64(locator.EngineID)}
		if err := db.Raw("SELECT column_name AS name FROM information_schema.columns WHERE table_schema=? AND table_name=? ORDER BY ordinal_position", locator.Path[0], locator.Path[1]).Scan(&item.Columns).Error; err != nil {
			return nil, err
		}
		if len(item.Columns) == 0 {
			return nil, fmt.Errorf("table %s is missing or unreadable", b.Alias)
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
