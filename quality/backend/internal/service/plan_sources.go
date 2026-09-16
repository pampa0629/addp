package service

import (
	"context"
	"encoding/json"
	"fmt"
	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataquality"
	"github.com/addp/quality/internal/models"
	"reflect"
	"strings"
)

// RuleSource is provenance, not an execution-time dependency.
type RuleSource = models.RuleSource

type planValueParams struct {
	Table      string                 `json:"table"`
	Column     string                 `json:"column"`
	Constraint dataquality.Parameters `json:"constraint"`
}

// A pinned source never tracks a later Standard revision implicitly. Editing a
// name, severity, enabled state or target keeps its verified value constraint.
func unchangedStandardConstraint(rule PlanRule, previous PlanRuleDocument) bool {
	for _, old := range previous.Rules {
		if old.RuleKey != rule.RuleKey || old.Type != rule.Type || !reflect.DeepEqual(old.Source, rule.Source) {
			continue
		}
		var before, after map[string]interface{}
		if json.Unmarshal(old.Params, &before) != nil || json.Unmarshal(rule.Params, &after) != nil {
			return false
		}
		delete(before, "table")
		delete(before, "column")
		delete(after, "table")
		delete(after, "column")
		return reflect.DeepEqual(before, after)
	}
	return false
}

func (s *RuleService) validateStandardSource(ctx context.Context, tenantID int64, rule PlanRule) error {
	source := rule.Source
	if source.ElementID <= 0 || source.ElementRevisionID <= 0 || s.standardClient == nil {
		return fmt.Errorf("%w: invalid standard source", commonAPI.ErrBadRequest)
	}
	snapshot, err := s.standardClient.WithTenantID(uint(tenantID)).GetElementQualityRules(ctx, source.ElementID)
	if err != nil {
		return err
	}
	if snapshot.ElementRevisionID != source.ElementRevisionID {
		return fmt.Errorf("%w: standard revision changed", commonAPI.ErrConflict)
	}
	for _, candidate := range snapshot.QualityRules.EnabledRules() {
		if candidate.RuleKey != source.RuleKey {
			continue
		}
		params, err := standardRuleParams(candidate, rule.Params)
		if err != nil {
			return err
		}
		var submitted interface{}
		if err := json.Unmarshal(rule.Params, &submitted); err != nil {
			return err
		}
		expected, _ := json.Marshal(params)
		actual, _ := json.Marshal(submitted)
		if rule.Type != candidate.Type || string(expected) != string(actual) {
			return fmt.Errorf("%w: standard constraint was modified", commonAPI.ErrBadRequest)
		}
		return nil
	}
	return fmt.Errorf("%w: standard rule not found", commonAPI.ErrBadRequest)
}

func standardRuleParams(rule dataquality.Rule, target json.RawMessage) (map[string]interface{}, error) {
	var fields map[string]interface{}
	if err := json.Unmarshal(target, &fields); err != nil {
		return nil, err
	}
	result := map[string]interface{}{"table": fields["table"], "column": fields["column"]}
	switch rule.Type {
	case "not_null":
	case "allowed_values":
		result["values"] = rule.Params.Values
	case "format", "length", "value_range":
		result["constraint"] = rule.Params
	default:
		return nil, fmt.Errorf("%w: unsupported standard rule", commonAPI.ErrBadRequest)
	}
	raw, _ := json.Marshal(result)
	_ = json.Unmarshal(raw, &result)
	return result, nil
}

func (s *RuleService) ListElementCandidates(ctx context.Context, tenantID int64, keyword string, page, pageSize int) ([]PlanElementCandidate, int64, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, 0, fmt.Errorf("%w: keyword is required", commonAPI.ErrBadRequest)
	}
	if s.standardClient == nil {
		return nil, 0, fmt.Errorf("standard client unavailable")
	}
	elements, total, err := s.standardClient.WithTenantID(uint(tenantID)).ListElementCandidates(ctx, keyword, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list quality plan element candidates: %w", err)
	}
	items := make([]PlanElementCandidate, len(elements))
	for index, element := range elements {
		items[index] = PlanElementCandidate{
			ID: element.ID, RevisionID: element.RevisionID, RevisionNo: element.RevisionNo, Name: element.Name, Code: element.Code, QualityRules: element.QualityRules,
		}
	}
	return items, total, nil
}

type PlanElementCandidate struct {
	ID           int64                `json:"id"`
	RevisionID   int64                `json:"revision_id"`
	RevisionNo   int64                `json:"revision_no"`
	Name         string               `json:"name"`
	Code         string               `json:"code"`
	QualityRules dataquality.Document `json:"quality_rules"`
}
