package models

import (
	"encoding/json"
	"fmt"
	"time"
)

type RuleSource struct {
	ElementID         int64  `json:"element_id"`
	ElementRevisionID int64  `json:"element_revision_id"`
	RuleKey           string `json:"rule_key"`
}

// RuleContent is target-independent and immutable once stored in a revision.
type RuleContent struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Params      json.RawMessage `gorm:"type:jsonb" json:"params" swaggertype:"object"`
	Source      *RuleSource     `gorm:"serializer:json;type:jsonb" json:"source,omitempty"`
}

type QualityRule struct {
	ID            int64  `gorm:"primaryKey" json:"id"`
	TenantID      int64  `json:"tenant_id"`
	Code          string `json:"code"`
	OwnerDomainID *int64 `gorm:"index" json:"owner_domain_id,omitempty"`
	Version       int64  `json:"version"`
	RevisionNo    int64  `json:"revision_no"`
	RuleContent   `gorm:"embedded"`
	CreatedBy     int64     `json:"created_by"`
	UpdatedBy     int64     `json:"updated_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	PlanCount     int64     `gorm:"-" json:"plan_count"`
}

func (QualityRule) TableName() string { return "quality.rules" }

type RuleRevision struct {
	TenantID         int64 `gorm:"primaryKey;autoIncrement:false" json:"-"`
	RuleID           int64 `gorm:"primaryKey;autoIncrement:false" json:"rule_id"`
	RevisionNo       int64 `gorm:"primaryKey;autoIncrement:false" json:"revision_no"`
	RuleContent      `gorm:"embedded"`
	CreatedAt        time.Time `json:"created_at"`
	CreatedBy        int64     `json:"created_by"`
	LatestRevisionNo int64     `gorm:"-" json:"latest_revision_no"`
}

func (RuleRevision) TableName() string { return "quality.rule_revisions" }

type CheckBindings struct {
	Table            string   `json:"table"`
	Column           string   `json:"column,omitempty"`
	Columns          []string `json:"columns,omitempty"`
	ReferenceTable   string   `json:"reference_table,omitempty"`
	ReferenceColumns []string `json:"reference_columns,omitempty"`
	WhenColumn       string   `json:"when_column,omitempty"`
	ThenColumn       string   `json:"then_column,omitempty"`
}

type PlanCheckItem struct {
	TenantID   int64         `gorm:"primaryKey;autoIncrement:false" json:"-"`
	PlanID     int64         `gorm:"primaryKey;autoIncrement:false" json:"-"`
	RuleKey    string        `gorm:"primaryKey" json:"rule_key"`
	Position   int           `json:"-"`
	RuleID     int64         `json:"rule_id"`
	RevisionNo int64         `json:"revision_no"`
	Severity   string        `json:"severity"`
	Disabled   bool          `json:"disabled"`
	Bindings   CheckBindings `gorm:"serializer:json;type:jsonb" json:"bindings"`
	Rule       *RuleRevision `gorm:"-" json:"rule"`
}

func (PlanCheckItem) TableName() string { return "quality.plan_check_items" }

// ResolveRuleParams combines a frozen constraint with an explicit target. It
// never reads engines, source standards, or the latest rule revision.
func ResolveRuleParams(kind string, raw json.RawMessage, binding CheckBindings) (json.RawMessage, error) {
	var params map[string]interface{}
	if err := json.Unmarshal(raw, &params); err != nil || params == nil {
		return nil, fmt.Errorf("rule params must be an object")
	}
	target, _ := json.Marshal(binding)
	var fields map[string]interface{}
	_ = json.Unmarshal(target, &fields)
	allowed := map[string]bool{"table": true}
	switch kind {
	case "not_null", "allowed_values", "format", "length", "value_range":
		allowed["column"] = true
	case "unique_key":
		allowed["columns"] = true
	case "foreign_key":
		allowed["columns"] = true
		allowed["reference_table"] = true
		allowed["reference_columns"] = true
	case "predicate_implication":
		allowed["when_column"] = true
		allowed["then_column"] = true
	case "row_count":
	default:
		return nil, fmt.Errorf("unsupported rule type")
	}
	for key, value := range fields {
		if !allowed[key] {
			return nil, fmt.Errorf("binding %s is not allowed for %s", key, kind)
		}
		if key == "when_column" || key == "then_column" {
			continue
		}
		if _, exists := params[key]; exists {
			return nil, fmt.Errorf("target is not a rule constraint")
		}
		params[key] = value
	}
	if kind == "predicate_implication" {
		for key, column := range map[string]string{"when": binding.WhenColumn, "then": binding.ThenColumn} {
			condition, ok := params[key].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("predicate condition is missing")
			}
			if _, exists := condition["column"]; exists {
				return nil, fmt.Errorf("predicate column belongs to binding")
			}
			condition["column"] = column
		}
	}
	return json.Marshal(params)
}

func (p *QualityPlan) ResolveRules() error {
	rules := make([]map[string]interface{}, 0, len(p.CheckItems))
	for _, item := range p.CheckItems {
		if item.Rule == nil {
			return fmt.Errorf("rule revision is missing")
		}
		params, err := ResolveRuleParams(item.Rule.Type, item.Rule.Params, item.Bindings)
		if err != nil {
			return err
		}
		rules = append(rules, map[string]interface{}{"rule_key": item.RuleKey, "rule_id": item.RuleID, "revision_no": item.RevisionNo, "type": item.Rule.Type, "name": item.Rule.Name, "source": item.Rule.Source, "severity": item.Severity, "disabled": item.Disabled, "params": params})
	}
	var err error
	p.Rules, err = json.Marshal(map[string]interface{}{"schema_version": "addp.quality.plan-rules/v1", "rules": rules})
	return err
}
