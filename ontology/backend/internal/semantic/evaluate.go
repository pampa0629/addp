package semantic

import (
	"context"
	"errors"
	"slices"
	"unicode/utf8"

	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/interpreter"
)

type FactState string

const (
	Known   FactState = "known"
	Absent  FactState = "absent"
	Unknown FactState = "unknown"
	Invalid FactState = "invalid"
)

type Fact struct {
	State FactState
	// Only string or bool are accepted; all non-known states require nil.
	Value any
}

type Outcome string

const (
	Matched      Outcome = "matched"
	NotMatched   Outcome = "not_matched"
	Undetermined Outcome = "unknown"
	Failed       Outcome = "error"
)

type Evidence struct {
	Variable   string    `json:"variable"`
	PropertyID string    `json:"property_id"`
	State      FactState `json:"state"`
	Treatment  string    `json:"treatment"`
}

type Decision struct {
	Outcome    Outcome    `json:"outcome"`
	Code       string     `json:"code"`
	Mode       string     `json:"mode"`
	Scope      Scope      `json:"scope"`
	Digest     string     `json:"digest,omitempty"`
	RuleID     string     `json:"rule_id,omitempty"`
	Expression string     `json:"expression,omitempty"`
	Basis      string     `json:"basis,omitempty"`
	Inputs     []Evidence `json:"inputs,omitempty"`
	Unresolved []string   `json:"unresolved,omitempty"`
}

// Evaluate performs hypothetical evaluation only. Identity matching is a
// consistency guard, not IAM authorization. Facts are keyed by rule variable.
// Every declared input is validated before evaluation, even if a boolean
// short-circuit could hide malformed or contradictory evidence.
func (s *Snapshot) Evaluate(ctx context.Context, scope Scope, ruleID string, facts map[string]Fact) Decision {
	out := Decision{Outcome: Failed, Mode: "hypothetical"}
	if scope != s.definition.Scope {
		out.Code = "scope_mismatch"
		return out
	}
	if ctx.Err() != nil {
		out.Code = "canceled"
		return out
	}
	c, ok := s.rules[ruleID]
	if !ok {
		out.Code = "unknown_rule"
		return out
	}
	out.Scope, out.Digest, out.RuleID = scope, s.digest, ruleID
	out.Expression, out.Basis = c.definition.Expression, c.definition.Basis
	if len(facts) > maxInputs {
		out.Code = "input_limit"
		return out
	}
	expected := map[string]bool{}
	for _, input := range c.definition.Inputs {
		expected[input.Variable] = true
	}
	for name := range facts {
		if !expected[name] {
			out.Code = "unexpected_input"
			return out
		}
	}
	values := map[string]any{}
	for _, input := range c.definition.Inputs {
		f, exists := facts[input.Variable]
		if !exists {
			f = Fact{State: Unknown}
		}
		evidence := Evidence{Variable: input.Variable, PropertyID: input.PropertyID, State: f.State}
		p := c.properties[input.PropertyID]
		if f.State != Known && f.Value != nil {
			out.Code = "contradictory_fact"
			return out
		}
		switch f.State {
		case Known:
			valid := false
			if p.Kind == Boolean {
				_, valid = f.Value.(bool)
			} else {
				v, ok := f.Value.(string)
				valid = ok && len(v) <= maxText && utf8.ValidString(v) && (len(p.Enum) == 0 || slices.Contains(p.Enum, v))
			}
			if !valid {
				out.Code = "invalid_fact_value"
				return out
			}
			values[input.Variable] = f.Value
			evidence.Treatment = "value"
		case Absent:
			if input.OnAbsent == AbsenceFalse {
				values[input.Variable] = false
				evidence.Treatment = "explicit_absence_false"
			} else {
				evidence.Treatment = "unresolved"
			}
		case Unknown:
			evidence.Treatment = "unresolved"
		default:
			out.Code = "invalid_fact_state"
			return out
		}
		out.Inputs = append(out.Inputs, evidence)
		if evidence.Treatment == "unresolved" {
			out.Unresolved = append(out.Unresolved, input.Variable)
		}
	}
	activation, err := c.env.PartialVars(values)
	if err != nil {
		out.Code = "activation_error"
		return out
	}
	value, _, err := c.program.ContextEval(ctx, activation)
	if ctx.Err() != nil {
		out.Code = "canceled"
		return out
	}
	if err != nil {
		out.Code = "evaluation_error"
		var canceled interpreter.EvalCancelledError
		if errors.As(err, &canceled) && canceled.Cause == interpreter.CostLimitExceeded {
			out.Code = "cost_limit"
		}
		return out
	}
	if types.IsUnknown(value) {
		out.Outcome, out.Code = Undetermined, "insufficient_evidence"
		return out
	}
	b, ok := value.Value().(bool)
	if !ok {
		out.Code = "non_boolean_result"
		return out
	}
	if b {
		out.Outcome, out.Code = Matched, "predicate_true"
	} else {
		out.Outcome, out.Code = NotMatched, "predicate_false"
	}
	return out
}
