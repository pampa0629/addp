package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataquality"
	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	qualityExecutionResultSchemaVersion    = "addp.quality.execution-result/v1"
	qualityExecutionFailedCode             = "quality.execution.failed"
	qualityExecutionAuthorizationMissing   = "quality.execution.authorization_missing"
	qualityExecutionAuthorizationFailed    = "quality.execution.authorization_failed"
	qualityExecutionEngineUnsupported      = "quality.execution.unsupported_engine"
	qualityExecutionTargetConnectionFailed = "quality.execution.target_connection_failed"
	qualityExecutionConfigInvalid          = "quality.execution.config_invalid"
	qualityExecutionRuleSnapshotInvalid    = "quality.execution.rule_snapshot_invalid"
	qualityExecutionNoRules                = "quality.execution.no_rule_applications"
	qualityExecutionRuleCompileFailed      = "quality.execution.rule_compile_failed"
	qualityExecutionSQLFailed              = "quality.execution.sql_execution_failed"
	qualityExecutionResultInvalid          = "quality.execution.result_invalid"
	qualityExecutionIssueReconcileFailed   = "quality.issue.reconcile_failed"
	qualityExecutionTimeout                = "quality.execution.timeout"
	qualityWorkerLease                     = 30 * time.Minute
	qualityWorkerPoll                      = 500 * time.Millisecond
)

type executionFailure struct {
	code string
	err  error
}

func (e *executionFailure) Error() string { return e.err.Error() }
func (e *executionFailure) Unwrap() error { return e.err }
func failExecution(code string, err error) error {
	if err == nil {
		return nil
	}
	return &executionFailure{code: code, err: err}
}
func executionFailureCode(err error) string {
	var failure *executionFailure
	if errors.As(err, &failure) && failure.code != "" {
		return failure.code
	}
	return qualityExecutionFailedCode
}

type CheckExecutor struct {
	systemClient           *commonClient.SystemServiceClient
	executionAuthorization *commonClient.SystemExecutionAuthorizationClient
	checkTaskRepo          *repository.CheckTaskRepository
	issueRepo              *repository.IssueRepository
	sqlGen                 *SQLGenerator
	checkTimeout           time.Duration
	workerConcurrency      int
	workerLease            time.Duration
	workerPoll             time.Duration
	workerID               string
	workerCancel           context.CancelFunc
	workerDone             chan struct{}
	workerStartOnce        sync.Once
}

func NewCheckExecutor(
	systemClient *commonClient.SystemServiceClient,
	executionAuthorization *commonClient.SystemExecutionAuthorizationClient,
	checkTaskRepo *repository.CheckTaskRepository,
	issueRepo *repository.IssueRepository,
	checkTimeout time.Duration,
	workerConcurrency int,
) *CheckExecutor {
	return &CheckExecutor{
		systemClient:           systemClient,
		executionAuthorization: executionAuthorization,
		checkTaskRepo:          checkTaskRepo,
		issueRepo:              issueRepo,
		sqlGen:                 NewSQLGenerator(),
		checkTimeout:           checkTimeout,
		workerConcurrency:      workerConcurrency,
		workerLease:            qualityWorkerLease,
		workerPoll:             qualityWorkerPoll,
		workerID:               "quality-" + uuid.NewString(),
		workerDone:             make(chan struct{}),
	}
}

func (e *CheckExecutor) ConfigureWorker(lease, poll time.Duration) error {
	if lease <= 0 || poll <= 0 || poll >= lease {
		return fmt.Errorf("quality worker lease and poll configuration is invalid")
	}
	e.workerLease = lease
	e.workerPoll = poll
	return nil
}

type RuleResult struct {
	RuleApplicationID int64   `json:"rule_application_id"`
	RuleKey           string  `json:"rule_key"`
	Type              string  `json:"type"`
	Severity          string  `json:"severity"`
	Message           string  `json:"message"`
	Column            string  `json:"column"`
	Table             string  `json:"table"`
	Schema            string  `json:"schema"`
	PassRate          float64 `json:"pass_rate"`
	FailedCount       int64   `json:"failed_count"`
	TotalCount        int64   `json:"total_count"`
	Passed            bool    `json:"passed"`
}

type FieldScore struct {
	Column    string  `json:"column"`
	Score     float64 `json:"score"`
	RuleCount int     `json:"rule_count"`
}

type ExecutionResult struct {
	QualityScore float64      `json:"quality_score"`
	TotalRules   int          `json:"total_rules"`
	PassedRules  int          `json:"passed_rules"`
	FailedRules  int          `json:"failed_rules"`
	FieldScores  []FieldScore `json:"field_scores"`
	RuleDetails  []RuleResult `json:"rule_details"`
}

// RunCheck creates a durable pending execution. The worker performs the work.
func (e *CheckExecutor) RunCheck(ctx context.Context, taskID, tenantID, userID int64, userAccessToken string) (string, error) {
	return e.RunCheckWithContext(ctx, taskID, tenantID, userID, userAccessToken, commonExecution.TriggerTypeManual, commonExecution.ModuleQuality, nil)
}

func (e *CheckExecutor) RunCheckWithContext(
	ctx context.Context,
	taskID, tenantID, userID int64,
	userAccessToken string,
	triggerType string,
	source string,
	parentExecutionID *string,
) (string, error) {
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	if normalizedTriggerType != commonExecution.TriggerTypeManual {
		return "", fmt.Errorf("%w: quality check only supports manual trigger_type", commonAPI.ErrBadRequest)
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = commonExecution.ModuleQuality
	}
	if userID <= 0 && parentExecutionID == nil {
		return "", fmt.Errorf("quality check requires a user principal")
	}
	executionID := uuid.NewString()
	now := time.Now().UTC()
	taskExec := &commonExecution.TaskExecution{
		ExecutionID: executionID, TenantID: int(tenantID), Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeQualityCheck, Source: normalizedSource, ParentExecutionID: parentExecutionID,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status:            commonExecution.ExecutionStatusPending, TriggerType: normalizedTriggerType,
		CreatedAt: now, UpdatedAt: now, MaxAttempts: 3,
		ExecutionConfig: commonModels.JSONMap{
			"schema_version":   qualityExecutionConfigSchemaVersion,
			"check_timeout_ms": e.checkTimeout.Milliseconds(),
		},
	}
	if userID > 0 {
		taskExec.TriggeredBy = intPtr(int(userID))
	}
	task, err := e.checkTaskRepo.ClaimExecution(ctx, taskID, tenantID, taskExec)
	if err != nil {
		return "", fmt.Errorf("claim quality check execution: %w", err)
	}
	authorization, err := e.issueExecutionAuthorization(ctx, tenantID, executionID, task.EngineID, userAccessToken, parentExecutionID)
	if err != nil {
		e.checkTaskRepo.FailPendingExecution(ctx, taskID, tenantID, executionID, "quality.authorization.issue_failed", time.Now().UTC())
		return "", fmt.Errorf("issue quality execution authorization: %w", err)
	}
	if err := e.checkTaskRepo.AttachExecutionAuthorization(ctx, tenantID, executionID, map[string]interface{}{
		"actor_principal_id": authorization.actorPrincipalID, "actor_tenant_membership_id": authorization.tenantMembershipID,
		"issued_authorization_version": authorization.authorizationVersion, "execution_authorization_id": authorization.authorizationID,
		"authorization_effects": pq.StringArray(authorization.effects), "authorization_expires_at": authorization.expiresAt,
	}); err != nil {
		e.checkTaskRepo.FailPendingExecution(ctx, taskID, tenantID, executionID, "quality.authorization.persist_failed", time.Now().UTC())
		return "", fmt.Errorf("persist quality execution authorization: %w", err)
	}
	return executionID, nil
}

type executionAuthorizationFacts struct {
	authorizationID      *int64
	actorPrincipalID     *int64
	tenantMembershipID   *int64
	authorizationVersion *int64
	effects              []string
	expiresAt            *time.Time
}

func (e *CheckExecutor) issueExecutionAuthorization(ctx context.Context, tenantID int64, executionID string, engineID int64, userAccessToken string, parentExecutionID *string) (*executionAuthorizationFacts, error) {
	engineIDs := []string{strconv.FormatInt(engineID, 10)}
	var issued *commonClient.IssuedExecutionAuthorization
	var err error
	if parentExecutionID != nil {
		issued, err = e.systemClient.WithTenantID(uint(tenantID)).IssueExecutionAuthorizationFromExecution(ctx, commonClient.IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: *parentExecutionID,
			Audience:          "addp-quality",
			ExecutionID:       executionID,
			EngineIDs:         engineIDs,
			Effects:           []string{"read"},
			ExpiresIn:         3600,
		})
	} else {
		if e.executionAuthorization == nil {
			return nil, fmt.Errorf("user execution authorization client is not configured")
		}
		issued, err = e.executionAuthorization.Issue(ctx, userAccessToken, commonClient.IssueExecutionAuthorizationRequest{
			Audience: "addp-quality", ExecutionID: executionID, EngineIDs: engineIDs, Effects: []string{"read"}, ExpiresIn: 3600,
		})
	}
	if err != nil {
		return nil, err
	}
	facts := &executionAuthorizationFacts{effects: append([]string(nil), issued.Effects...), expiresAt: &issued.ExpiresAt}
	facts.authorizationID, err = parsePositiveID(issued.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization id: %w", err)
	}
	facts.actorPrincipalID, err = parsePositiveID(issued.ActorPrincipalID)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization principal: %w", err)
	}
	facts.tenantMembershipID, err = parsePositiveID(issued.TenantMembershipID)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization membership: %w", err)
	}
	facts.authorizationVersion, err = parsePositiveID(issued.IssuedAuthorizationVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization version: %w", err)
	}
	return facts, nil
}

func parsePositiveID(value string) (*int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return nil, fmt.Errorf("invalid positive ID %q", value)
	}
	return &parsed, nil
}

// StartWorker starts the single durable execution route inside the independent
// quality-worker process. Multiple workers coordinate through PostgreSQL leases.
func (e *CheckExecutor) StartWorker(ctx context.Context) {
	e.workerStartOnce.Do(func() {
		workerCtx, cancel := context.WithCancel(ctx)
		e.workerCancel = cancel
		go e.workerSupervisor(workerCtx)
	})
}

func (e *CheckExecutor) StopWorker() {
	if e.workerCancel != nil {
		e.workerCancel()
		<-e.workerDone
	}
}

func (e *CheckExecutor) workerSupervisor(ctx context.Context) {
	defer close(e.workerDone)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		e.recoveryLoop(ctx)
	}()
	for slot := 1; slot <= e.workerConcurrency; slot++ {
		workerID := fmt.Sprintf("%s-%d", e.workerID, slot)
		workers.Add(1)
		go func(workerID string) {
			defer workers.Done()
			e.executionWorkerLoop(ctx, workerID)
		}(workerID)
	}
	workers.Wait()
}

func (e *CheckExecutor) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(e.workerPoll)
	defer ticker.Stop()
	e.processExpired(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.processExpired(ctx)
		}
	}
}

func (e *CheckExecutor) executionWorkerLoop(ctx context.Context, workerID string) {
	ticker := time.NewTicker(e.workerPoll)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if e.processPending(ctx, workerID) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *CheckExecutor) processExpired(ctx context.Context) {
	if err := e.checkTaskRepo.RecoverExpiredExecutions(ctx, time.Now().UTC()); err != nil {
		if ctx.Err() == nil {
			log.Printf("quality execution lease recovery failed: %v", err)
		}
	}
}

func (e *CheckExecutor) processPending(ctx context.Context, workerID string) bool {
	execution, task, err := e.checkTaskRepo.ClaimPendingExecution(ctx, workerID, time.Now().UTC(), e.workerLease)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("quality execution claim failed: %v", err)
		}
		return false
	}
	if execution == nil || task == nil {
		return false
	}
	lease, err := commonExecution.LeaseFromExecution(*execution)
	if err != nil {
		log.Printf("quality execution %s has invalid lease identity: %v", execution.ExecutionID, err)
		return true
	}
	checkTimeout, configErr := executionCheckTimeout(execution.ExecutionConfig)
	if configErr != nil {
		completedAt := time.Now().UTC()
		fields := executionTerminalFields(failExecution(qualityExecutionConfigInvalid, configErr), false)
		if execution.StartedAt != nil {
			fields["execution_time_ms"] = completedAt.Sub(*execution.StartedAt).Milliseconds()
		}
		if err := e.checkTaskRepo.CompleteExecutionWithLease(ctx, task.ID, int64(task.TenantID), lease, fields, completedAt); err != nil {
			log.Printf("quality execution %s config failure completion failed: %v", execution.ExecutionID, err)
		}
		return true
	}
	checkCtx, cancelCheck := context.WithTimeout(ctx, checkTimeout)
	heartbeatDone := make(chan error, 1)
	go e.renewExecutionLease(checkCtx, cancelCheck, lease, heartbeatDone)
	result, observations, execErr := e.doCheck(checkCtx, task, execution)
	timedOut := errors.Is(checkCtx.Err(), context.DeadlineExceeded) || errors.Is(execErr, context.DeadlineExceeded)
	execErr = executionErrorForDeadline(execErr, timedOut)
	cancelCheck()
	if heartbeatErr := <-heartbeatDone; heartbeatErr != nil {
		log.Printf("quality execution %s lease renewal failed: %v", execution.ExecutionID, heartbeatErr)
		return true
	}
	completedAt := time.Now().UTC()
	fields := map[string]interface{}{}
	if execution.StartedAt != nil {
		fields["execution_time_ms"] = completedAt.Sub(*execution.StartedAt).Milliseconds()
	}
	if execErr != nil {
		if timedOut {
			log.Printf("quality execution %s timed out: %v", execution.ExecutionID, execErr)
		} else {
			log.Printf("quality execution %s failed: %v", execution.ExecutionID, execErr)
		}
		for key, value := range executionTerminalFields(execErr, timedOut) {
			fields[key] = value
		}
	} else {
		if err := e.checkTaskRepo.RenewLease(ctx, lease, time.Now().UTC().Add(e.workerLease)); err != nil {
			log.Printf("quality execution %s lost lease before issue reconciliation: %v", execution.ExecutionID, err)
			return true
		}
		if err := e.issueRepo.Reconcile(ctx, int64(task.TenantID), execution.ExecutionID, observations, completedAt); err != nil {
			execErr = fmt.Errorf("reconcile quality issues: %w", err)
			log.Printf("quality execution %s issue reconciliation failed: %v", execution.ExecutionID, err)
			fields["status"] = commonExecution.ExecutionStatusFailed
			fields["error_details"] = commonModels.JSONMap{"code": qualityExecutionIssueReconcileFailed, "message": "quality issue reconciliation failed"}
		} else {
			fields["status"] = commonExecution.ExecutionStatusSuccess
			fields["metadata"] = executionResultMetadata(result)
			fields["progress"] = 100
		}
	}
	if err := e.checkTaskRepo.CompleteExecutionWithLease(ctx, task.ID, int64(task.TenantID), lease, fields, completedAt); err != nil {
		log.Printf("quality execution %s completion failed: %v", execution.ExecutionID, err)
	}
	return true
}

func executionTerminalFields(execErr error, timedOut bool) map[string]interface{} {
	if timedOut {
		return map[string]interface{}{
			"status":        commonExecution.ExecutionStatusTimeout,
			"error_details": commonModels.JSONMap{"code": qualityExecutionTimeout, "message": "quality check execution timed out"},
		}
	}
	return map[string]interface{}{
		"status":        commonExecution.ExecutionStatusFailed,
		"error_details": commonModels.JSONMap{"code": executionFailureCode(execErr), "message": "quality check execution failed"},
	}
}

func executionErrorForDeadline(execErr error, timedOut bool) error {
	if timedOut && execErr == nil {
		return failExecution(qualityExecutionTimeout, context.DeadlineExceeded)
	}
	return execErr
}

func (e *CheckExecutor) renewExecutionLease(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(e.workerLease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := e.checkTaskRepo.RenewLease(ctx, lease, now.UTC().Add(e.workerLease)); err != nil {
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

func executionResultMetadata(result *ExecutionResult) commonModels.JSONMap {
	if result == nil {
		return commonModels.JSONMap{}
	}
	return commonModels.JSONMap{
		"schema_version": qualityExecutionResultSchemaVersion,
		"quality_score":  result.QualityScore,
		"total_rules":    result.TotalRules,
		"passed_rules":   result.PassedRules,
		"failed_rules":   result.FailedRules,
		"field_scores":   result.FieldScores,
		"rule_details":   result.RuleDetails,
	}
}

func (e *CheckExecutor) doCheck(ctx context.Context, task *models.CheckTask, execution *commonExecution.TaskExecution) (*ExecutionResult, []models.IssueObservation, error) {
	if execution.ExecutionAuthorizationID == nil {
		return nil, nil, failExecution(qualityExecutionAuthorizationMissing, fmt.Errorf("execution authorization is missing"))
	}
	engineAccess, err := e.systemClient.WithTenantID(uint(task.TenantID)).GetExecutionEngineAccess(ctx, strconv.FormatInt(*execution.ExecutionAuthorizationID, 10), commonClient.ExecutionEngineAccessRequest{
		ExecutionID: execution.ExecutionID, EngineID: strconv.FormatInt(task.EngineID, 10), RequiredEffects: []string{"read"},
	})
	if err != nil {
		return nil, nil, failExecution(qualityExecutionAuthorizationFailed, fmt.Errorf("get authorized engine access: %w", err))
	}
	if engineAccess.Engine == nil || !strings.EqualFold(engineAccess.Engine.EngineType, "postgresql") {
		return nil, nil, failExecution(qualityExecutionEngineUnsupported, fmt.Errorf("quality v1 only supports PostgreSQL engines"))
	}
	targetDB, err := dbbridge.GetOrCreatePool(engineAccess.Engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, nil, failExecution(qualityExecutionTargetConnectionFailed, fmt.Errorf("connect to authorized engine: %w", err))
	}
	ruleApps, err := snapshotRuleApplications(execution.ExecutionConfig)
	if err != nil {
		return nil, nil, failExecution(qualityExecutionRuleSnapshotInvalid, err)
	}
	if len(ruleApps) == 0 {
		return nil, nil, failExecution(qualityExecutionNoRules, fmt.Errorf("quality check has no enabled rule applications"))
	}

	ruleDetails := make([]RuleResult, 0)
	observations := make([]models.IssueObservation, 0)
	for _, application := range ruleApps {
		document, parseErr := dataquality.Parse(application.RuleConfig)
		if parseErr != nil {
			return nil, nil, failExecution(qualityExecutionRuleSnapshotInvalid, fmt.Errorf("rule application %d has invalid snapshot: %w", application.ID, parseErr))
		}
		for _, rule := range document.EnabledRules() {
			compiled, compileErr := e.sqlGen.GenerateCheckSQL(application.SchemaName, application.Table, application.ColumnName, rule)
			if compileErr != nil {
				return nil, nil, failExecution(qualityExecutionRuleCompileFailed, fmt.Errorf("compile rule application %d (%s): %w", application.ID, rule.Type, compileErr))
			}
			var counts CheckCounts
			if queryErr := targetDB.WithContext(ctx).Raw(compiled.SQL, compiled.Args...).Scan(&counts).Error; queryErr != nil {
				return nil, nil, failExecution(qualityExecutionSQLFailed, fmt.Errorf("execute rule application %d (%s): %w", application.ID, rule.Type, queryErr))
			}
			passRate, passed, countErr := evaluateCheckCounts(counts)
			if countErr != nil {
				return nil, nil, failExecution(qualityExecutionResultInvalid, fmt.Errorf("rule application %d (%s) returned invalid counts: %w", application.ID, rule.Type, countErr))
			}
			detail := RuleResult{RuleApplicationID: application.ID, RuleKey: rule.RuleKey, Type: rule.Type, Severity: rule.Severity, Message: rule.Message, Column: application.ColumnName, Table: application.Table, Schema: application.SchemaName, PassRate: passRate, FailedCount: counts.FailedCount, TotalCount: counts.TotalCount, Passed: passed}
			ruleDetails = append(ruleDetails, detail)
			observations = append(observations, models.IssueObservation{RuleApplicationID: detail.RuleApplicationID, RuleKey: detail.RuleKey, RuleType: detail.Type, Severity: detail.Severity, Message: detail.Message, ColumnName: detail.Column, Table: detail.Table, SchemaName: detail.Schema, EngineID: task.EngineID, FailedCount: detail.FailedCount, TotalCount: detail.TotalCount, PassRate: detail.PassRate, Passed: detail.Passed})
		}
	}
	result, err := aggregateExecutionResult(ruleDetails)
	if err != nil {
		return nil, nil, failExecution(qualityExecutionResultInvalid, err)
	}
	return result, observations, nil
}

func evaluateCheckCounts(counts CheckCounts) (float64, bool, error) {
	if counts.TotalCount < 0 || counts.FailedCount < 0 || counts.FailedCount > counts.TotalCount {
		return 0, false, fmt.Errorf("failed_count must be between zero and total_count")
	}
	if counts.TotalCount == 0 {
		return 100, true, nil
	}
	return float64(counts.TotalCount-counts.FailedCount) / float64(counts.TotalCount) * 100, counts.FailedCount == 0, nil
}

func aggregateExecutionResult(ruleDetails []RuleResult) (*ExecutionResult, error) {
	if len(ruleDetails) == 0 {
		return nil, fmt.Errorf("quality check has no enabled rules")
	}
	fieldValues := make(map[string][]float64)
	for _, detail := range ruleDetails {
		fieldValues[detail.Column] = append(fieldValues[detail.Column], detail.PassRate)
	}
	fieldScores := make([]FieldScore, 0, len(fieldValues))
	for column, scores := range fieldValues {
		var total float64
		for _, score := range scores {
			total += score
		}
		fieldScores = append(fieldScores, FieldScore{Column: column, Score: total / float64(len(scores)), RuleCount: len(scores)})
	}
	sort.Slice(fieldScores, func(i, j int) bool { return fieldScores[i].Column < fieldScores[j].Column })
	var totalScore float64
	passedRules := 0
	for _, detail := range ruleDetails {
		totalScore += detail.PassRate
		if detail.Passed {
			passedRules++
		}
	}
	return &ExecutionResult{QualityScore: totalScore / float64(len(ruleDetails)), TotalRules: len(ruleDetails), PassedRules: passedRules, FailedRules: len(ruleDetails) - passedRules, FieldScores: fieldScores, RuleDetails: ruleDetails}, nil
}

func snapshotRuleApplications(config commonModels.JSONMap) ([]models.RuleApplication, error) {
	raw, ok := config["rule_applications"]
	if !ok {
		return nil, fmt.Errorf("execution rule snapshot is missing")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode execution rule snapshot: %w", err)
	}
	var applications []models.RuleApplication
	if err := json.Unmarshal(encoded, &applications); err != nil {
		return nil, fmt.Errorf("decode execution rule snapshot: %w", err)
	}
	return applications, nil
}

const qualityExecutionConfigSchemaVersion = "addp.quality.execution-config/v1"

func executionCheckTimeout(config commonModels.JSONMap) (time.Duration, error) {
	encoded, err := json.Marshal(config)
	if err != nil {
		return 0, fmt.Errorf("encode execution config: %w", err)
	}
	var snapshot struct {
		SchemaVersion  string `json:"schema_version"`
		CheckTimeoutMS int64  `json:"check_timeout_ms"`
	}
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return 0, fmt.Errorf("decode execution config: %w", err)
	}
	if snapshot.SchemaVersion != qualityExecutionConfigSchemaVersion {
		return 0, fmt.Errorf("unsupported execution config version %q", snapshot.SchemaVersion)
	}
	if snapshot.CheckTimeoutMS <= 0 || snapshot.CheckTimeoutMS > int64(time.Duration(1<<63-1)/time.Millisecond) {
		return 0, fmt.Errorf("invalid check_timeout_ms %d", snapshot.CheckTimeoutMS)
	}
	return time.Duration(snapshot.CheckTimeoutMS) * time.Millisecond, nil
}

func intPtr(v int) *int { return &v }
