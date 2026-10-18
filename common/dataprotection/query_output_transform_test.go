package dataprotection

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func transformRule() Rule {
	return Rule{Action: "query", Component: Component{Key: "phone", ValueType: "string", Path: []PathSegment{{Name: "phone", Container: "scalar"}}}, Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": float64(3), "suffix_runes": float64(4), "mask_rune": "●"}, InvalidValueEffect: EffectSuppress}}
}

func TestQueryOutputTransformsUseSameAliasAndExactAuthorizationBoundary(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	user := SubjectReference{Type: "user", ID: "33"}
	rule := transformRule()
	rule.Authorizations = []TemporaryAuthorization{{Subject: user, Effect: EffectAllow, ValidFrom: now, ValidUntil: now.Add(time.Hour)}}
	source := plugin.QueryOutputSource{Bindings: []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{"contact"}, Transformation: plugin.QueryOutputTransformationDirect}}}
	for _, test := range []struct {
		name       string
		subject    SubjectReference
		at         time.Time
		authorized bool
	}{
		{"default", SubjectReference{}, now, false},
		{"exact start", user, now, true},
		{"before start", user, now.Add(-time.Nanosecond), false},
		{"exact expiry", user, now.Add(time.Hour), false},
		{"other user", SubjectReference{Type: "user", ID: "34"}, now, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations, err := QueryOutputTransformsAt(source, "query", []Rule{rule}, test.subject, test.at)
			if err != nil {
				t.Fatal(err)
			}
			if test.authorized {
				if len(operations) != 0 {
					t.Fatal("authorized value was masked")
				}
				return
			}
			want := []plugin.QueryOutputTransform{{Column: "contact", Kind: "keep_prefix_suffix", Prefix: 3, Suffix: 4, MaskRune: "●", InvalidValue: "omit"}}
			if !reflect.DeepEqual(operations, want) {
				t.Fatal("alias or decision parameters changed", operations)
			}
		})
	}
	if operations, err := QueryOutputTransformsAt(source, "preview", []Rule{rule}, SubjectReference{}, now); err != nil || len(operations) != 0 {
		t.Fatal("different outlet action affected query")
	}
}

func TestQueryOutputTransformsRejectUnprovenAndPreserveInvalidEffects(t *testing.T) {
	source := plugin.QueryOutputSource{IdentityOutput: true}
	now := time.Now().UTC()
	rule := transformRule()
	rule.Decision.InvalidValueEffect = ""
	operations, err := QueryOutputTransformsAt(source, "query", []Rule{rule}, SubjectReference{}, now)
	if err != nil || len(operations) != 1 || operations[0].InvalidValue != "reject" {
		t.Fatal("implicit deny fallback was weakened", err)
	}
	for _, mode := range []string{"opaque", "derived", "nested", "duplicate", "sm3", "numeric constant", "invalid parameters", "deny"} {
		t.Run(mode, func(t *testing.T) {
			source := plugin.QueryOutputSource{IdentityOutput: true}
			rule := transformRule()
			rules := []Rule{rule}
			switch mode {
			case "opaque":
				source.IdentityOutput = false
				source.OpaqueOutput = true
			case "derived":
				source.IdentityOutput = false
				source.Bindings = []plugin.QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{"computed"}, Transformation: plugin.QueryOutputTransformationDerived}}
			case "nested":
				rules[0].Component.Path = []PathSegment{{Name: "user", Container: "object"}, {Name: "phone", Container: "scalar"}}
			case "duplicate":
				rules = append(rules, rule)
			case "sm3":
				rules[0].Decision.Algorithm = AlgorithmSM3V1
				rules[0].Decision.Parameters = nil
			case "numeric constant":
				rules[0].Component.ValueType = "int"
				rules[0].Decision.Algorithm = AlgorithmConstantV1
				rules[0].Decision.Parameters = map[string]any{"value": 1}
			case "invalid parameters":
				rules[0].Decision.Parameters = map[string]any{"prefix_runes": "3"}
			case "deny":
				rules[0].Decision = Decision{Effect: EffectDeny}
			}
			if operations, err := QueryOutputTransformsAt(source, "query", rules, SubjectReference{}, now); operations != nil || !errors.Is(err, ErrDenied) {
				t.Fatal("unproven rule returned native operations", operations, err)
			}
		})
	}
	for _, decision := range []Decision{{Effect: EffectSuppress}, {Effect: EffectMask, Algorithm: AlgorithmConstantV1, Parameters: map[string]any{"value": "[保护]"}}} {
		rule := transformRule()
		rule.Decision = decision
		operations, err := QueryOutputTransformsAt(source, "query", []Rule{rule}, SubjectReference{}, now)
		if err != nil || len(operations) != 1 {
			t.Fatal(err)
		}
		if decision.Effect == EffectSuppress && operations[0].Kind != "omit" || decision.Effect == EffectMask && (operations[0].Kind != "constant_text" || operations[0].Replacement != "[保护]") {
			t.Fatal("replacement or suppression changed")
		}
	}
}
