package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/quality/internal/models"
)

const (
	planConfigInvalidCode       = "quality.plan.config_invalid"
	planReadContextFailedCode   = "quality.plan.read_context_failed"
	planUnsupportedEngineCode   = "quality.plan.unsupported_engine"
	planAuthorizationFailedCode = "quality.plan.authorization_failed"
	planCompileFailedCode       = "quality.plan.rule_compile_failed"
	planSQLFailedCode           = "quality.plan.sql_execution_failed"
	planRuleFailedCode          = "quality.plan.rule_failed"
	planResultInvalidCode       = "quality.plan.result_invalid"
)

type planExecutionConfig struct {
	TargetKey         string             `json:"target_key"`
	SchemaVersion     string             `json:"schema_version"`
	TaskVersion       int64              `json:"task_version"`
	OwnerDomainID     *int64             `json:"owner_domain_id,omitempty"`
	TableBindings     []PlanTableBinding `json:"table_bindings"`
	Rules             PlanRuleDocument   `json:"rules"`
	ParentExecutionID string             `json:"parent_execution_id"`
	CheckTimeoutMS    int64              `json:"check_timeout_ms"`
}

type PlanRuleResult struct {
	Evidence    *models.FailureEvidence `json:"-"`
	RuleKey     string                  `json:"rule_key"`
	RuleID      int64                   `json:"rule_id,omitempty"`
	RevisionNo  int64                   `json:"revision_no,omitempty"`
	Type        string                  `json:"type"`
	Severity    string                  `json:"severity"`
	Passed      bool                    `json:"passed"`
	Name        string                  `json:"name,omitempty"`
	TotalCount  int64                   `json:"total_count"`
	Table       string                  `json:"table"`
	Columns     []string                `json:"columns"`
	FailedCount int64                   `json:"failed_count"`
	Observed    map[string]interface{}  `json:"observed"`
}

type PlanResult struct {
	Rules  []PlanRuleResult `json:"rules"`
	Passed bool             `json:"passed"`
}

func (e *CheckExecutor) processPendingPlan(ctx context.Context, workerID string) bool {
	execution, task, err := e.planRepo.ClaimPendingExecution(ctx, workerID, time.Now().UTC(), e.workerLease)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("quality plan claim failed: %v", err)
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
		log.Printf("quality plan %s has invalid lease: %v", execution.ExecutionID, err)
		return true
	}
	config, err := decodePlanExecutionConfig(execution.ExecutionConfig)
	if err != nil {
		e.completePlan(ctx, task, execution, lease, nil, failExecution(planConfigInvalidCode, err), false)
		return true
	}
	gateCtx, cancel := context.WithTimeout(ctx, time.Duration(config.CheckTimeoutMS)*time.Millisecond)
	heartbeatDone := make(chan error, 1)
	go e.renewPlanLease(gateCtx, cancel, lease, heartbeatDone)
	result, execErr := e.doPlan(gateCtx, task, execution, lease, config)
	timedOut := errors.Is(gateCtx.Err(), context.DeadlineExceeded) || errors.Is(execErr, context.DeadlineExceeded)
	execErr = executionErrorForDeadline(execErr, timedOut)
	cancel()
	if heartbeatErr := <-heartbeatDone; heartbeatErr != nil {
		log.Printf("quality plan %s lease renewal failed: %v", execution.ExecutionID, heartbeatErr)
		return true
	}
	e.completePlan(ctx, task, execution, lease, result, execErr, timedOut)
	return true
}

func decodePlanExecutionConfig(config commonModels.JSONMap) (*planExecutionConfig, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var snapshot planExecutionConfig
	if err := decodeStrictJSON(raw, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.SchemaVersion != planExecutionConfigVersion || snapshot.TaskVersion <= 0 || (snapshot.CheckTimeoutMS <= 0 || snapshot.CheckTimeoutMS > int64(time.Duration(1<<63-1)/time.Millisecond)) {
		return nil, fmt.Errorf("quality plan execution config is invalid")
	}
	key, err := models.PlanTargetKey(snapshot.TableBindings)
	if err != nil || snapshot.TargetKey != key {
		return nil, fmt.Errorf("quality plan target scope is invalid")
	}
	rulesRaw, _ := json.Marshal(snapshot.Rules)
	if _, err := validatePlanContract(snapshot.TableBindings, rulesRaw); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (e *CheckExecutor) doPlan(ctx context.Context, task *models.QualityPlan, execution *commonExecution.TaskExecution, lease commonExecution.Lease, config *planExecutionConfig) (*PlanResult, error) {
	if (execution.Source == commonExecution.ModuleOrchestrator && (execution.ParentExecutionID == nil || *execution.ParentExecutionID != config.ParentExecutionID)) || (execution.Source == commonExecution.ModuleQuality && (execution.ParentExecutionID != nil || config.ParentExecutionID != "")) || (execution.Source != commonExecution.ModuleQuality && execution.Source != commonExecution.ModuleOrchestrator) {
		return nil, failExecution(planConfigInvalidCode, fmt.Errorf("quality plan task or parent changed"))
	}
	first, _ := resourcetree.ParseURI(config.TableBindings[0].Locator)
	engineID := int64(first.EngineID)

	authorizationID := ""
	if execution.ExecutionAuthorizationID == nil {
		if execution.Source != commonExecution.ModuleOrchestrator {
			return nil, failExecution(planAuthorizationFailedCode, fmt.Errorf("user authorization is missing"))
		}
		issued, issueErr := e.systemClient.WithTenantID(uint(task.TenantID)).IssueExecutionAuthorizationFromExecution(ctx, commonClient.IssueExecutionAuthorizationFromExecutionRequest{
			ParentExecutionID: config.ParentExecutionID, Audience: commonExecution.AudienceQuality,
			ExecutionID: execution.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token,
			Accesses: []commonClient.ExecutionEngineAccessScope{{EngineID: strconv.FormatInt(engineID, 10), Effects: []string{"read"}}}, ExpiresIn: 3600,
		})
		if issueErr != nil {
			return nil, failExecution(planAuthorizationFailedCode, issueErr)
		}
		authorizationFields, fieldErr := commonClient.TaskExecutionAuthorizationFields(issued)
		if fieldErr != nil {
			return nil, failExecution(planAuthorizationFailedCode, fieldErr)
		}
		if err := e.planRepo.AttachExecutionAuthorization(ctx, lease, authorizationFields); err != nil {
			return nil, failExecution(planAuthorizationFailedCode, err)
		}
		authorizationID = issued.ID
	} else {
		authorizationID = strconv.FormatInt(*execution.ExecutionAuthorizationID, 10)
	}
	engineAccess, err := e.systemClient.WithTenantID(uint(task.TenantID)).GetExecutionEngineAccess(ctx, authorizationID, commonClient.ExecutionEngineAccessRequest{
		ExecutionID: execution.ExecutionID, EngineID: strconv.FormatInt(engineID, 10), RequiredEffects: []string{"read"},
	})
	if err != nil {
		return nil, failExecution(planAuthorizationFailedCode, err)
	}
	return executePostgreSQLPlan(ctx, engineAccess.Engine, config)
}

func (e *CheckExecutor) renewPlanLease(ctx context.Context, cancel context.CancelFunc, lease commonExecution.Lease, done chan<- error) {
	ticker := time.NewTicker(e.workerLease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case now := <-ticker.C:
			if err := e.planRepo.RenewLease(ctx, lease, now.UTC().Add(e.workerLease)); err != nil {
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

func (e *CheckExecutor) completePlan(ctx context.Context, task *models.QualityPlan, execution *commonExecution.TaskExecution, lease commonExecution.Lease, result *PlanResult, execErr error, timedOut bool) {
	completedAt := time.Now().UTC()
	status := commonExecution.ExecutionStatusSuccess
	fields := map[string]interface{}{"progress": 100}
	if execution.StartedAt != nil {
		fields["execution_time_ms"] = completedAt.Sub(*execution.StartedAt).Milliseconds()
	}
	if result != nil {
		passedRules := 0
		for _, rule := range result.Rules {
			if rule.Passed {
				passedRules++
			}
		}
		fields["metadata"] = commonModels.JSONMap{
			"schema_version": planResultVersion,
			"rules":          result.Rules, "passed": result.Passed,
			"outputs": commonModels.JSONMap{"passed": result.Passed},
		}
		if len(result.Rules) > 0 {
			fields["metadata"].(commonModels.JSONMap)["quality_score"] = 100 * float64(passedRules) / float64(len(result.Rules))
		}
	}
	if timedOut {
		status = commonExecution.ExecutionStatusTimeout
		fields["error_details"] = commonModels.JSONMap{"code": qualityExecutionTimeout, "message": "quality plan timed out"}
	} else if execErr != nil {
		status = commonExecution.ExecutionStatusFailed
		fields["error_details"] = commonModels.JSONMap{"code": executionFailureCode(execErr), "message": "quality plan failed"}
	}
	var observations []models.IssueObservation
	if result != nil && !timedOut && (execErr == nil || executionFailureCode(execErr) == planRuleFailedCode) {
		var issueErr error
		observations, issueErr = planIssueObservations(task.ID, execution.ExecutionConfig, result)
		if issueErr != nil {
			status = commonExecution.ExecutionStatusFailed
			fields["error_details"] = commonModels.JSONMap{"code": planResultInvalidCode, "message": "quality issue projection failed"}
		}
	}
	if err := e.planRepo.CompleteExecutionWithLease(ctx, task.ID, task.TenantID, lease, status, fields, completedAt, observations...); err != nil {
		log.Printf("quality plan %s completion failed: %v", execution.ExecutionID, err)
	}
}
