package service

import (
	"encoding/json"
	"fmt"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/quality/internal/models"
	"strings"
)

type ruleTargetInfo struct {
	Table   string
	Columns []string
}

func ruleTarget(rule PlanRule) ruleTargetInfo {
	var value struct {
		Table   string   `json:"table"`
		Column  string   `json:"column"`
		Columns []string `json:"columns"`
		When    struct {
			Column string `json:"column"`
		} `json:"when"`
		Then struct {
			Column string `json:"column"`
		} `json:"then"`
	}
	_ = json.Unmarshal(rule.Params, &value)
	if value.Column != "" {
		value.Columns = []string{value.Column}
	}
	if rule.Type == "predicate_implication" {
		value.Columns = []string{value.When.Column}
		if value.Then.Column != value.When.Column {
			value.Columns = append(value.Columns, value.Then.Column)
		}
	}
	return ruleTargetInfo{Table: value.Table, Columns: value.Columns}
}

func planIssueObservations(planID int64, config commonModels.JSONMap, result *PlanResult) ([]models.IssueObservation, error) {
	snapshot, err := decodePlanExecutionConfig(config)
	if err != nil {
		return nil, err
	}
	bindings := map[string]PlanTableBinding{}
	for _, b := range snapshot.TableBindings {
		bindings[b.Alias] = b
	}
	observations := make([]models.IssueObservation, 0, len(result.Rules))
	for _, rule := range result.Rules {
		locator, err := resourcetree.ParseURI(bindings[rule.Table].Locator)
		if err != nil || len(locator.Path) != 2 {
			return nil, fmt.Errorf("invalid issue target for rule %s", rule.RuleKey)
		}
		rate := 100.0
		if rule.Type == "row_count" {
			if !rule.Passed {
				rate = 0
			}
		} else if rule.TotalCount > 0 {
			rate = 100 * float64(rule.TotalCount-rule.FailedCount) / float64(rule.TotalCount)
		}
		observations = append(observations, models.IssueObservation{PlanID: planID, RuleKey: rule.RuleKey, RuleType: rule.Type, Severity: rule.Severity, Message: rule.Name, ColumnName: strings.Join(rule.Columns, ", "), Table: locator.Path[1], SchemaName: locator.Path[0], EngineID: int64(locator.EngineID), FailedCount: rule.FailedCount, TotalCount: rule.TotalCount, PassRate: rate, Passed: rule.Passed})
	}
	return observations, nil
}
