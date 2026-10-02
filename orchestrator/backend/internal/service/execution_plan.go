package service

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/addp/common/models"
	orchmodels "github.com/addp/orchestrator/internal/models"
)

const executionPlanVersion = "orchestrator.execution/v1"

type executionPlan struct {
	Version string           `json:"schema_version"`
	Steps   orchmodels.Steps `json:"steps"`
}

// Task identity uses decimal text in the private snapshot because JSONMap and
// PostgreSQL JSON decoding otherwise round integers above 2^53 through float64.
type frozenStepFields orchmodels.Step
type frozenExecutionStep struct {
	frozenStepFields
	TaskID uint `json:"task_id,string"`
}
type frozenExecutionPlan struct {
	Version string                `json:"schema_version"`
	Steps   []frozenExecutionStep `json:"steps"`
}

func freezeExecutionPlan(steps orchmodels.Steps) (models.JSONMap, error) {
	if err := orchmodels.ValidateSteps(steps); err != nil {
		return nil, err
	}
	frozen := frozenExecutionPlan{Version: executionPlanVersion, Steps: make([]frozenExecutionStep, len(steps))}
	for i, step := range steps {
		frozen.Steps[i] = frozenExecutionStep{frozenStepFields: frozenStepFields(step), TaskID: step.TaskID}
	}
	data, err := json.Marshal(frozen)
	if err != nil {
		return nil, err
	}
	var result models.JSONMap
	err = json.Unmarshal(data, &result)
	return result, err
}

func readExecutionPlan(value models.JSONMap) (*executionPlan, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var frozen frozenExecutionPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&frozen); err != nil {
		return nil, err
	}
	plan := executionPlan{Version: frozen.Version, Steps: make(orchmodels.Steps, len(frozen.Steps))}
	for i, step := range frozen.Steps {
		plan.Steps[i] = orchmodels.Step(step.frozenStepFields)
		plan.Steps[i].TaskID = step.TaskID
	}
	if plan.Version != executionPlanVersion {
		return nil, fmt.Errorf("execution plan version is invalid")
	}
	if err = orchmodels.ValidateSteps(plan.Steps); err != nil {
		return nil, err
	}
	return &plan, nil
}

func readStepResults(metadata models.JSONMap) (orchmodels.StepResults, error) {
	results := orchmodels.StepResults{}
	if metadata["step_results"] == nil {
		return results, nil
	}
	data, err := json.Marshal(metadata["step_results"])
	if err == nil {
		err = json.Unmarshal(data, &results)
	}
	return results, err
}

func planProgress(steps orchmodels.Steps, results orchmodels.StepResults) int {
	successful := 0
	for _, step := range steps {
		if results[step.ID].Status == "success" {
			successful++
		}
	}
	if len(steps) == 0 {
		return 0
	}
	return successful * 100 / len(steps)
}

func freezeStepResults(results orchmodels.StepResults) (map[string]interface{}, error) {
	data, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	return result, err
}
