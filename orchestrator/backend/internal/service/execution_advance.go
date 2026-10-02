package service

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"net/http"
	"time"

	execution "github.com/addp/common/execution"
	"github.com/addp/orchestrator/internal/models"
)

// Advance performs one durable step transition or one bounded child status query.
// Waiting returns to the supervisor; it does not hold a workflow execution slot.
func (e *Executor) Advance(ctx context.Context, lease execution.Lease) error {
	item, err := e.executionService.OwnedExecution(ctx, lease)
	if err != nil {
		return err
	}
	plan, err := readExecutionPlan(item.ExecutionConfig)
	if err != nil {
		return e.executionService.FinishExecution(ctx, lease, "failed", "orchestrator.execution.plan_invalid")
	}
	results, err := readStepResults(item.Metadata)
	if err != nil {
		return e.executionService.FinishExecution(ctx, lease, "failed", "orchestrator.execution.step_state_invalid")
	}
	order, err := topologicalSort(buildDAG(plan.Steps))
	if err != nil {
		return err
	}
	ctx = execution.ContextWithLease(ctx, lease)
	for _, id := range order {
		step := findStep(plan.Steps, id)
		result, recorded := results[id]
		if recorded && result.Status == "success" {
			continue
		}
		if recorded && result.Status != "running" {
			return e.executionService.FinishExecution(ctx, lease, "failed", result.ErrorCode)
		}
		if recorded {
			if result.Phase != "waiting" || extractProviderExecutionID(result.Result) == "" {
				return e.failStep(ctx, lease, id, results, result, "orchestrator.execution.dispatch_uncertain")
			}
			timeout := time.Duration(step.Timeout) * time.Second
			if timeout <= 0 {
				timeout = 5 * time.Minute
			}
			deadline := result.StartedAt.Add(timeout)
			if !time.Now().Before(deadline) {
				return e.failStep(ctx, lease, id, results, result, "orchestrator.execution.step_timeout")
			}
			observationContext, cancel := context.WithDeadline(ctx, deadline)
			data, err := e.observeChild(observationContext, step, extractProviderExecutionID(result.Result), item.TenantID)
			cancel()
			if err != nil {
				if ctx.Err() == nil && !time.Now().Before(deadline) {
					return e.failStep(ctx, lease, id, results, result, "orchestrator.execution.step_timeout")
				}
				// Discovery/transport outages retry observations only, never the execution POST.
				return nil
			}
			switch status, _ := data["status"].(string); status {
			case "pending", "running":
				return nil
			case "success":
				result.Status = "success"
				result.Phase = "terminal"
				result.Result = data
				result.EndedAt = time.Now().UTC()
				result.Duration = result.EndedAt.Sub(result.StartedAt).Milliseconds()
				results[id] = result
				return e.executionService.UpdateStepResults(ctx, lease, results, id)
			case "failed", "timeout", "cancelled":
				return e.failStep(ctx, lease, id, results, result, "orchestrator.execution.child_failed")
			default:
				return nil
			}
		}
		// Persist intent before any network call. A subsequent advance cannot resubmit it.
		for _, dependency := range step.DependsOn {
			if results[dependency].Status != "success" {
				return fmt.Errorf("orchestration dependency is not successful")
			}
		}
		result = models.StepResult{Status: "running", Phase: "dispatching", StartedAt: time.Now().UTC()}
		results[id] = result
		if err := e.executionService.UpdateStepResults(ctx, lease, results, id); err != nil {
			return err
		}
		params, err := e.resolveTemplateReferences(step.Parameters, results)
		if err != nil {
			return e.failStep(ctx, lease, id, results, result, "orchestrator.execution.binding_invalid")
		}
		timeout := time.Duration(step.Timeout) * time.Second
		if timeout <= 0 {
			timeout = 5 * time.Minute
		}
		submissionContext, cancel := context.WithDeadline(ctx, result.StartedAt.Add(timeout))
		submitted, err := e.submitTaskProviderStep(submissionContext, step, params, result.StartedAt, item.ExecutionID, item.TriggerType, item.TenantID)
		cancel()
		if err != nil {
			code := submitted.ErrorCode
			if code == "" {
				code = "orchestrator.execution.dispatch_uncertain"
			}
			return e.failStep(ctx, lease, id, results, submitted, code)
		}
		results[id] = submitted
		return e.executionService.UpdateStepResults(ctx, lease, results, id)
	}
	return e.executionService.FinishExecution(ctx, lease, "success", "")
}

func (e *Executor) failStep(ctx context.Context, lease execution.Lease, id string, results models.StepResults, result models.StepResult, code string) error {
	result.Status = "failed"
	result.Phase = "terminal"
	result.ErrorCode = code
	result.Error = executionFailureMessage(code)
	result.EndedAt = time.Now().UTC()
	result.Duration = result.EndedAt.Sub(result.StartedAt).Milliseconds()
	results[id] = result
	// State and terminal event are one transaction; partial failures cannot advance the DAG.
	return e.executionService.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bound := *e.executionService
		bound.db = tx
		if err := bound.UpdateStepResults(ctx, lease, results, id); err != nil {
			return err
		}
		return bound.FinishExecution(ctx, lease, "failed", code)
	})
}

func (e *Executor) observeChild(ctx context.Context, step *models.Step, id string, tenant int) (map[string]interface{}, error) {
	provider, err := e.taskProviderResolver.GetProvider(ctx, step.Provider)
	if err != nil {
		return nil, err
	}
	endpoint := replaceTaskProviderEndpoint(provider.TaskStatusEndpoint, "", "", id)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.ResolvedBaseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	token, err := e.serviceToken(ctx, tenant)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("child status unavailable")
	}
	var data map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return nil, err
	}
	if extractProviderExecutionID(data) != id {
		return nil, fmt.Errorf("child execution identity mismatch")
	}
	return data, nil
}
