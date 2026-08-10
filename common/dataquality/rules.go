package dataquality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const RulesSchemaVersion = "addp.quality.rules/v1"

const (
	RuleTypeNotNull       = "not_null"
	RuleTypeUnique        = "unique"
	RuleTypeFormat        = "format"
	RuleTypeLength        = "length"
	RuleTypeValueRange    = "value_range"
	RuleTypeAllowedValues = "allowed_values"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

type Document struct {
	SchemaVersion string `json:"schema_version"`
	Rules         []Rule `json:"rules"`
}

type Rule struct {
	Type     string     `json:"type"`
	Enabled  bool       `json:"enabled"`
	Severity string     `json:"severity"`
	Message  string     `json:"message"`
	Params   Parameters `json:"params"`
}

type Parameters struct {
	Pattern *string      `json:"pattern,omitempty"`
	Min     *json.Number `json:"min,omitempty"`
	Max     *json.Number `json:"max,omitempty"`
	Values  []string     `json:"values,omitempty"`
}

func EmptyDocument() Document {
	return Document{SchemaVersion: RulesSchemaVersion, Rules: []Rule{}}
}

func Parse(raw []byte) (Document, error) {
	var document Document
	if err := decodeStrict(raw, &document); err != nil {
		return Document{}, fmt.Errorf("invalid quality rules document: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func FromValue(value any) (Document, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return Document{}, fmt.Errorf("encode quality rules document: %w", err)
	}
	return Parse(raw)
}

func ToMap(document Document) (map[string]interface{}, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode quality rules document: %w", err)
	}
	var result map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("convert quality rules document: %w", err)
	}
	return result, nil
}

func (d Document) EnabledRules() []Rule {
	rules := make([]Rule, 0, len(d.Rules))
	for _, rule := range d.Rules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	return rules
}

func (d Document) Validate() error {
	if d.SchemaVersion != RulesSchemaVersion {
		return fmt.Errorf("quality_rules.schema_version must be %q", RulesSchemaVersion)
	}
	if d.Rules == nil {
		return fmt.Errorf("quality_rules.rules must be an array")
	}
	for index, rule := range d.Rules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("quality_rules.rules[%d]: %w", index, err)
		}
	}
	return nil
}

func (r Rule) Validate() error {
	switch r.Severity {
	case SeverityError, SeverityWarning, SeverityInfo:
	default:
		return fmt.Errorf("severity must be one of error, warning, info")
	}

	switch r.Type {
	case RuleTypeNotNull, RuleTypeUnique:
		if !r.Params.empty() {
			return fmt.Errorf("%s params must be empty", r.Type)
		}
	case RuleTypeFormat:
		if r.Params.Pattern == nil || strings.TrimSpace(*r.Params.Pattern) == "" {
			return fmt.Errorf("format params.pattern must be a non-empty string")
		}
		if r.Params.Min != nil || r.Params.Max != nil || r.Params.Values != nil {
			return fmt.Errorf("format params only allows pattern")
		}
	case RuleTypeLength:
		if r.Params.Pattern != nil || r.Params.Values != nil {
			return fmt.Errorf("length params only allows min and max")
		}
		if r.Params.Min == nil && r.Params.Max == nil {
			return fmt.Errorf("length params requires min or max")
		}
		min, hasMin, err := nonNegativeInteger(r.Params.Min, "length params.min")
		if err != nil {
			return err
		}
		max, hasMax, err := nonNegativeInteger(r.Params.Max, "length params.max")
		if err != nil {
			return err
		}
		if hasMin && hasMax && min > max {
			return fmt.Errorf("length params.min must be less than or equal to max")
		}
	case RuleTypeValueRange:
		if r.Params.Pattern != nil || r.Params.Values != nil {
			return fmt.Errorf("value_range params only allows min and max")
		}
		if r.Params.Min == nil && r.Params.Max == nil {
			return fmt.Errorf("value_range params requires min or max")
		}
		min, hasMin, err := finiteNumber(r.Params.Min, "value_range params.min")
		if err != nil {
			return err
		}
		max, hasMax, err := finiteNumber(r.Params.Max, "value_range params.max")
		if err != nil {
			return err
		}
		if hasMin && hasMax && min > max {
			return fmt.Errorf("value_range params.min must be less than or equal to max")
		}
	case RuleTypeAllowedValues:
		if r.Params.Pattern != nil || r.Params.Min != nil || r.Params.Max != nil {
			return fmt.Errorf("allowed_values params only allows values")
		}
		if len(r.Params.Values) == 0 {
			return fmt.Errorf("allowed_values params.values must not be empty")
		}
		seen := make(map[string]struct{}, len(r.Params.Values))
		for index, value := range r.Params.Values {
			if value == "" {
				return fmt.Errorf("allowed_values params.values[%d] must not be empty", index)
			}
			if _, exists := seen[value]; exists {
				return fmt.Errorf("allowed_values params.values contains duplicate %q", value)
			}
			seen[value] = struct{}{}
		}
	default:
		return fmt.Errorf("unsupported rule type %q", r.Type)
	}
	return nil
}

func (d *Document) UnmarshalJSON(raw []byte) error {
	type documentJSON struct {
		SchemaVersion *string            `json:"schema_version"`
		Rules         *[]json.RawMessage `json:"rules"`
	}
	var value documentJSON
	if err := decodeStrict(raw, &value); err != nil {
		return err
	}
	if value.SchemaVersion == nil {
		return fmt.Errorf("schema_version is required")
	}
	if value.Rules == nil {
		return fmt.Errorf("rules is required")
	}
	rules := make([]Rule, len(*value.Rules))
	for index, ruleRaw := range *value.Rules {
		if err := decodeStrict(ruleRaw, &rules[index]); err != nil {
			return fmt.Errorf("rules[%d]: %w", index, err)
		}
	}
	d.SchemaVersion = *value.SchemaVersion
	d.Rules = rules
	return nil
}

func (r *Rule) UnmarshalJSON(raw []byte) error {
	type ruleJSON struct {
		Type     *string          `json:"type"`
		Enabled  *bool            `json:"enabled"`
		Severity *string          `json:"severity"`
		Message  *string          `json:"message"`
		Params   *json.RawMessage `json:"params"`
	}
	var value ruleJSON
	if err := decodeStrict(raw, &value); err != nil {
		return err
	}
	if value.Type == nil || value.Enabled == nil || value.Severity == nil || value.Message == nil || value.Params == nil {
		return fmt.Errorf("type, enabled, severity, message and params are required")
	}
	var params Parameters
	if err := decodeStrict(*value.Params, &params); err != nil {
		return fmt.Errorf("params: %w", err)
	}
	r.Type = *value.Type
	r.Enabled = *value.Enabled
	r.Severity = *value.Severity
	r.Message = *value.Message
	r.Params = params
	return nil
}

func (p Parameters) empty() bool {
	return p.Pattern == nil && p.Min == nil && p.Max == nil && p.Values == nil
}

func nonNegativeInteger(value *json.Number, field string) (int64, bool, error) {
	if value == nil {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(value.String(), 10, 64)
	if err != nil || parsed < 0 {
		return 0, false, fmt.Errorf("%s must be a non-negative integer", field)
	}
	return parsed, true, nil
}

func finiteNumber(value *json.Number, field string) (float64, bool, error) {
	if value == nil {
		return 0, false, nil
	}
	parsed, err := strconv.ParseFloat(value.String(), 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be a JSON number", field)
	}
	return parsed, true, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
